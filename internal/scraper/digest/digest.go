// Package digest builds the weekly Telegram digest: the sessions of the
// next 14 days (purchased ones marked), what is new this week, the hubs
// watched and how the scraper's runs went. It goes to the shared channel
// every Sunday evening, "Nothing new this week" included; the runs line
// (or its "no scraper run" warning) is what shows the scanner is alive.
package digest

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"time"
	// The image (alpine) has no zone database.
	_ "time/tzdata"

	"github.com/jfms7s/ticket-live-event-scanner/internal/runs"
)

const (
	// upcomingDays is the window of "next sessions", today included.
	upcomingDays = 14
	// maxUpcoming and maxNewTitles keep the message to one screen.
	maxUpcoming  = 10
	maxNewTitles = 5
	maxHubs      = 10
	// maxTitleLen cuts long titles so the caps above bound the length.
	maxTitleLen = 80
)

var lisbon = mustLoadLocation("Europe/Lisbon")

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err) // time/tzdata is embedded: only a typo fails
	}
	return loc
}

// Session is one upcoming session.
type Session struct {
	Day       time.Time // the date, at midnight Lisbon
	Time      string    // "18:30", or "" when the listing has no time
	Title     string
	Venue     string
	Purchased bool
}

// Hub is one watched hub page.
type Hub struct {
	Slug     string
	Upcoming int
	Failed   bool
}

// Digest is everything the message says.
type Digest struct {
	// WeekStart and WeekEnd are the Monday and Sunday of the week covered.
	WeekStart, WeekEnd time.Time
	// Week is the ISO week, e.g. "2026-W45".
	Week     string
	Upcoming []Session
	// UpcomingTotal counts every upcoming session, including those past
	// the cap.
	UpcomingTotal int
	NewCount      int
	NewTitles     []string
	Hubs          []Hub
	// Runs and FailedRuns count the scraper runs that started in the last
	// seven days.
	Runs, FailedRuns int
}

// RunLog is what Build reads from the run log (a *runs.Reader).
type RunLog interface {
	Summary(ctx context.Context, limit int) (runs.Summary, error)
	Since(ctx context.Context, since time.Time) ([]runs.Record, error)
}

// Build gathers the digest as of now from web-ui-api's events table
// (read only) and the run log.
func Build(ctx context.Context, db *sql.DB, log RunLog, now time.Time) (Digest, error) {
	now = now.In(lisbon)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, lisbon)
	weekday := (int(today.Weekday()) + 6) % 7 // Monday 0 .. Sunday 6
	d := Digest{
		WeekStart: today.AddDate(0, 0, -weekday),
		WeekEnd:   today.AddDate(0, 0, 6-weekday),
	}
	year, week := now.ISOWeek()
	d.Week = fmt.Sprintf("%d-W%02d", year, week)

	if err := d.addEvents(ctx, db, today, now); err != nil {
		return Digest{}, err
	}
	if err := d.addRuns(ctx, log, now); err != nil {
		return Digest{}, err
	}
	return d, nil
}

func (d *Digest) addEvents(ctx context.Context, db *sql.DB, today, now time.Time) error {
	rows, err := db.QueryContext(ctx,
		`SELECT title, venue, event_date, purchased, discovered_at FROM events WHERE hidden = 0`)
	if err != nil {
		return fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	end := today.AddDate(0, 0, upcomingDays)
	weekAgo := now.Add(-7 * 24 * time.Hour)
	type newEvent struct {
		title string
		at    time.Time
	}
	var news []newEvent
	for rows.Next() {
		var (
			title                   string
			venue, date, discovered sql.NullString
			purchased               bool
		)
		if err := rows.Scan(&title, &venue, &date, &purchased, &discovered); err != nil {
			return fmt.Errorf("scan event: %w", err)
		}
		if at, ok := parseTimestamp(discovered.String); ok && !at.Before(weekAgo) {
			news = append(news, newEvent{title: title, at: at})
		}
		day, hm, ok := parseEventDate(date.String)
		if !ok || day.Before(today) || !day.Before(end) {
			continue
		}
		d.Upcoming = append(d.Upcoming, Session{Day: day, Time: hm, Title: title, Venue: venue.String, Purchased: purchased})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate events: %w", err)
	}

	slices.SortStableFunc(d.Upcoming, func(a, b Session) int {
		if c := a.Day.Compare(b.Day); c != 0 {
			return c
		}
		if c := strings.Compare(a.Time, b.Time); c != 0 {
			return c
		}
		return strings.Compare(a.Title, b.Title)
	})
	d.UpcomingTotal = len(d.Upcoming)
	if len(d.Upcoming) > maxUpcoming {
		d.Upcoming = d.Upcoming[:maxUpcoming]
	}

	slices.SortStableFunc(news, func(a, b newEvent) int { return b.at.Compare(a.at) })
	d.NewCount = len(news)
	for _, n := range news {
		if len(d.NewTitles) == maxNewTitles {
			break
		}
		d.NewTitles = append(d.NewTitles, n.title)
	}
	return nil
}

func (d *Digest) addRuns(ctx context.Context, log RunLog, now time.Time) error {
	sum, err := log.Summary(ctx, 0)
	if err != nil {
		return fmt.Errorf("read run log: %w", err)
	}
	for _, h := range sum.Latest {
		d.Hubs = append(d.Hubs, Hub{Slug: h.HubSlug, Upcoming: h.UpcomingSessions, Failed: h.Failed()})
	}

	records, err := log.Since(ctx, now.Add(-7*24*time.Hour))
	if err != nil {
		return fmt.Errorf("read run log: %w", err)
	}
	failed := map[string]bool{}
	for _, r := range records {
		failed[r.RunID] = failed[r.RunID] || r.Failed()
	}
	d.Runs = len(failed)
	for _, f := range failed {
		if f {
			d.FailedRuns++
		}
	}
	return nil
}

// parseEventDate reads a stored event_date: "2026-11-08", "2026-11-08T18:30"
// or the same re-serialised as RFC 3339 by some drivers (whose "Z" is not
// a real UTC conversion; see repository.computeStatus). A "00:00" time is
// the driver's placeholder for a date-only listing.
func parseEventDate(s string) (day time.Time, hm string, ok bool) {
	if len(s) < 10 {
		return time.Time{}, "", false
	}
	day, err := time.ParseInLocation("2006-01-02", s[:10], lisbon)
	if err != nil {
		return time.Time{}, "", false
	}
	if len(s) >= 16 && s[10] == 'T' && s[13] == ':' {
		if _, err := time.Parse("15:04", s[11:16]); err == nil && s[11:16] != "00:00" {
			hm = s[11:16]
		}
	}
	return day, hm, true
}

// timestampLayouts are the forms discovered_at comes back in: web-ui-api
// writes "2006-01-02T15:04:05Z", SQLite's default is "2006-01-02 15:04:05"
// (UTC), and some drivers hand back RFC 3339.
var timestampLayouts = []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05"}

func parseTimestamp(s string) (time.Time, bool) {
	for _, layout := range timestampLayouts {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}
