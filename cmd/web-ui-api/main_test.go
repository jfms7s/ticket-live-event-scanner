package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/nats-io/nats.go/jetstream"
	_ "modernc.org/sqlite"
)

// setupTestDB creates a file-based SQLite database for testing.
// Using file-based DB instead of :memory: to avoid connection pool issues
func setupTestDB(t *testing.T) *sql.DB {
	// Use file-based SQLite database for unit tests with URI=true for better control
	tempDir := t.TempDir()
	dbPath := "file:" + tempDir + "/test.db?cache=shared"

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	// Limit connection pool to avoid database locking issues in tests
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	// Set reasonable timeouts
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create tables directly for testing
	if err := setupTestSchema(ctx, db); err != nil {
		t.Fatalf("Failed to setup test schema: %v", err)
	}

	return db
}

// setupTestSchema creates the necessary tables for testing
func setupTestSchema(ctx context.Context, db *sql.DB) error {
	// Create tables separately since Exec doesn't handle multiple statements well
	tables := []string{
		`CREATE TABLE IF NOT EXISTS events (
			id            INTEGER PRIMARY KEY,
			slug          TEXT NOT NULL,
			title         TEXT NOT NULL,
			venue         TEXT,
			category      TEXT,
			event_date    DATE,
			url           TEXT NOT NULL,
			image_url     TEXT,
			discovered_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			purchased     BOOLEAN NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS notifications (
			id                  INTEGER PRIMARY KEY AUTOINCREMENT,
			event_id            INTEGER NOT NULL REFERENCES events(id),
			status              TEXT NOT NULL CHECK (status IN ('pending','sent','failed')),
			telegram_message_id TEXT,
			attempted_at        TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			confirmed_at        TIMESTAMP,
			error               TEXT,
			triggered_by        TEXT NOT NULL DEFAULT 'scraper'
		)`,
		`CREATE INDEX IF NOT EXISTS idx_notifications_event_id ON notifications(event_id)`,
	}

	for _, table := range tables {
		if _, err := db.ExecContext(ctx, table); err != nil {
			return err
		}
	}
	return nil
}

// MockJetStream is a minimal mock that records published messages.
type MockJetStream struct {
	publishedMessages []publishedMessage
}

type publishedMessage struct {
	subject string
	data    []byte
}

func NewMockJetStream() *MockJetStream {
	return &MockJetStream{
		publishedMessages: []publishedMessage{},
	}
}

func (m *MockJetStream) Publish(ctx context.Context, subject string, data []byte, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	// opts are jetstream.PublishOpt values; in a real test we'd extract msgID,
	// but for this test we just verify the message was published
	_ = opts
	m.publishedMessages = append(m.publishedMessages, publishedMessage{
		subject: subject,
		data:    data,
	})
	return &jetstream.PubAck{}, nil
}

// TestHealthz tests the health check endpoint.
func TestHealthz(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	app := &App{db: db, js: NewMockJetStream()}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", app.handleHealthz)

	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/healthz")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	var result string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result != "ok" {
		t.Fatalf("Expected 'ok', got %q", result)
	}
}

// TestListEventsEmpty tests listing events when database is empty.
func TestListEventsEmpty(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	app := &App{db: db, js: NewMockJetStream()}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/events", app.handleListEvents)

	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/events")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	var events []EventResponse
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(events) != 0 {
		t.Fatalf("Expected 0 events, got %d", len(events))
	}
}

// TestGetEventWithNotifications tests getting an event with its notifications.
func TestGetEventWithNotifications(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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

	// Insert test notification
	_, err = db.ExecContext(ctx, `
		INSERT INTO notifications (event_id, status, telegram_message_id, attempted_at, confirmed_at, triggered_by)
		VALUES (?, ?, ?, ?, ?, ?)
	`, 98164, "sent", "1337", "2026-08-17T09:00:05Z", "2026-08-17T09:00:06Z", "scraper")
	if err != nil {
		t.Fatalf("Failed to insert test notification: %v", err)
	}

	// Test the getEvent function directly
	event, err := getEvent(ctx, db, 98164)
	if err != nil {
		t.Fatalf("Failed to get event: %v", err)
	}

	if event.ID != 98164 {
		t.Fatalf("Expected event ID 98164, got %d", event.ID)
	}

	if event.Status != "active" {
		t.Fatalf("Expected status 'active', got %q", event.Status)
	}

	if len(event.Notifications) != 1 {
		t.Fatalf("Expected 1 notification, got %d", len(event.Notifications))
	}
}

// TestGetEventNotFound tests getting a non-existent event.
func TestGetEventNotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	app := &App{db: db, js: NewMockJetStream()}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/events/{id}", app.handleGetEvent)

	server := httptest.NewServer(mux)
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
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/events/{id}", app.handleGetEvent)

	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/events/98164")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	var eventResp EventResponse
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
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/events", app.handleListEvents)

	server := httptest.NewServer(mux)
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

