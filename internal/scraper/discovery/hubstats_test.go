package discovery

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/scraper/config"
)

// lisbonNoon is the fixed "now" of these tests: 2026-11-03 12:00 in Lisbon.
var lisbonNoon = time.Date(2026, 11, 3, 12, 0, 0, 0, lisbon)

// statsRun is one scraper process over hubs (slugs served by site) with a
// fixed clock.
func statsRun(t *testing.T, site *hubSite, now time.Time, hubs ...string) (*Scraper, error) {
	t.Helper()
	cfg := config.Config{UserAgent: "test", HubPages: hubs}
	s := newScraper(cfg, site.srv.URL, newSeenStore(newMemKV(), migratedDB(t)), &recordingPublisher{})
	s.now = func() time.Time { return now }
	if err := s.LoadKnownIDs(context.Background()); err != nil {
		t.Fatal(err)
	}
	return s, s.Discover(context.Background())
}

func TestDiscoverCountsPerHubSessions(t *testing.T) {
	site := newHubSite(t, 101, 102, 103, 104)
	site.cardDates[101] = "2026-11-02"       // yesterday
	site.cardDates[102] = "2026-11-03"       // today counts as future
	site.cardDates[103] = "2026-11-20T18:30" // date and time
	site.cardDates[104] = "soon"             // unparseable: not future

	s, err := statsRun(t, site, lisbonNoon, hubSlug)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Stats.Hubs) != 1 {
		t.Fatalf("Hubs = %+v, want one", s.Stats.Hubs)
	}
	h := s.Stats.Hubs[0]
	if h.Slug != hubSlug || h.ID != 500 || h.Err != nil {
		t.Errorf("hub = %+v, want %s (500) with no error", h, hubSlug)
	}
	if h.SessionsSeen != 4 || h.FutureSessions != 2 || h.NewEvents != 4 {
		t.Errorf("seen/future/new = %d/%d/%d, want 4/2/4", h.SessionsSeen, h.FutureSessions, h.NewEvents)
	}
}

func TestDiscoverCountsAHubWhoseSessionsAreAllPast(t *testing.T) {
	site := newHubSite(t, 101, 102)
	site.cardDates[101] = "2026-10-01"
	site.cardDates[102] = "2026-11-02T21:00"

	s, err := statsRun(t, site, lisbonNoon, hubSlug)
	if err != nil {
		t.Fatalf("Discover = %v: past-only sessions must not fail the run", err)
	}
	h := s.Stats.Hubs[0]
	if h.SessionsSeen != 2 || h.FutureSessions != 0 {
		t.Errorf("seen/future = %d/%d, want 2/0", h.SessionsSeen, h.FutureSessions)
	}
}

// TestDiscoverUsesLisbonDays checks "today" is the Lisbon date: at 23:30
// UTC on 2026-07-01 it is already 2 July in Lisbon (UTC+1 in summer).
func TestDiscoverUsesLisbonDays(t *testing.T) {
	site := newHubSite(t, 101)
	site.cardDates[101] = "2026-07-01"
	s, err := statsRun(t, site, time.Date(2026, 7, 1, 23, 30, 0, 0, time.UTC), hubSlug)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Stats.Hubs[0].FutureSessions; got != 0 {
		t.Errorf("FutureSessions = %d, want 0: 1 July is past in Lisbon", got)
	}
}

func TestDiscoverRecordsAFailedHub(t *testing.T) {
	site := newHubSite(t, 101)
	site.hubStatus = 500

	s, err := statsRun(t, site, lisbonNoon, "no-id-here", hubSlug)
	if err == nil {
		t.Fatal("Discover = nil with a failing hub page, want an error")
	}
	if len(s.Stats.Hubs) != 2 {
		t.Fatalf("Hubs = %+v, want both configured hubs", s.Stats.Hubs)
	}
	for _, h := range s.Stats.Hubs {
		if h.Err == nil {
			t.Errorf("hub %s has no error, want one", h.Slug)
		}
	}
}

func TestRunRecords(t *testing.T) {
	start := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	finish := start.Add(time.Minute)
	hubs := []HubStats{
		{Slug: "maia-98164", ID: 98164, SessionsSeen: 5, FutureSessions: 3, NewEvents: 1},
	}
	runErr := errors.New("discovery failed: context deadline exceeded")

	recs := RunRecords([]string{"maia-98164", "aveiro-98167"}, hubs, start, finish, runErr)
	if len(recs) != 2 {
		t.Fatalf("RunRecords = %+v, want one per configured hub", recs)
	}
	maia, aveiro := recs[0], recs[1]
	if maia.Failed() || maia.SessionsSeen != 5 || maia.FutureSessions != 3 || maia.NewEvents != 1 {
		t.Errorf("maia = %+v, want its stats and no error", maia)
	}
	if !aveiro.Failed() || aveiro.HubID != 98167 {
		t.Errorf("aveiro = %+v, want hub 98167 with the run's error (never reached)", aveiro)
	}
	if !strings.Contains(aveiro.Error, "context deadline exceeded") {
		t.Errorf("aveiro error %q lost the run's error", aveiro.Error)
	}
	if maia.RunID != aveiro.RunID || !maia.StartedAt.Equal(start) || !maia.FinishedAt.Equal(finish) {
		t.Errorf("records don't share the run: %+v / %+v", maia, aveiro)
	}

	// A run that failed before discovery records every hub with its error.
	recs = RunRecords([]string{"maia-98164"}, nil, start, finish, errors.New("load known event IDs: nats: timeout"))
	if len(recs) != 1 || !strings.Contains(recs[0].Error, "load known event IDs") {
		t.Errorf("RunRecords before discovery = %+v", recs)
	}
}

func TestRunRecordsTruncatesLongErrors(t *testing.T) {
	long := errors.New(strings.Repeat("é", 400))
	recs := RunRecords([]string{"x-1"}, []HubStats{{Slug: "x-1", ID: 1, Err: long}}, time.Now(), time.Now(), nil)
	if n := len(recs[0].Error); n > maxErrorLen+len("…") {
		t.Errorf("error is %d bytes, want at most %d", n, maxErrorLen+len("…"))
	}
	if !strings.HasSuffix(recs[0].Error, "…") {
		t.Errorf("truncated error %q has no ellipsis", recs[0].Error)
	}
}

func TestRunRecordsKeepsOneRecordPerHubID(t *testing.T) {
	recs := RunRecords([]string{"bad-a", "maia-1", "bad-b", "maia-1"}, nil, time.Now(), time.Now(),
		errors.New("boom"))
	if len(recs) != 2 || recs[0].HubSlug != "bad-a" || recs[1].HubID != 1 {
		t.Errorf("RunRecords = %+v, want bad-a (id 0) and maia-1 once each", recs)
	}
}
