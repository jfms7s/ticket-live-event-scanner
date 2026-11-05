package discovery

import "testing"

// TestDiscoverStampsTheHubOnEachPublishedSession: every published
// event.Discovered names the hub page that listed it, so web-ui-api can
// group events by hub.
func TestDiscoverStampsTheHubOnEachPublishedSession(t *testing.T) {
	site := newHubSite(t, 101, 102)
	db := migratedDB(t)
	pub := newQueue(db)

	mustRun(t, site, db, pub)

	if len(pub.published) != 2 {
		t.Fatalf("published %d messages, want 2", len(pub.published))
	}
	for _, d := range pub.published {
		if d.HubSlug != hubSlug {
			t.Errorf("event %d HubSlug = %q, want %q", d.EventID, d.HubSlug, hubSlug)
		}
	}
}
