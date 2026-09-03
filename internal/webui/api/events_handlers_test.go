package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/jfms7s/ticket-live-event-scanner/internal/webui/repository"
)

// TestListEventsEmpty tests listing events when database is empty.
func TestListEventsEmpty(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	app := &App{db: db, js: NewMockJetStream()}
	server := httptest.NewServer(app.NewMux())
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/events")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	var events []repository.EventResponse
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(events) != 0 {
		t.Fatalf("Expected 0 events, got %d", len(events))
	}
}

// TestGetEventNotFound tests getting a non-existent event.
func TestGetEventNotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	app := &App{db: db, js: NewMockJetStream()}
	server := httptest.NewServer(app.NewMux())
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/events/99999")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Expected status 404, got %d", resp.StatusCode)
	}
}

// TestGetEventFound tests getting an existing event.
func TestGetEventFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Insert test event
	eventDate := "2026-08-22"
	_, err := db.ExecContext(ctx, `
		INSERT INTO events (id, slug, title, venue, category, event_date, url, image_url, discovered_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, 98164, "auchan-live-academia-maia-98164", "Auchan Live | Academia Maia",
		"Academia Auchan Live | Loja da Maia", "Formação", eventDate,
		"https://www.ticketline.pt/evento/auchan-live-academia-maia-98164",
		"https://info.ticketline.pt/images/Espectaculos/98164/cartaz.jpg",
		"2026-08-17T09:00:00Z")
	if err != nil {
		t.Fatalf("Failed to insert test event: %v", err)
	}

	app := &App{db: db, js: NewMockJetStream()}
	server := httptest.NewServer(app.NewMux())
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/events/98164")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	var eventResp repository.EventResponse
	if err := json.NewDecoder(resp.Body).Decode(&eventResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if eventResp.ID != 98164 {
		t.Fatalf("Expected event ID 98164, got %d", eventResp.ID)
	}
}

// TestListEventsInvalidStatus tests that an unrecognized status filter is rejected.
func TestListEventsInvalidStatus(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	app := &App{db: db, js: NewMockJetStream()}
	server := httptest.NewServer(app.NewMux())
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/events?status=bogus")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("Expected status 400, got %d", resp.StatusCode)
	}
}

// TestGetEventInvalidID tests that a non-numeric event ID is rejected.
func TestGetEventInvalidID(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	app := &App{db: db, js: NewMockJetStream()}
	server := httptest.NewServer(app.NewMux())
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/events/not-a-number")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("Expected status 400, got %d", resp.StatusCode)
	}
}

// TestListEventsActiveFinishedFilter tests that ?status=active and
// ?status=finished correctly partition events by event_date relative to now.
func TestListEventsActiveFinishedFilter(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	insert := func(id int64, slug string, eventDate string) {
		_, err := db.ExecContext(ctx, `
			INSERT INTO events (id, slug, title, venue, category, event_date, url, image_url, discovered_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, id, slug, "Title "+slug, "Venue", "Category", eventDate,
			"https://www.ticketline.pt/evento/"+slug, "", "2026-08-17T09:00:00Z")
		if err != nil {
			t.Fatalf("Failed to insert test event %d: %v", id, err)
		}
	}

	insert(1, "past-event-1", "2000-01-01")
	insert(2, "future-event-2", "2999-01-01")

	app := &App{db: db, js: NewMockJetStream()}
	server := httptest.NewServer(app.NewMux())
	defer server.Close()

	fetch := func(status string) []repository.EventResponse {
		resp, err := http.Get(server.URL + "/api/events?status=" + status)
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%s: expected 200, got %d", status, resp.StatusCode)
		}
		var events []repository.EventResponse
		if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
			t.Fatalf("status=%s: failed to decode: %v", status, err)
		}
		return events
	}

	active := fetch("active")
	if len(active) != 1 || active[0].ID != 2 {
		t.Fatalf("Expected exactly the future event (id=2) for status=active, got %+v", active)
	}

	finished := fetch("finished")
	if len(finished) != 1 || finished[0].ID != 1 {
		t.Fatalf("Expected exactly the past event (id=1) for status=finished, got %+v", finished)
	}
}

// TestRetriggerNotFound tests triggering a retry for a non-existent event.
func TestRetriggerNotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	app := &App{db: db, js: NewMockJetStream()}
	server := httptest.NewServer(app.NewMux())
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/events/99999/retrigger", "application/json", nil)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Expected status 404, got %d", resp.StatusCode)
	}
}

