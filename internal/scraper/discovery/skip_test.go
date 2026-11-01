package discovery

import (
	"context"
	"errors"
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

// skipRun is one scraper process with a skipped bucket.
func skipRun(t *testing.T, site *hubSite, seenKV, skippedKV seenKV, pub publisher) (*Scraper, error) {
	t.Helper()
	cfg := config.Config{UserAgent: "test", HubPages: []string{hubSlug}}
	s := newScraper(cfg, site.srv.URL, newSeenStore(seenKV, migratedDB(t)), pub)
	s.skipped = newSkipStore(skippedKV)
	if err := s.LoadKnownIDs(context.Background()); err != nil {
		return s, err
	}
	return s, s.Discover(context.Background())
}

func TestDiscoverSkipsASessionMissingItsTitle(t *testing.T) {
	site := newHubSite(t, 101, 102)
	site.detail[101] = noTitlePage
	pub := &recordingPublisher{}

	s, err := skipRun(t, site, newMemKV(), newMemKV(), pub)
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
	pub := &recordingPublisher{}

	s, err := skipRun(t, site, newMemKV(), newMemKV(), pub)
	if err != nil {
		t.Fatalf("Discover = %v, want nil", err)
	}
	if s.Stats.Skipped != 1 || len(pub.published) != 0 {
		t.Errorf("Stats = %+v, published %d, want Skipped 1 and nothing published", s.Stats, len(pub.published))
	}
}

// TestDiscoverRemembersRejectedSessions is the next hourly run: a session
// rejected less than a day ago (its skipped-bucket key not yet expired)
// is not fetched again, and once the key is gone it is retried.
func TestDiscoverRemembersRejectedSessions(t *testing.T) {
	site := newHubSite(t, 101)
	site.detail[101] = noTitlePage
	seen, skipped, pub := newMemKV(), newMemKV(), &recordingPublisher{}

	for range 2 {
		if _, err := skipRun(t, site, seen, skipped, pub); err != nil {
			t.Fatal(err)
		}
	}
	if n := site.detailFetches[101]; n != 1 {
		t.Errorf("rejected session fetched %d times over two runs, want 1", n)
	}
	if !skipped.keys["101"] {
		t.Errorf("skipped bucket keys = %v, want 101", skipped.keys)
	}

	// The key expired and the page was fixed.
	delete(skipped.keys, "101")
	delete(site.detail, 101)
	s, err := skipRun(t, site, seen, skipped, pub)
	if err != nil {
		t.Fatal(err)
	}
	if s.Stats.Skipped != 0 {
		t.Errorf("Skipped = %d after the key expired, want 0", s.Stats.Skipped)
	}
	assertPublishedOnce(t, pub, 101)
}

// TestDiscoverWorksWithAnUnreadableSkippedBucket checks that the skipped
// set only saves requests: if it can't be read the run carries on.
func TestDiscoverWorksWithAnUnreadableSkippedBucket(t *testing.T) {
	site := newHubSite(t, 101)
	skipped := newMemKV()
	skipped.listErr = errors.New("nats: timeout")
	pub := &recordingPublisher{}

	if _, err := skipRun(t, site, newMemKV(), skipped, pub); err != nil {
		t.Fatalf("run = %v, want nil with an unreadable skipped bucket", err)
	}
	assertPublishedOnce(t, pub, 101)
}

func TestDiscoverFailsOnATransportErrorForADetailPage(t *testing.T) {
	site := newHubSite(t, 101)
	site.detail[101] = func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}
	pub := &recordingPublisher{}

	s, err := skipRun(t, site, newMemKV(), newMemKV(), pub)
	if err == nil {
		t.Fatal("Discover = nil, want an error for a detail page that can't be fetched")
	}
	if s.Stats.Skipped != 0 {
		t.Errorf("Skipped = %d, want 0: a transport error is not a data problem", s.Stats.Skipped)
	}
}
