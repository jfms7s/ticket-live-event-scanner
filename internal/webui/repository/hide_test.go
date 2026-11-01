package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
)

// TestHideEventKeepsRowOutOfTheAPI pins what Remove means: the event leaves
// the dashboard and the API, but its row and notifications stay, because
// the row is also the record that the event was already notified.
func TestHideEventKeepsRowOutOfTheAPI(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()
	now := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	for _, id := range []int64{1, 2} {
		if _, err := db.ExecContext(ctx, `INSERT INTO events (id, slug, title, url, event_date) VALUES (?, 'e', 'Event', 'https://x', '2030-01-01')`, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO notifications (event_id, status, triggered_by) VALUES (1, 'sent', 'scraper')`); err != nil {
		t.Fatal(err)
	}

	if err := HideEvent(ctx, db, 1); err != nil {
		t.Fatalf("HideEvent: %v", err)
	}

	events, err := ListEvents(ctx, db, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].ID != 2 {
		t.Fatalf("ListEvents after hiding 1 = %v, want only event 2", events)
	}

	if _, err := GetEvent(ctx, db, 1, now); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("GetEvent(hidden) err = %v, want sql.ErrNoRows", err)
	}
	if err := SetEventPurchased(ctx, db, 1, true); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("SetEventPurchased(hidden) err = %v, want sql.ErrNoRows", err)
	}

	var rows, notifs int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE id = 1 AND hidden = 1`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE event_id = 1`).Scan(&notifs); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || notifs != 1 {
		t.Errorf("after hide: hidden rows = %d, notifications = %d; want 1 and 1 (kept as the dedup record)", rows, notifs)
	}

	if err := HideEvent(ctx, db, 1); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("HideEvent twice err = %v, want sql.ErrNoRows", err)
	}
	if err := HideEvent(ctx, db, 999); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("HideEvent(missing) err = %v, want sql.ErrNoRows", err)
	}
}

// TestRediscoveredHiddenEventStaysHidden covers a late or replayed
// events.discovered message for a removed event: the upsert refreshes the
// row but must not bring it back to the dashboard.
func TestRediscoveredHiddenEventStaysHidden(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	disc := &event.Discovered{EventID: 7, Slug: "s-7", Title: "Seven", URL: "https://x/7", Reason: event.ReasonDiscovered}
	if err := UpsertEventAndInsertNotification(ctx, db, disc, 0); err != nil {
		t.Fatal(err)
	}
	if err := HideEvent(ctx, db, 7); err != nil {
		t.Fatal(err)
	}
	if err := UpsertEventAndInsertNotification(ctx, db, disc, 0); err != nil {
		t.Fatal(err)
	}

	events, err := ListEvents(ctx, db, "", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("ListEvents = %v, want none: a rediscovered hidden event must stay hidden", events)
	}
}
