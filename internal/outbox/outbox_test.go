package outbox

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/store"
)

func newQueue(t *testing.T) *Queue {
	t.Helper()
	db, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "x.db"), AllowEmpty: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &Queue{DB: db}
}

func hubOf(t *testing.T, db *sql.DB, id int64) sql.NullString {
	t.Helper()
	var h sql.NullString
	if err := db.QueryRow(`SELECT hub_slug FROM events WHERE id = ?`, id).Scan(&h); err != nil {
		t.Fatal(err)
	}
	return h
}

func disc(id int64, hub string) *event.Discovered {
	return &event.Discovered{EventID: id, Slug: "s", Title: "T", URL: "https://x", Reason: event.ReasonDiscovered, HubSlug: hub}
}

func TestHubSlugIsStoredKeptWhenAbsentAndMovedWhenGiven(t *testing.T) {
	q := newQueue(t)
	ctx := context.Background()
	if _, err := q.EnqueueDiscovered(ctx, disc(7, "hub-500")); err != nil {
		t.Fatal(err)
	}
	if h := hubOf(t, q.DB, 7); h.String != "hub-500" {
		t.Fatalf("hub_slug = %v, want hub-500", h)
	}
	if _, err := q.EnqueueDiscovered(ctx, disc(7, "")); err != nil {
		t.Fatal(err)
	}
	if h := hubOf(t, q.DB, 7); h.String != "hub-500" {
		t.Errorf("hub_slug after a message without a hub = %v, want hub-500", h)
	}
	if _, err := q.EnqueueDiscovered(ctx, disc(7, "hub-600")); err != nil {
		t.Fatal(err)
	}
	if h := hubOf(t, q.DB, 7); h.String != "hub-600" {
		t.Errorf("hub_slug after rediscovery on another hub = %v, want hub-600", h)
	}
}

func TestEventWithoutAHubHasNullHubSlug(t *testing.T) {
	q := newQueue(t)
	if _, err := q.EnqueueDiscovered(context.Background(), disc(8, "")); err != nil {
		t.Fatal(err)
	}
	if h := hubOf(t, q.DB, 8); h.Valid {
		t.Errorf("hub_slug = %v, want NULL", h)
	}
}

func TestRediscoveringTwiceQueuesOnce(t *testing.T) {
	q := newQueue(t)
	ctx := context.Background()
	first, err := q.EnqueueDiscovered(ctx, disc(9, ""))
	second, err2 := q.EnqueueDiscovered(ctx, disc(9, ""))
	if err != nil || err2 != nil || !first || second {
		t.Fatalf("queued = %v, %v (errors %v, %v), want true, false", first, second, err, err2)
	}
}
