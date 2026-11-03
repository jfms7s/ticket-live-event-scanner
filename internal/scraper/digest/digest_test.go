package digest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/runs"
	"github.com/jfms7s/ticket-live-event-scanner/internal/store"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/nats-io/nats.go/jetstream"
	_ "modernc.org/sqlite"
)

// sunday is the digest's scheduled time: Sunday 9 Nov 2025 19:00 Lisbon.
var sunday = time.Date(2025, 11, 9, 19, 0, 0, 0, lisbon)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/scanner.db")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

type ev struct {
	id         int64
	title      string
	date       string
	discovered time.Time
	purchased  bool
	hidden     bool
}

func insert(t *testing.T, db *sql.DB, events ...ev) {
	t.Helper()
	for _, e := range events {
		_, err := db.Exec(`INSERT INTO events (id, slug, title, venue, event_date, url, discovered_at, purchased, hidden)
			VALUES (?, ?, ?, 'Auchan Maia', ?, 'https://x', ?, ?, ?)`,
			e.id, fmt.Sprintf("s-%d", e.id), e.title, e.date, e.discovered.UTC().Format("2006-01-02T15:04:05Z"), e.purchased, e.hidden)
		if err != nil {
			t.Fatal(err)
		}
	}
}

// fakeLog is a RunLog with fixed answers.
type fakeLog struct {
	latest  []runs.HubStatus
	records []runs.Record
}

func (f fakeLog) Summary(context.Context, int) (runs.Summary, error) {
	return runs.Summary{Latest: f.latest}, nil
}

func (f fakeLog) Since(_ context.Context, since time.Time) ([]runs.Record, error) {
	var out []runs.Record
	for _, r := range f.records {
		if !r.StartedAt.Before(since) {
			out = append(out, r)
		}
	}
	return out, nil
}

func record(start time.Time, hub int64, errMsg string) runs.Record {
	return runs.Record{RunID: runs.RunID(start), HubID: hub, StartedAt: start, Error: errMsg}
}

func TestBuild(t *testing.T) {
	db := testDB(t)
	old := sunday.Add(-30 * 24 * time.Hour)
	insert(t, db,
		ev{id: 1, title: "Today, later", date: "2025-11-09T20:00", discovered: old},
		ev{id: 2, title: "Bought", date: "2025-11-15", discovered: sunday.Add(-2 * 24 * time.Hour), purchased: true},
		ev{id: 3, title: "Day 14, out", date: "2025-11-23", discovered: old},
		ev{id: 4, title: "Yesterday", date: "2025-11-08", discovered: old},
		ev{id: 5, title: "Removed", date: "2025-11-10", discovered: sunday.Add(-time.Hour), hidden: true},
		ev{id: 6, title: "Day 13, in", date: "2025-11-22T00:00:00Z", discovered: sunday.Add(-8 * 24 * time.Hour)},
	)
	log := fakeLog{
		latest: []runs.HubStatus{{Record: runs.Record{HubSlug: "auchan-live-academia-maia-98164"}, UpcomingSessions: 3}},
		records: []runs.Record{
			record(sunday.Add(-8*24*time.Hour), 1, "too old"),
			record(sunday.Add(-2*time.Hour), 1, ""), record(sunday.Add(-2*time.Hour), 2, "boom"),
			record(sunday.Add(-time.Hour), 1, ""), record(sunday.Add(-time.Hour), 2, ""),
		},
	}

	d, err := Build(context.Background(), db, log, sunday)
	if err != nil {
		t.Fatal(err)
	}
	if d.Week != "2025-W45" {
		t.Errorf("Week = %s, want 2025-W45", d.Week)
	}
	if got := d.WeekStart.Format("2006-01-02") + ".." + d.WeekEnd.Format("2006-01-02"); got != "2025-11-03..2025-11-09" {
		t.Errorf("week = %s, want Mon 3 to Sun 9 Nov", got)
	}
	var titles []string
	for _, s := range d.Upcoming {
		titles = append(titles, s.Title)
	}
	if strings.Join(titles, "|") != "Today, later|Bought|Day 13, in" {
		t.Errorf("upcoming = %v", titles)
	}
	if d.Upcoming[0].Time != "20:00" || d.Upcoming[2].Time != "" || !d.Upcoming[1].Purchased {
		t.Errorf("upcoming details = %+v", d.Upcoming)
	}
	if d.NewCount != 1 || d.NewTitles[0] != "Bought" {
		t.Errorf("new = %d %v, want only Bought (the removed one is hidden)", d.NewCount, d.NewTitles)
	}
	if d.Runs != 2 || d.FailedRuns != 1 || !d.RunsKnown {
		t.Errorf("runs = %d (%d failed), want 2 (1 failed)", d.Runs, d.FailedRuns)
	}
	if len(d.Hubs) != 1 || d.Hubs[0].Upcoming != 3 {
		t.Errorf("hubs = %+v", d.Hubs)
	}
}

