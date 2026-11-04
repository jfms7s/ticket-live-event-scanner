package repository

import (
	"context"
	"testing"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
)

// TestHubSlugIsStoredAndSurvivesARetry pins that the hub which found an event
// is stored, listed, and not blanked by a manual retry, which carries no hub
// for an event stored before hubs were recorded.
func TestHubSlugIsStoredAndSurvivesARetry(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	disc := &event.Discovered{EventID: 7, Slug: "s-7", Title: "Seven", URL: "https://x/7", Reason: event.ReasonDiscovered, HubSlug: "hub-500"}
	if err := UpsertEventAndInsertNotification(ctx, db, disc, 1); err != nil {
		t.Fatal(err)
	}
	listed := func() *string {
		events, err := ListEvents(ctx, db, "", time.Now())
		if err != nil || len(events) != 1 {
			t.Fatalf("ListEvents = %v, %v", events, err)
		}
		return events[0].HubSlug
	}
	if got := listed(); got == nil || *got != "hub-500" {
		t.Fatalf("hub_slug = %v, want hub-500", got)
	}

	retry := *disc
	retry.Reason, retry.HubSlug = event.ReasonManualRetry, ""
	if err := UpsertEventAndInsertNotification(ctx, db, &retry, 2); err != nil {
		t.Fatal(err)
	}
	if got := listed(); got == nil || *got != "hub-500" {
		t.Errorf("hub_slug after a retry without a hub = %v, want hub-500", got)
	}

	moved := *disc
	moved.HubSlug = "hub-600"
	if err := UpsertEventAndInsertNotification(ctx, db, &moved, 3); err != nil {
		t.Fatal(err)
	}
	if got := listed(); got == nil || *got != "hub-600" {
		t.Errorf("hub_slug after rediscovery on another hub = %v, want hub-600", got)
	}
}

// TestEventWithoutAHubHasNullHubSlug: events stored before hubs were
// recorded (or retried before ever having one) report no hub.
func TestEventWithoutAHubHasNullHubSlug(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	disc := &event.Discovered{EventID: 8, Slug: "s-8", Title: "Eight", URL: "https://x/8", Reason: event.ReasonDiscovered}
	if err := UpsertEventAndInsertNotification(ctx, db, disc, 1); err != nil {
		t.Fatal(err)
	}
	events, err := ListEvents(ctx, db, "", time.Now())
	if err != nil || len(events) != 1 {
		t.Fatalf("ListEvents = %v, %v", events, err)
	}
	if events[0].HubSlug != nil {
		t.Errorf("hub_slug = %q, want nil", *events[0].HubSlug)
	}
}
