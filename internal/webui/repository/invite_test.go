package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func insertInviteTestEvent(t *testing.T, db *sql.DB, id int64) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO events (id, slug, title, url, discovered_at)
		VALUES (?, 'slug', 'Title', 'http://example.com', '2026-08-17T09:00:00Z')
	`, id); err != nil {
		t.Fatalf("insert event: %v", err)
	}
}

func readInvite(t *testing.T, db *sql.DB, id int64) (status, errMsg *string) {
	t.Helper()
	ev, err := GetEvent(context.Background(), db, id, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	return ev.InviteStatus, ev.InviteError
}

func strOrNil(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}

func TestNewEventHasNoInviteStatus(t *testing.T) {
	db := setupTestDB(t)
	insertInviteTestEvent(t, db, 1)

	status, errMsg := readInvite(t, db, 1)
	if status != nil || errMsg != nil {
		t.Fatalf("invite = (%s, %s), want (<nil>, <nil>)", strOrNil(status), strOrNil(errMsg))
	}
}

func TestSetEventPurchasedMarksInvitePending(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	insertInviteTestEvent(t, db, 1)

	if _, err := SetEventPurchased(ctx, db, 1, true); err != nil {
		t.Fatalf("SetEventPurchased: %v", err)
	}
	if status, _ := readInvite(t, db, 1); strOrNil(status) != "pending" {
		t.Fatalf("invite_status = %s, want pending", strOrNil(status))
	}

	// Recording the purchase again while it is already recorded changes
	// nothing: no new invite is published, so the invite state stays.
	if err := UpdateInviteFailed(ctx, db, 1, "smtp down"); err != nil {
		t.Fatalf("UpdateInviteFailed: %v", err)
	}
	changed, err := SetEventPurchased(ctx, db, 1, true)
	if err != nil {
		t.Fatalf("SetEventPurchased again: %v", err)
	}
	if changed {
		t.Errorf("SetEventPurchased(true) on a purchased event reported changed")
	}
	if status, _ := readInvite(t, db, 1); strOrNil(status) != "failed" {
		t.Fatalf("invite_status = %s, want failed (unchanged)", strOrNil(status))
	}

	// A failed invite is reset to pending (with its error cleared) by a
	// real re-purchase (untick, tick), since that publishes a new invite.
	if _, err := SetEventPurchased(ctx, db, 1, false); err != nil {
		t.Fatalf("SetEventPurchased(false): %v", err)
	}
	changed, err = SetEventPurchased(ctx, db, 1, true)
	if err != nil || !changed {
		t.Fatalf("SetEventPurchased re-purchase = (%v, %v), want (true, nil)", changed, err)
	}
	status, errMsg := readInvite(t, db, 1)
	if strOrNil(status) != "pending" || errMsg != nil {
		t.Fatalf("invite = (%s, %s), want (pending, <nil>)", strOrNil(status), strOrNil(errMsg))
	}
}

func TestSetEventPurchasedReportsChange(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	insertInviteTestEvent(t, db, 1)

	for i, step := range []struct {
		purchased, want bool
	}{
		{false, false}, // already not purchased
		{true, true},
		{true, false},
		{false, true},
		{false, false},
	} {
		changed, err := SetEventPurchased(ctx, db, 1, step.purchased)
		if err != nil {
			t.Fatalf("step %d: SetEventPurchased(%v): %v", i, step.purchased, err)
		}
		if changed != step.want {
			t.Errorf("step %d: SetEventPurchased(%v) changed = %v, want %v", i, step.purchased, changed, step.want)
		}
	}

	if _, err := SetEventPurchased(ctx, db, 404, true); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("SetEventPurchased(unknown) err = %v, want sql.ErrNoRows", err)
	}
}

func TestSetEventNotPurchasedKeepsInviteStatus(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	insertInviteTestEvent(t, db, 1)

	if _, err := SetEventPurchased(ctx, db, 1, true); err != nil {
		t.Fatalf("SetEventPurchased: %v", err)
	}
	if err := UpdateInviteSent(ctx, db, 1); err != nil {
		t.Fatalf("UpdateInviteSent: %v", err)
	}
	if _, err := SetEventPurchased(ctx, db, 1, false); err != nil {
		t.Fatalf("SetEventPurchased(false): %v", err)
	}
	// The invite already went out; unticking doesn't unsend it.
	if status, _ := readInvite(t, db, 1); strOrNil(status) != "sent" {
		t.Fatalf("invite_status = %s, want sent", strOrNil(status))
	}
}

func TestUpdateInviteSentFromPending(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	insertInviteTestEvent(t, db, 1)
	if _, err := SetEventPurchased(ctx, db, 1, true); err != nil {
		t.Fatalf("SetEventPurchased: %v", err)
	}

	if err := UpdateInviteSent(ctx, db, 1); err != nil {
		t.Fatalf("UpdateInviteSent: %v", err)
	}
	status, errMsg := readInvite(t, db, 1)
	if strOrNil(status) != "sent" || errMsg != nil {
		t.Fatalf("invite = (%s, %s), want (sent, <nil>)", strOrNil(status), strOrNil(errMsg))
	}
}

func TestUpdateInviteFailedFromPending(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	insertInviteTestEvent(t, db, 1)
	if _, err := SetEventPurchased(ctx, db, 1, true); err != nil {
		t.Fatalf("SetEventPurchased: %v", err)
	}

	if err := UpdateInviteFailed(ctx, db, 1, "smtp: 550 mailbox unavailable"); err != nil {
		t.Fatalf("UpdateInviteFailed: %v", err)
	}
	status, errMsg := readInvite(t, db, 1)
	if strOrNil(status) != "failed" || strOrNil(errMsg) != "smtp: 550 mailbox unavailable" {
		t.Fatalf("invite = (%s, %s), want (failed, smtp: 550 mailbox unavailable)", strOrNil(status), strOrNil(errMsg))
	}
}

// Results only resolve a pending invite: a redelivered result is a no-op,
// and an event purchased before invite tracking (NULL) stays unknown.
func TestInviteResultsOnlyResolvePending(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	insertInviteTestEvent(t, db, 1) // never purchased: NULL
	if err := UpdateInviteSent(ctx, db, 1); err != nil {
		t.Fatalf("UpdateInviteSent on NULL: %v", err)
	}
	if err := UpdateInviteFailed(ctx, db, 1, "x"); err != nil {
		t.Fatalf("UpdateInviteFailed on NULL: %v", err)
	}
	if status, _ := readInvite(t, db, 1); status != nil {
		t.Fatalf("invite_status = %s, want <nil>", strOrNil(status))
	}

	insertInviteTestEvent(t, db, 2)
	if _, err := SetEventPurchased(ctx, db, 2, true); err != nil {
		t.Fatalf("SetEventPurchased: %v", err)
	}
	if err := UpdateInviteSent(ctx, db, 2); err != nil {
		t.Fatalf("UpdateInviteSent: %v", err)
	}
	if err := UpdateInviteFailed(ctx, db, 2, "late"); err != nil {
		t.Fatalf("UpdateInviteFailed: %v", err)
	}
	status, errMsg := readInvite(t, db, 2)
	if strOrNil(status) != "sent" || errMsg != nil {
		t.Fatalf("invite = (%s, %s), want (sent, <nil>)", strOrNil(status), strOrNil(errMsg))
	}

	// Unknown event: no error, nothing to update.
	if err := UpdateInviteFailed(ctx, db, 404, "x"); err != nil {
		t.Fatalf("UpdateInviteFailed on unknown event: %v", err)
	}
}

func TestListEventsIncludesInvite(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	insertInviteTestEvent(t, db, 1)
	if _, err := SetEventPurchased(ctx, db, 1, true); err != nil {
		t.Fatalf("SetEventPurchased: %v", err)
	}
	if err := UpdateInviteFailed(ctx, db, 1, "boom"); err != nil {
		t.Fatalf("UpdateInviteFailed: %v", err)
	}

	events, err := ListEvents(ctx, db, "", time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if strOrNil(events[0].InviteStatus) != "failed" || strOrNil(events[0].InviteError) != "boom" {
		t.Fatalf("invite = (%s, %s), want (failed, boom)", strOrNil(events[0].InviteStatus), strOrNil(events[0].InviteError))
	}
}