// TestListNotificationsInvalidStatus tests that an unrecognized notification
// status filter is rejected.
func TestListNotificationsInvalidStatus(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	app := &App{db: db, js: NewMockJetStream()}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/notifications", app.handleListNotifications)

	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/notifications?status=bogus")
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
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/events/{id}", app.handleGetEvent)

	server := httptest.NewServer(mux)
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
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/events", app.handleListEvents)

	server := httptest.NewServer(mux)
	defer server.Close()

	fetch := func(status string) []EventResponse {
		resp, err := http.Get(server.URL + "/api/events?status=" + status)
		if err != nil {
			t.Fatalf("Failed to make request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status=%s: expected 200, got %d", status, resp.StatusCode)
		}
		var events []EventResponse
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
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/events/{id}/retrigger", app.handleRetrigger)

	server := httptest.NewServer(mux)
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
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/events/{id}/retrigger", app.handleRetrigger)

	server := httptest.NewServer(mux)
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
	mux := http.NewServeMux()
	mux.HandleFunc("PATCH /api/events/{id}/purchased", app.handleSetPurchased)

	server := httptest.NewServer(mux)
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

	app := &App{db: db, js: NewMockJetStream()}
	mux := http.NewServeMux()
	mux.HandleFunc("PATCH /api/events/{id}/purchased", app.handleSetPurchased)
	mux.HandleFunc("GET /api/events/{id}", app.handleGetEvent)

	server := httptest.NewServer(mux)
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
	var ev EventResponse
	if err := json.NewDecoder(getResp.Body).Decode(&ev); err != nil {
		t.Fatalf("Failed to decode event: %v", err)
	}
	if !ev.Purchased {
		t.Fatalf("Expected purchased=true after PATCH, got false")
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
	var ev2 EventResponse
	if err := json.NewDecoder(getResp2.Body).Decode(&ev2); err != nil {
		t.Fatalf("Failed to decode event: %v", err)
	}
	if ev2.Purchased {
		t.Fatalf("Expected purchased=false after second PATCH, got true")
	}
}

// TestDeleteEventNotFound tests deleting a non-existent event.
func TestDeleteEventNotFound(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	app := &App{db: db, js: NewMockJetStream()}
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/events/{id}", app.handleDeleteEvent)

	server := httptest.NewServer(mux)
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
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/events/{id}", app.handleDeleteEvent)
	mux.HandleFunc("GET /api/events/{id}", app.handleGetEvent)

	server := httptest.NewServer(mux)
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

// TestUpdateMostRecentPendingRow tests the critical logic of updating the most recent pending row.
func TestUpdateMostRecentPendingRow(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Insert test event
	_, err := db.ExecContext(ctx, `
		INSERT INTO events (id, slug, title, venue, category, event_date, url, image_url, discovered_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, 98164, "test-event", "Test Event", "", "", "", "http://example.com", "", "2026-08-17T09:00:00Z")
	if err != nil {
		t.Fatalf("Failed to insert test event: %v", err)
	}

	// Insert two pending notifications for the same event
	_, err = db.ExecContext(ctx, `
		INSERT INTO notifications (event_id, status, triggered_by, attempted_at)
		VALUES (?, 'pending', 'scraper', ?)
	`, 98164, "2026-08-17T10:00:00Z")
	if err != nil {
		t.Fatalf("Failed to insert first notification: %v", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO notifications (event_id, status, triggered_by, attempted_at)
		VALUES (?, 'pending', 'scraper', ?)
	`, 98164, "2026-08-17T11:00:00Z")
	if err != nil {
		t.Fatalf("Failed to insert second notification: %v", err)
	}

	// Update the most recent one to sent
	sent := &event.NotificationSent{
		EventID:           98164,
		TelegramMessageID: "1337",
		SentAt:            time.Date(2026, 8, 17, 11, 30, 0, 0, time.UTC),
	}

	if err := updateNotificationSent(ctx, db, sent); err != nil {
		t.Fatalf("Failed to update notification: %v", err)
	}

	// Verify only the most recent one was updated
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM notifications WHERE event_id = ? AND status = 'sent'", 98164).Scan(&count); err != nil {
		t.Fatalf("Failed to query: %v", err)
	}

	if count != 1 {
		t.Fatalf("Expected 1 sent notification, got %d", count)
	}

	// Verify the older one is still pending
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM notifications WHERE event_id = ? AND status = 'pending'", 98164).Scan(&count); err != nil {
		t.Fatalf("Failed to query: %v", err)
	}

	if count != 1 {
		t.Fatalf("Expected 1 pending notification, got %d", count)
	}
}

// TestListNotifications tests the notifications endpoint.
func TestListNotifications(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Insert test event
	_, err := db.ExecContext(ctx, `
		INSERT INTO events (id, slug, title, url, discovered_at)
		VALUES (?, ?, ?, ?, ?)
	`, 98164, "test-event", "Test Event", "http://example.com", "2026-08-17T09:00:00Z")
	if err != nil {
		t.Fatalf("Failed to insert test event: %v", err)
	}

	// Insert notifications with different statuses
	_, err = db.ExecContext(ctx, `
		INSERT INTO notifications (event_id, status, telegram_message_id, attempted_at, confirmed_at, triggered_by)
		VALUES (?, ?, ?, ?, ?, ?)
	`, 98164, "sent", "1337", "2026-08-17T10:00:00Z", "2026-08-17T10:01:00Z", "scraper")
	if err != nil {
		t.Fatalf("Failed to insert sent notification: %v", err)
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO notifications (event_id, status, error, attempted_at, confirmed_at, triggered_by)
		VALUES (?, ?, ?, ?, ?, ?)
	`, 98164, "failed", "Telegram error", "2026-08-17T11:00:00Z", "2026-08-17T11:01:00Z", "manual-retry")
	if err != nil {
		t.Fatalf("Failed to insert failed notification: %v", err)
	}

	app := &App{db: db, js: NewMockJetStream()}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/notifications", app.handleListNotifications)

	server := httptest.NewServer(mux)
	defer server.Close()

	// Test listing all notifications
	resp, err := http.Get(server.URL + "/api/notifications")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	var notifs []NotificationResponse
	if err := json.NewDecoder(resp.Body).Decode(&notifs); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(notifs) != 2 {
		t.Fatalf("Expected 2 notifications, got %d", len(notifs))
	}

	// Test filtering by status
	resp, err = http.Get(server.URL + "/api/notifications?status=sent")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	var sentNotifs []NotificationResponse
	if err := json.NewDecoder(resp.Body).Decode(&sentNotifs); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(sentNotifs) != 1 {
		t.Fatalf("Expected 1 sent notification, got %d", len(sentNotifs))
	}

	if sentNotifs[0].Status != "sent" {
		t.Fatalf("Expected status 'sent', got %q", sentNotifs[0].Status)
	}
}

// TestManualRetryWithExistingPendingNotification tests that manual retrigger always inserts
// a new pending notification even when one already exists (ReasonManualRetry path).
// This is the critical test for the idempotency fix in upsertEventAndInsertNotification.
// This test calls upsertEventAndInsertNotification directly with ReasonManualRetry.
func TestManualRetryWithExistingPendingNotification(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Insert test event
	eventDate := "2026-08-25"
	_, err := db.ExecContext(ctx, `
		INSERT INTO events (id, slug, title, venue, category, event_date, url, image_url, discovered_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, 99999, "test-retrigger-event", "Test Retrigger Event",
		"Test Venue", "Test Category", eventDate,
		"https://example.com/event",
		"https://example.com/image.jpg",
		"2026-08-17T09:00:00Z")
	if err != nil {
		t.Fatalf("Failed to insert test event: %v", err)
	}

	// Insert an existing pending notification for this event (simulating a prior notification)
	_, err = db.ExecContext(ctx, `
		INSERT INTO notifications (event_id, status, triggered_by, attempted_at)
		VALUES (?, 'pending', 'scraper', ?)
	`, 99999, "2026-08-17T10:00:00Z")
	if err != nil {
		t.Fatalf("Failed to insert existing notification: %v", err)
	}

	// Verify we have 1 pending notification
	var initialCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM notifications WHERE event_id = ? AND status = 'pending'", 99999).Scan(&initialCount); err != nil {
		t.Fatalf("Failed to query initial count: %v", err)
	}
	if initialCount != 1 {
		t.Fatalf("Expected 1 initial pending notification, got %d", initialCount)
	}

	// Simulate a manual retrigger by calling upsertEventAndInsertNotification with ReasonManualRetry
	disc := &event.Discovered{
		EventID:   99999,
		Slug:      "test-retrigger-event",
		Title:     "Test Retrigger Event",
		Venue:     "Test Venue",
		Category:  "Test Category",
		EventDate: eventDate,
		URL:       "https://example.com/event",
		ImageURL:  "https://example.com/image.jpg",
		Reason:    event.ReasonManualRetry, // Critical: manual retrigger should always insert
	}

	if err := upsertEventAndInsertNotification(ctx, db, disc); err != nil {
		t.Fatalf("Failed to call upsertEventAndInsertNotification with ReasonManualRetry: %v", err)
	}

	// The critical assertion: verify a NEW pending notification was inserted
	// (not skipped due to the existing one). There should now be 2 pending rows.
	var finalCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM notifications WHERE event_id = ? AND status = 'pending'", 99999).Scan(&finalCount); err != nil {
		t.Fatalf("Failed to query final count: %v", err)
	}
	if finalCount != 2 {
		t.Fatalf("Expected 2 pending notifications after ReasonManualRetry, got %d. This means the manual-retry path did not insert a new row (bug!).", finalCount)
	}

	// Verify the triggered_by field was set to "manual-retry" for the new notification
	var manualRetryCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM notifications WHERE event_id = ? AND status = 'pending' AND triggered_by = 'manual-retry'", 99999).Scan(&manualRetryCount); err != nil {
		t.Fatalf("Failed to query manual-retry count: %v", err)
	}
	if manualRetryCount != 1 {
		t.Fatalf("Expected 1 notification with triggered_by='manual-retry', got %d", manualRetryCount)
	}
}

