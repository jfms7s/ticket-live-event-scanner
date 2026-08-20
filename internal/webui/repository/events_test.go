package repository

import (
	"context"
	"testing"
	"time"
)

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

	// Test the GetEvent function directly
	event, err := GetEvent(ctx, db, 98164)
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
