package discovery

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"testing"

	"github.com/jfms7s/ticket-live-event-scanner/internal/scraper/config"
)

// A detail page without a title: the parser can't build an event from it.
func noTitlePage(w http.ResponseWriter, _ *http.Request) {
	fmt.Fprint(w, `<html><body><div itemscope itemtype="http://schema.org/Event">
	<p itemprop="location">Casa do Fado</p>
	<span itemprop="startDate" content="2030-01-01">1 Jan 2030</span>
</div></body></html>`)
}

// A detail page whose start date matches no known layout.
func badDatePage(w http.ResponseWriter, _ *http.Request) {
	fmt.Fprint(w, `<html><body><div itemscope itemtype="http://schema.org/Event">
	<h1 itemprop="name">Session</h1>
	<span itemprop="startDate" content="sometime soon">soon</span>
</div></body></html>`)
}

// skipRun is one scraper process over db (a fresh one when nil).
func skipRun(t *testing.T, site *hubSite, db *sql.DB, pub *recordingPublisher) (*Scraper, error) {
	t.Helper()
	cfg := config.Config{UserAgent: "test", HubPages: []string{hubSlug}}
	s := newScraper(cfg, site.srv.URL, db, pub)
	if err := s.LoadKnownIDs(context.Background()); err != nil {
		return s, err
	}
	return s, s.Discover(context.Background())
}

func TestDiscoverSkipsASessionMissingItsTitle(t *testing.T) {
	site := newHubSite(t, 101, 102)
	site.detail[101] = noTitlePage
	db := migratedDB(t)
	pub := newQueue(db)

	s, err := skipRun(t, site, db, pub)
	if err != nil {
		t.Fatalf("Discover = %v, want nil: a per-session data problem must not fail the run", err)
	}
	if s.Stats.Skipped != 1 || s.Stats.Errors != 0 {
		t.Errorf("Stats = %+v, want Skipped 1, Errors 0", s.Stats)
	}
	assertPublishedOnce(t, pub, 102)
}

func TestDiscoverSkipsASessionWithAnInvalidDate(t *testing.T) {
	site := newHubSite(t, 101)
	site.detail[101] = badDatePage
	db := migratedDB(t)
	pub := newQueue(db)

	s, err := skipRun(t, site, db, pub)
	if err != nil {
		t.Fatalf("Discover = %v, want nil", err)
	}
	if s.Stats.Skipped != 1 || len(pub.published) != 0 {
		t.Errorf("Stats = %+v, published %d, want Skipped 1 and nothing published", s.Stats, len(pub.published))
	}
}

// TestDiscoverRemembersRejectedSessions is the next hourly run: a session
// rejected less than a day ago is not fetched again, and once the entry has
// expired it is retried.
func TestDiscoverRemembersRejectedSessions(t *testing.T) {
	site := newHubSite(t, 101)
	site.detail[101] = noTitlePage
	db := migratedDB(t)
	pub := newQueue(db)

	for range 2 {
		if _, err := skipRun(t, site, db, pub); err != nil {
			t.Fatal(err)
		}
	}
	if n := site.detailFetches[101]; n != 1 {
		t.Errorf("rejected session fetched %d times over two runs, want 1", n)
	}
	var reason string
	if err := db.QueryRow(`SELECT reason FROM skipped_sessions WHERE event_id = 101`).Scan(&reason); err != nil || reason == "" {
		t.Errorf("skipped_sessions has no entry with a reason for 101: %q, %v", reason, err)
	}

	// The entry expired and the page was fixed.
	if _, err := db.Exec(`UPDATE skipped_sessions SET until = '2000-01-01T00:00:00Z'`); err != nil {
		t.Fatal(err)
	}
	delete(site.detail, 101)
	s, err := skipRun(t, site, db, pub)
	if err != nil {
		t.Fatal(err)
	}
	if s.Stats.Skipped != 0 {
		t.Errorf("Skipped = %d after the entry expired, want 0", s.Stats.Skipped)
	}
	assertPublishedOnce(t, pub, 101)
}

// TestDiscoverWorksWithAnUnreadableSkippedTable checks that the skipped set
// only saves requests: if it can't be read the run carries on.
func TestDiscoverWorksWithAnUnreadableSkippedTable(t *testing.T) {
	site := newHubSite(t, 101)
	db := migratedDB(t)
	if _, err := db.Exec(`DROP TABLE skipped_sessions`); err != nil {
		t.Fatal(err)
	}
	pub := newQueue(db)

	if _, err := skipRun(t, site, db, pub); err != nil {
		t.Fatalf("run = %v, want nil with an unreadable skipped table", err)
	}
	assertPublishedOnce(t, pub, 101)
}

func TestDiscoverFailsOnATransportErrorForADetailPage(t *testing.T) {
	site := newHubSite(t, 101)
	site.detail[101] = func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}
	db := migratedDB(t)
	pub := newQueue(db)

	s, err := skipRun(t, site, db, pub)
	if err == nil {
		t.Fatal("Discover = nil, want an error for a detail page that can't be fetched")
	}
	if s.Stats.Skipped != 0 {
		t.Errorf("Skipped = %d, want 0: a transport error is not a data problem", s.Stats.Skipped)
	}
}