// TestDiscoveryIdempotencyWithExistingPending tests that ReasonDiscovered path
// still skips insertion when a pending notification already exists (original idempotency behavior).
func TestDiscoveryIdempotencyWithExistingPending(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Insert test event
	eventDate := "2026-08-28"
	_, err := db.ExecContext(ctx, `
		INSERT INTO events (id, slug, title, venue, category, event_date, url, image_url, discovered_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, 88888, "test-discovery-event", "Test Discovery Event",
		"Discovery Venue", "Discovery Category", eventDate,
		"https://example.com/discovery",
		"https://example.com/discovery.jpg",
		"2026-08-17T09:00:00Z")
	if err != nil {
		t.Fatalf("Failed to insert test event: %v", err)
	}

	// Insert an existing pending notification for this event
	_, err = db.ExecContext(ctx, `
		INSERT INTO notifications (event_id, status, triggered_by, attempted_at)
		VALUES (?, 'pending', 'scraper', ?)
	`, 88888, "2026-08-17T10:00:00Z")
	if err != nil {
		t.Fatalf("Failed to insert existing notification: %v", err)
	}

	// Verify we have 1 pending notification
	var initialCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM notifications WHERE event_id = ? AND status = 'pending'", 88888).Scan(&initialCount); err != nil {
		t.Fatalf("Failed to query initial count: %v", err)
	}
	if initialCount != 1 {
		t.Fatalf("Expected 1 initial pending notification, got %d", initialCount)
	}

	// Simulate a redelivery of a ReasonDiscovered event by calling upsertEventAndInsertNotification directly
	disc := &event.Discovered{
		EventID:   88888,
		Slug:      "test-discovery-event",
		Title:     "Test Discovery Event",
		Venue:     "Discovery Venue",
		Category:  "Discovery Category",
		EventDate: eventDate,
		URL:       "https://example.com/discovery",
		ImageURL:  "https://example.com/discovery.jpg",
		Reason:    event.ReasonDiscovered, // Critical: this is the discovery path
	}

	if err := upsertEventAndInsertNotification(ctx, db, disc); err != nil {
		t.Fatalf("Failed to call upsertEventAndInsertNotification: %v", err)
	}

	// The critical assertion: verify NO new notification was inserted (idempotency check worked).
	// There should still be only 1 pending row, not 2.
	var finalCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM notifications WHERE event_id = ? AND status = 'pending'", 88888).Scan(&finalCount); err != nil {
		t.Fatalf("Failed to query final count: %v", err)
	}
	if finalCount != 1 {
		t.Fatalf("Expected 1 pending notification after ReasonDiscovered redelivery (idempotency should skip), got %d. The original dedup fix was broken!", finalCount)
	}

	// Verify the existing notification is still from 'scraper' (not replaced)
	var scraperCount int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM notifications WHERE event_id = ? AND status = 'pending' AND triggered_by = 'scraper'", 88888).Scan(&scraperCount); err != nil {
		t.Fatalf("Failed to query scraper count: %v", err)
	}
	if scraperCount != 1 {
		t.Fatalf("Expected 1 notification with triggered_by='scraper', got %d", scraperCount)
	}
}

// TestComputeStatusEventDateWithTime verifies computeStatus can parse the
// "YYYY-MM-DDTHH:MM" event_date format the scraper stores for hub sessions
// that have a fixed start time (no seconds/timezone offset), not just plain
// "YYYY-MM-DD".
func TestComputeStatusEventDateWithTime(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)

	futureToday := "2026-09-04T18:30"
	if status := computeStatus(&futureToday, now); status != "active" {
		t.Errorf("computeStatus(%q) = %q, want %q", futureToday, status, "active")
	}

	past := "2026-09-03T18:30"
	if status := computeStatus(&past, now); status != "finished" {
		t.Errorf("computeStatus(%q) = %q, want %q", past, status, "finished")
	}
}
