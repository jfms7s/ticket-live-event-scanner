package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
)

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

	if err := UpdateNotificationSent(ctx, db, sent); err != nil {
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

// TestManualRetryWithExistingPendingNotification tests that manual retrigger always inserts
// a new pending notification even when one already exists (ReasonManualRetry path).
// This is the critical test for the idempotency fix in UpsertEventAndInsertNotification.
// This test calls UpsertEventAndInsertNotification directly with ReasonManualRetry.
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

	// Simulate a manual retrigger by calling UpsertEventAndInsertNotification with ReasonManualRetry
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

	if err := UpsertEventAndInsertNotification(ctx, db, disc); err != nil {
		t.Fatalf("Failed to call UpsertEventAndInsertNotification with ReasonManualRetry: %v", err)
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

	// Simulate a redelivery of a ReasonDiscovered event by calling UpsertEventAndInsertNotification directly
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

	if err := UpsertEventAndInsertNotification(ctx, db, disc); err != nil {
		t.Fatalf("Failed to call UpsertEventAndInsertNotification: %v", err)
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
