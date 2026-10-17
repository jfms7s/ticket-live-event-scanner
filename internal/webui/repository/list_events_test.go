package repository

import (
	"context"
	"testing"
	"time"
)

func TestListEventsAttachesEachEventsOwnNotificationsNewestFirst(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	for i, id := range []int64{1, 2, 3} {
		if _, err := db.ExecContext(ctx, `INSERT INTO events (id, slug, title, url, event_date, discovered_at) VALUES (?, ?, ?, ?, ?, ?)`,
			id, "e", "Event", "https://x", "2030-01-01", time.Date(2026, 8, 1+i, 0, 0, 0, 0, time.UTC).Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []struct {
		event int64
		at    string
		msg   string
	}{
		{1, "2026-08-01T10:00:00Z", "old"},
		{1, "2026-08-02T10:00:00Z", "new"},
		{3, "2026-08-03T10:00:00Z", "only"},
	} {
		if _, err := db.ExecContext(ctx, `INSERT INTO notifications (event_id, status, telegram_message_id, attempted_at, triggered_by) VALUES (?, 'sent', ?, ?, 'scraper')`,
			n.event, n.msg, n.at); err != nil {
			t.Fatal(err)
		}
	}

	events, err := ListEvents(ctx, db, "", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	got := map[int64][]string{}
	for _, e := range events {
		if e.Notifications == nil {
			t.Errorf("event %d: notifications is nil, want empty slice", e.ID)
		}
		for _, n := range e.Notifications {
			got[e.ID] = append(got[e.ID], *n.TelegramMessageID)
		}
	}
	want := map[int64][]string{1: {"new", "old"}, 3: {"only"}}
	if len(got) != len(want) || len(got[1]) != 2 || got[1][0] != "new" || got[1][1] != "old" || len(got[3]) != 1 || got[3][0] != "only" || len(got[2]) != 0 {
		t.Fatalf("notifications per event = %v, want %v (event 2 none)", got, want)
	}
}
