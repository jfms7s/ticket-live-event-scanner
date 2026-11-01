package discovery

import (
	"context"
	"testing"

	"github.com/jfms7s/ticket-live-event-scanner/internal/scraper/config"
)

// TestDiscoverFailsOnAHubPageWithNoCards is a markup change on
// ticketline.pt: the hub page parses to zero sessions, which must fail the
// run rather than look like "no new sessions".
func TestDiscoverFailsOnAHubPageWithNoCards(t *testing.T) {
	site := newHubSite(t) // lists no sessions
	s, err := skipRun(t, site, newMemKV(), newMemKV(), &recordingPublisher{})
	if err == nil {
		t.Fatal("Discover = nil for a hub page with no cards, want an error")
	}
	if s.Stats.Errors != 1 {
		t.Errorf("Errors = %d, want 1", s.Stats.Errors)
	}
}

func TestDiscoverFailsOnAHubSlugWithoutAnID(t *testing.T) {
	site := newHubSite(t, 101)
	pub := &recordingPublisher{}
	cfg := config.Config{UserAgent: "test", HubPages: []string{"no-id-here", hubSlug}}
	s := newScraper(cfg, site.srv.URL, newSeenStore(newMemKV(), migratedDB(t)), pub)
	if err := s.LoadKnownIDs(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Discover(context.Background()); err == nil {
		t.Fatal("Discover = nil with a hub slug that has no ID, want an error")
	}
	// The valid hub page is still scraped.
	assertPublishedOnce(t, pub, 101)
}

func TestDiscoverDoesNotFetchAKnownSession(t *testing.T) {
	site := newHubSite(t, 101, 102)
	seen, skipped, pub := newMemKV(), newMemKV(), &recordingPublisher{}
	seen.keys["101"] = true

	if _, err := skipRun(t, site, seen, skipped, pub); err != nil {
		t.Fatal(err)
	}
	if n := site.detailFetches[101]; n != 0 {
		t.Errorf("known session 101 fetched %d times, want 0", n)
	}
	assertPublishedOnce(t, pub, 102)
}