// TestRetriggerSuccess tests successfully triggering a retry.
func TestRetriggerSuccess(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Insert test event
	eventDate := "2026-08-22"
	_, err := db.ExecContext(ctx, `
		INSERT INTO events (id, slug, title, venue, category, event_date, url, image_url, discovered_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, 98164, "auchan-live-academia-maia-98164", "Auchan Live | Academia Maia",
		"Academia Auchan Live | Loja da Maia", "Formação", eventDate,
		"https://www.ticketline.pt/evento/auchan-live-academia-maia-98164",
		"https://info.ticketline.pt/images/Espectaculos/98164/cartaz.jpg",
		"2026-08-17T09:00:00Z")
	if err != nil {
		t.Fatalf("Failed to insert test event: %v", err)
	}

	mockJS := &MockJetStream{}
	app := &App{db: db, js: mockJS}
	server := httptest.NewServer(app.NewMux())
	defer server.Close()

	resp, err := http.Post(server.URL+"/api/events/98164/retrigger", "application/json", nil)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("Expected status 202, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result["status"] != "pending" {
		t.Fatalf("Expected status 'pending', got %q", result["status"])
	}

	// Verify that a message was published to NATS
	if len(mockJS.publishedMessages) != 1 {
		t.Fatalf("Expected 1 published message, got %d", len(mockJS.publishedMessages))
	}
}

// TestSetPurchasedNotFound tests marking a non-existent event as purchased.
func TestSetPurchasedNotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	app := &App{db: db, js: NewMockJetStream()}
	server := httptest.NewServer(app.NewMux())
	defer server.Close()

	req, err := http.NewRequest(http.MethodPatch, server.URL+"/api/events/99999/purchased", strings.NewReader(`{"purchased":true}`))
	if err != nil {
		t.Fatalf("Failed to build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Expected status 404, got %d", resp.StatusCode)
	}
}

// TestSetPurchasedSuccess tests marking an existing event as purchased and un-purchased.
func TestSetPurchasedSuccess(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := db.ExecContext(ctx, `
		INSERT INTO events (id, slug, title, venue, category, event_date, url, image_url, discovered_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, 98164, "test-event", "Test Event", "", "", "", "http://example.com", "", "2026-08-17T09:00:00Z")
	if err != nil {
		t.Fatalf("Failed to insert test event: %v", err)
	}

	mockJS := NewMockJetStream()
	app := &App{db: db, js: mockJS}
	server := httptest.NewServer(app.NewMux())
	defer server.Close()

	setPurchased := func(purchased bool) *http.Response {
		body := fmt.Sprintf(`{"purchased":%t}`, purchased)
		req, err := http.NewRequest(http.MethodPatch, server.URL+"/api/events/98164/purchased", strings.NewReader(body))
		if err != nil {
			t.Fatalf("Failed to build request: %v", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}
		return resp
	}

	resp := setPurchased(true)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	getResp, err := http.Get(server.URL + "/api/events/98164")
	if err != nil {
		t.Fatalf("Failed to get event: %v", err)
	}
	defer getResp.Body.Close()
	var ev repository.EventResponse
	if err := json.NewDecoder(getResp.Body).Decode(&ev); err != nil {
		t.Fatalf("Failed to decode event: %v", err)
	}
	if !ev.Purchased {
		t.Fatalf("Expected purchased=true after PATCH, got false")
	}

	// Marking purchased=true should publish an events.purchased message so
	// email-notifier can send a calendar invite.
	if len(mockJS.publishedMessages) != 1 {
		t.Fatalf("Expected 1 published message after purchased=true, got %d", len(mockJS.publishedMessages))
	}
	if mockJS.publishedMessages[0].subject != streams.PurchasedSubject {
		t.Fatalf("Expected publish to %q, got %q", streams.PurchasedSubject, mockJS.publishedMessages[0].subject)
	}

	resp2 := setPurchased(false)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp2.StatusCode)
	}

	getResp2, err := http.Get(server.URL + "/api/events/98164")
	if err != nil {
		t.Fatalf("Failed to get event: %v", err)
	}
	defer getResp2.Body.Close()
	var ev2 repository.EventResponse
	if err := json.NewDecoder(getResp2.Body).Decode(&ev2); err != nil {
		t.Fatalf("Failed to decode event: %v", err)
	}
	if ev2.Purchased {
		t.Fatalf("Expected purchased=false after second PATCH, got true")
	}

	// Un-purchasing should not publish anything further.
	if len(mockJS.publishedMessages) != 1 {
		t.Fatalf("Expected still 1 published message after purchased=false, got %d", len(mockJS.publishedMessages))
	}
}

// TestDeleteEventNotFound tests deleting a non-existent event.
func TestDeleteEventNotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	app := &App{db: db, js: NewMockJetStream()}
	server := httptest.NewServer(app.NewMux())
	defer server.Close()

	req, err := http.NewRequest(http.MethodDelete, server.URL+"/api/events/99999", nil)
	if err != nil {
		t.Fatalf("Failed to build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Expected status 404, got %d", resp.StatusCode)
	}
}

// TestDeleteEventSuccess tests deleting an event along with its notifications.
func TestDeleteEventSuccess(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := db.ExecContext(ctx, `
		INSERT INTO events (id, slug, title, venue, category, event_date, url, image_url, discovered_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, 98164, "test-event", "Test Event", "", "", "", "http://example.com", "", "2026-08-17T09:00:00Z")
	if err != nil {
		t.Fatalf("Failed to insert test event: %v", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO notifications (event_id, status, triggered_by, attempted_at)
		VALUES (?, 'sent', 'scraper', ?)
	`, 98164, "2026-08-17T10:00:00Z")
	if err != nil {
		t.Fatalf("Failed to insert notification: %v", err)
	}

	app := &App{db: db, js: NewMockJetStream()}
	server := httptest.NewServer(app.NewMux())
	defer server.Close()

	req, err := http.NewRequest(http.MethodDelete, server.URL+"/api/events/98164", nil)
	if err != nil {
		t.Fatalf("Failed to build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("Expected status 204, got %d", resp.StatusCode)
	}

	getResp, err := http.Get(server.URL + "/api/events/98164")
	if err != nil {
		t.Fatalf("Failed to get event: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("Expected event to be gone (404), got %d", getResp.StatusCode)
	}

	var notifCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE event_id = ?`, 98164).Scan(&notifCount); err != nil {
		t.Fatalf("Failed to count notifications: %v", err)
	}
	if notifCount != 0 {
		t.Fatalf("Expected notifications to be deleted, found %d", notifCount)
	}
}