func TestFormat(t *testing.T) {
	day := func(s string) time.Time {
		d, _ := time.ParseInLocation("2006-01-02", s, lisbon)
		return d
	}
	base := Digest{WeekStart: day("2025-10-27"), WeekEnd: day("2025-11-02"), Week: "2025-W44"}
	tests := []struct {
		name    string
		digest  func() Digest
		want    []string
		notWant []string
	}{
		{
			name:    "nothing new is the heartbeat",
			digest:  func() Digest { return base },
			want:    []string{"week of 27 Oct – 2 Nov", "No sessions in the next 14 days", "<b>Nothing new this week</b>"},
			notWant: []string{"<b>Runs</b>", "<b>Hubs</b>"},
		},
		{
			name: "purchased marked and text escaped",
			digest: func() Digest {
				d := base
				d.Upcoming = []Session{
					{Day: day("2025-11-08"), Time: "18:30", Title: "Fado & <b>Jazz</b>", Venue: "Maia", Purchased: true},
					{Day: day("2025-11-09"), Title: "Workshop", Venue: "Aveiro"},
				}
				d.UpcomingTotal = 12
				d.NewCount = 7
				d.NewTitles = []string{"A", "B", "C", "D", "E"}
				return d
			},
			want: []string{
				"Sat 8 Nov 18:30 · Fado &amp; &lt;b&gt;Jazz&lt;/b&gt; · Maia ✅\n",
				"Sun 9 Nov · Workshop · Aveiro\n",
				"+10 more", "<b>New this week: 7</b>", "• E\n+2 more",
			},
			notWant: []string{"Nothing new", "Aveiro ✅"},
		},
		{
			name: "a failed run and a failed hub",
			digest: func() Digest {
				d := base
				d.Hubs = []Hub{{Slug: "auchan-live-academia-maia-98164", Upcoming: 4}, {Slug: "aveiro-98167", Upcoming: 2, Failed: true}}
				d.Runs, d.FailedRuns, d.RunsKnown = 167, 3, true
				return d
			},
			want: []string{"Auchan Live Academia Maia: 4 upcoming", "Aveiro: last run failed (2 upcoming before)", "<b>Runs</b>: 167 runs, 3 failed"},
		},
		{
			name: "same-month week",
			digest: func() Digest {
				d := base
				d.WeekStart, d.WeekEnd = day("2025-11-03"), day("2025-11-09")
				return d
			},
			want: []string{"week of 3 – 9 Nov"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Format(tt.digest())
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q in:\n%s", w, got)
				}
			}
			for _, w := range tt.notWant {
				if strings.Contains(got, w) {
					t.Errorf("unexpected %q in:\n%s", w, got)
				}
			}
		})
	}
}

// TestFormatFitsTelegram: 30 events with long titles stay one message.
func TestFormatFitsTelegram(t *testing.T) {
	db := testDB(t)
	long := strings.Repeat("Workshop de Natal com <muito> & mais ", 10)
	for i := int64(1); i <= 30; i++ {
		insert(t, db, ev{id: i, title: fmt.Sprintf("%s %d", long, i), date: sunday.AddDate(0, 0, int(i%14)).Format("2006-01-02T15:04"), discovered: sunday.Add(-time.Hour)})
	}
	var hubs []runs.HubStatus
	for i := 0; i < 5; i++ {
		hubs = append(hubs, runs.HubStatus{Record: runs.Record{HubSlug: strings.Repeat("very-long-hub-name-", 6) + "1"}})
	}
	d, err := Build(context.Background(), db, fakeLog{latest: hubs}, sunday)
	if err != nil {
		t.Fatal(err)
	}
	text := Format(d)
	if n := utf8.RuneCountInString(text); n > MaxMessageLen {
		t.Errorf("digest is %d characters, over %d", n, MaxMessageLen)
	}
	if len(d.Upcoming) != maxUpcoming || !strings.Contains(text, "+20 more") {
		t.Errorf("upcoming not capped at %d: %d shown\n%s", maxUpcoming, len(d.Upcoming), text)
	}
}

func TestHubName(t *testing.T) {
	for slug, want := range map[string]string{
		"auchan-live-academia-maia-98164": "Auchan Live Academia Maia",
		"aveiro":                          "Aveiro",
		"98164":                           "98164",
	} {
		if got := HubName(slug); got != want {
			t.Errorf("HubName(%q) = %q, want %q", slug, got, want)
		}
	}
}

// recordingPublisher records publishes and acks a second digest for the
// same week as a duplicate, like the stream's dedup window on the
// "digest-<week>" message ID (checked on a real server in
// TestSendMessageIDOnARealStream).
type recordingPublisher struct {
	msgs  []event.Digest
	weeks map[string]bool
}

func (p *recordingPublisher) Publish(_ context.Context, subject string, data []byte, _ ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	if subject != streams.DigestSubject {
		return nil, fmt.Errorf("subject %q", subject)
	}
	var d event.Digest
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	if p.weeks[d.Week] {
		return &jetstream.PubAck{Duplicate: true}, nil
	}
	p.weeks[d.Week] = true
	p.msgs = append(p.msgs, d)
	return &jetstream.PubAck{}, nil
}

// TestSendPublishesOneDigest is the --digest mode against a temp database:
// exactly one message, carrying the week and the formatted text.
func TestSendPublishesOneDigest(t *testing.T) {
	db := testDB(t)
	insert(t, db, ev{id: 1, title: "Workshop", date: "2025-11-12", discovered: sunday.Add(-time.Hour)})
	pub := &recordingPublisher{weeks: map[string]bool{}}

	if err := Send(context.Background(), db, fakeLog{}, pub, sunday); err != nil {
		t.Fatal(err)
	}
	if len(pub.msgs) != 1 {
		t.Fatalf("published %d digests, want 1", len(pub.msgs))
	}
	if pub.msgs[0].Week != "2025-W45" || !strings.Contains(pub.msgs[0].Text, "Wed 12 Nov · Workshop") {
		t.Errorf("digest = %+v", pub.msgs[0])
	}

	// A re-run the same week is a stream duplicate: still one message.
	if err := Send(context.Background(), db, fakeLog{}, pub, sunday.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(pub.msgs) != 1 {
		t.Errorf("published %d digests after a re-run, want 1", len(pub.msgs))
	}
}
