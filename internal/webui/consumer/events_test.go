package consumer

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/nats-io/nats.go/jetstream"
)

func discoveredMsg(t *testing.T, id int64, seq uint64) *fakeMsg {
	t.Helper()
	m := jsonMsg(t, streams.EventsSubject, event.Discovered{
		EventID: id, Slug: "s", Title: "T", URL: "http://example.com", Reason: event.ReasonDiscovered,
	})
	m.meta = &jetstream.MsgMetadata{Sequence: jetstream.SequencePair{Stream: seq}}
	return m
}

func notificationRows(t *testing.T, db *sql.DB, id int64) (statuses []string, seqs []sql.NullInt64) {
	t.Helper()
	rows, err := db.Query(`SELECT status, delivery_seq FROM notifications WHERE event_id = ? ORDER BY id`, id)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var s string
		var q sql.NullInt64
		if err := rows.Scan(&s, &q); err != nil {
			t.Fatalf("scan: %v", err)
		}
		statuses = append(statuses, s)
		seqs = append(seqs, q)
	}
	return statuses, seqs
}

// The events consumer stores the message's EVENTS stream sequence as the
// row's delivery_seq. A sent result consumed before it is nak'd with a
// delay (its event isn't stored yet); its redelivery resolves the row.
func TestDiscoveredStoresStreamSequenceAndResultMatchesIt(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()
	result := event.NotificationSent{EventID: 1, TelegramMessageID: "42", SentAt: time.Now(), DeliverySeq: 77}

	early := jsonMsg(t, streams.NotificationsSentSubject, result)
	handleNotification(ctx, db, early)
	early.assertCalls(t, 0, 1, 0)

	disc := discoveredMsg(t, 1, 77)
	handleDiscovered(ctx, db, disc)
	disc.assertCalls(t, 1, 0, 0)

	redelivered := jsonMsg(t, streams.NotificationsSentSubject, result)
	handleNotification(ctx, db, redelivered)
	redelivered.assertCalls(t, 1, 0, 0)

	statuses, seqs := notificationRows(t, db, 1)
	if len(statuses) != 1 || statuses[0] != "sent" || seqs[0].Int64 != 77 {
		t.Fatalf("rows = %v %v, want one sent row with delivery_seq 77", statuses, seqs)
	}
}

// Without metadata the row is recorded with no delivery_seq.
func TestDiscoveredWithoutMetadataHasNoDeliverySeq(t *testing.T) {
	db := setupDB(t)
	m := discoveredMsg(t, 1, 0)
	m.meta = nil
	m.metaErr = errors.New("no metadata")
	handleDiscovered(context.Background(), db, m)
	m.assertCalls(t, 1, 0, 0)

	statuses, seqs := notificationRows(t, db, 1)
	if len(statuses) != 1 || statuses[0] != "pending" || seqs[0].Valid {
		t.Fatalf("rows = %v %v, want one pending row with NULL delivery_seq", statuses, seqs)
	}
}

func TestMalformedDiscoveredIsTerminated(t *testing.T) {
	db := setupDB(t)
	m := &fakeMsg{subject: streams.EventsSubject, data: []byte("{nope")}
	handleDiscovered(context.Background(), db, m)
	m.assertCalls(t, 0, 0, 1)
}

func TestMaterializeRetryDelay(t *testing.T) {
	want := map[uint64]time.Duration{1: 15 * time.Second, 2: 30 * time.Second, 3: time.Minute, 4: 2 * time.Minute, 5: 4 * time.Minute, 6: 5 * time.Minute, 40: 5 * time.Minute}
	for n, d := range want {
		if got := materializeRetryDelay(n); got != d {
			t.Errorf("materializeRetryDelay(%d) = %v, want %v", n, got, d)
		}
	}
}

// A database error is retried with the linear backoff (5s at the first
// delivery, 10s at the second), and the last allowed delivery is
// terminated with an ERROR log instead of being dropped silently.
func TestDiscoveredDBErrorBacksOffThenTerminates(t *testing.T) {
	db := setupDB(t)
	db.Close()

	for _, tc := range []struct {
		delivered uint64
		delay     time.Duration
	}{{1, 5 * time.Second}, {2, 10 * time.Second}} {
		m := discoveredMsg(t, 1, 9)
		m.meta.NumDelivered = tc.delivered
		handleDiscovered(context.Background(), db, m)
		m.assertCalls(t, 0, 1, 0)
		m.assertNakDelay(t, tc.delay)
	}

	last := discoveredMsg(t, 1, 9)
	last.meta.NumDelivered = streams.DefaultMaxDeliver
	handleDiscovered(context.Background(), db, last)
	last.assertCalls(t, 0, 0, 1)
}

func TestDiscoveredSuccessIsAcked(t *testing.T) {
	db := setupDB(t)
	m := discoveredMsg(t, 1, 3)
	handleDiscovered(context.Background(), db, m)
	m.assertCalls(t, 1, 0, 0)
	if m.plainNaks != 0 || len(m.nakDelays) != 0 {
		t.Fatalf("unexpected naks on success: %d plain, %v delayed", m.plainNaks, m.nakDelays)
	}
}
