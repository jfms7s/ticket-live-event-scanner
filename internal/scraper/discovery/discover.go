package discovery

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"
	// The scraper image (alpine) has no zone database.
	_ "time/tzdata"

	"github.com/jfms7s/ticket-live-event-scanner/internal/scraper/parse"
)

// Discover fetches every tracked hub page (design §6.1 — no site-wide
// /agenda or /pesquisa crawling) and publishes any new sessions found on
// them.
func (s *Scraper) Discover(ctx context.Context) error {
	var errorCount int
	today := s.now().In(lisbon).Format(dateLayout)

	for _, hubSlug := range s.cfg.HubPages {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Extract event ID from hub page slug
		hubID := parse.ExtractIDFromSlug(hubSlug)
		if hubID == 0 {
			// A misconfigured entry: it can never be scraped, so say so
			// on every run instead of skipping it silently.
			err := fmt.Errorf("hub page %q has no event ID in its slug", hubSlug)
			log.Printf("ERROR %v", err)
			s.Stats.Hubs = append(s.Stats.Hubs, HubStats{Slug: hubSlug, Err: err})
			errorCount++
			continue
		}

		// Rate limit
		time.Sleep(time.Duration(s.cfg.RequestDelayMS) * time.Millisecond)

		hub := HubStats{Slug: hubSlug, ID: hubID}
		published := s.Stats.EventsPublished
		hub.Err = s.fetchHubPage(ctx, hubSlug, hubID, today, &hub)
		hub.NewEvents = s.Stats.EventsPublished - published
		s.Stats.Hubs = append(s.Stats.Hubs, hub)
		if hub.Err != nil {
			log.Printf("Error fetching hub page %s: %v", hubSlug, hub.Err)
			errorCount++
		}
	}

	if errorCount > 0 {
		s.Stats.Errors = errorCount
		return fmt.Errorf("discovery completed with %d error(s)", errorCount)
	}

	return nil
}

// fetchHubPage fetches a venue/series hub page (the project design doc,
// kept outside this repo, §4/§6.1: the two example URLs are hub pages, not
// single events) and discovers new session links on it with
// parse.ParseHubSessions, which ignores the similar-events widget.
//
// It fills hub's session counts; today is the current date in
// Europe/Lisbon (YYYY-MM-DD).
func (s *Scraper) fetchHubPage(ctx context.Context, hubSlug string, hubID int64, today string, hub *HubStats) error {
	hubURL := fmt.Sprintf("%s/evento/%s", s.baseURL, hubSlug)
	body, err := s.fetcher.Fetch(ctx, hubURL, s.baseURL)
	if err != nil {
		return fmt.Errorf("fetch hub page %d: %w", hubID, err)
	}

	sessions, err := parse.ParseHubSessions(body)
	if err != nil {
		return fmt.Errorf("parse hub page %d: %w", hubID, err)
	}
	// A tracked hub page always lists its sessions; zero cards most likely
	// means ticketline.pt changed its markup, which must not look like "no
	// new sessions".
	if len(sessions) == 0 {
		return fmt.Errorf("hub page %d: no session cards found (markup change?)", hubID)
	}

	hub.SessionsSeen = len(sessions)
	for _, sess := range sessions {
		if isFutureSession(sess.EventDate, today) {
			hub.FutureSessions++
		}
	}

	var errorCount int
	for _, sess := range sessions {
		s.Stats.EventsFound++

		if s.knownIDs[sess.EventID] {
			continue
		}
		if s.skipIDs[sess.EventID] {
			// Rejected by this run or a recent one; retried once its
			// skipped-bucket key expires.
			s.Stats.Skipped++
			continue
		}

		time.Sleep(time.Duration(s.cfg.RequestDelayMS) * time.Millisecond)

		if err := s.fetchAndPublishEvent(ctx, sess, hubSlug); errors.Is(err, errSkip) {
			s.skip(ctx, sess.EventID, err)
		} else if err != nil {
			log.Printf("Error fetching detail for hub session event %d: %v", sess.EventID, err)
			errorCount++
		}
	}

	if errorCount > 0 {
		return fmt.Errorf("hub page %d: %d error(s) fetching events", hubID, errorCount)
	}

	return nil
}

// lisbon is the venue's time zone: "today" for future sessions is a
// Lisbon day.
var lisbon = mustLoadLocation("Europe/Lisbon")

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		// The binary embeds the zone database (time/tzdata), so this only
		// fails on a typo.
		panic(err)
	}
	return loc
}

const dateLayout = "2006-01-02"

// isFutureSession reports whether a card's start date (date-only, or a
// date and time such as 2026-09-04T18:30) falls on today or later. today
// is YYYY-MM-DD; both are compared as dates, so the time of day of a
// session today doesn't matter. A date that doesn't parse is not future.
func isFutureSession(startDate, today string) bool {
	if len(startDate) < len(dateLayout) {
		return false
	}
	day := startDate[:len(dateLayout)]
	if _, err := time.Parse(dateLayout, day); err != nil {
		return false
	}
	return day >= today
}
