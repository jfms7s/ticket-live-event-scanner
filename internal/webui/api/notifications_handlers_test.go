package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/webui/repository"
)

// TestListNotificationsInvalidStatus tests that an unrecognized notification
// status filter is rejected.
func TestListNotificationsInvalidStatus(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	app := &App{db: db, js: NewMockJetStream()}
	server := httptest.NewServer(app.NewMux())
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
	server := httptest.NewServer(app.NewMux())
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

	var notifs []repository.NotificationResponse
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

	var sentNotifs []repository.NotificationResponse
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
