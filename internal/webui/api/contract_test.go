package api

import (
	"context"
	"encoding/json"
	"github.com/jfms7s/ticket-live-event-scanner/internal/outbox"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/webui/repository"
)

// The JSON field names below are the contract with the dashboard: the
// frontend reads them through the hand-written interfaces in
// web/frontend/src/types.ts (Event, Notification). Renaming, adding or
// removing a field here means updating types.ts in the same change.

var wantEventKeys = []string{
	"category", "discovered_at", "event_date", "hub_slug", "id", "image_url", "invite_error",
	"invite_status", "notifications", "purchased", "slug", "status", "title", "url", "venue",
}

var wantNotificationKeys = []string{
	"attempted_at", "confirmed_at", "error", "id", "status", "telegram_message_id", "triggered_by",
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestEventJSONContract(t *testing.T) {
	s := func(v string) *string { return &v }
	ev := repository.EventResponse{
		ID: 1, Slug: "s", Title: "t", Venue: s("v"), Category: s("c"), EventDate: s("2026-01-01"),
		URL: "u", ImageURL: s("i"), DiscoveredAt: "2026-01-01T00:00:00Z", Status: "active",
		Notifications: []repository.NotificationInEventResponse{{ID: 2, Status: "sent", AttemptedAt: "a", TriggeredBy: "scraper"}},
	}
	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}

	var event map[string]json.RawMessage
	if err := json.Unmarshal(raw, &event); err != nil {
		t.Fatal(err)
	}
	if got := sortedKeys(event); !reflect.DeepEqual(got, wantEventKeys) {
		t.Errorf("event JSON keys = %v, want %v (update web/frontend/src/types.ts too)", got, wantEventKeys)
	}

	var notifs []map[string]json.RawMessage
	if err := json.Unmarshal(event["notifications"], &notifs); err != nil || len(notifs) != 1 {
		t.Fatalf("notifications = %s (err %v)", event["notifications"], err)
	}
	if got := sortedKeys(notifs[0]); !reflect.DeepEqual(got, wantNotificationKeys) {
		t.Errorf("notification JSON keys = %v, want %v (update web/frontend/src/types.ts too)", got, wantNotificationKeys)
	}
}

// TestNotificationResponseJSONUnchanged pins GET /api/notifications to the
// shape it had before NotificationResponse embedded
// NotificationInEventResponse: the same keys and values (only the key order
// within each object moved, which JSON clients don't see).
func TestNotificationResponseJSONUnchanged(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, `INSERT INTO events (id, slug, title, url, discovered_at) VALUES (98164, 'e', 'E', 'http://example.com', '2026-08-17T09:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO notifications (event_id, status, telegram_message_id, attempted_at, confirmed_at, error, triggered_by)
		VALUES (98164, 'sent', '1337', '2026-08-17T10:00:00Z', '2026-08-17T10:01:00Z', NULL, 'scraper'),
		       (98164, 'failed', NULL, '2026-08-17T11:00:00Z', '2026-08-17T11:01:00Z', 'boom', 'manual-retry')`); err != nil {
		t.Fatal(err)
	}

	app := &App{db: db, queue: &outbox.Queue{DB: db}}
	server := httptest.NewServer(app.NewMux())
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/notifications")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}

	// The response the flat pre-embedding struct produced for these rows.
	const before = `[
		{"id":2,"event_id":98164,"status":"failed","telegram_message_id":null,"attempted_at":"2026-08-17T11:00:00Z","confirmed_at":"2026-08-17T11:01:00Z","error":"boom","triggered_by":"manual-retry"},
		{"id":1,"event_id":98164,"status":"sent","telegram_message_id":"1337","attempted_at":"2026-08-17T10:00:00Z","confirmed_at":"2026-08-17T10:01:00Z","error":null,"triggered_by":"scraper"}
	]`
	var got, want any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode response %s: %v", body, err)
	}
	if err := json.Unmarshal([]byte(before), &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("GET /api/notifications = %s\nwant (as JSON) %s", body, before)
	}
}

func TestErrorResponsesAreJSON(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	app := &App{db: db, queue: &outbox.Queue{DB: db}}
	server := httptest.NewServer(app.NewMux())
	defer server.Close()

	cases := []struct {
		path      string
		status    int
		wantError string
	}{
		{"/api/events/abc", http.StatusBadRequest, "invalid event ID"},
		{"/api/events/999", http.StatusNotFound, "event not found"},
		{"/api/events?status=bogus", http.StatusBadRequest, "status must be 'active' or 'finished'"},
		{"/api/notifications?status=bogus", http.StatusBadRequest, "status must be 'pending', 'sent', or 'failed'"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			resp, err := http.Get(server.URL + tc.path)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tc.status {
				t.Errorf("status = %d, want %d", resp.StatusCode, tc.status)
			}
			if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q, want application/json", ct)
			}
			var body struct {
				Error string `json:"error"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatalf("decode error body: %v", err)
			}
			if body.Error != tc.wantError {
				t.Errorf("error = %q, want %q", body.Error, tc.wantError)
			}
		})
	}
}
