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
// row's delivery_seq, and a sent result carrying that sequence resolves it
// even when the result is consumed first.
func TestDiscoveredStoresStreamSequenceAndResultMatchesIt(t *testing.T) {
	db := setupDB(t)
	ctx := context.Background()

	sent := jsonMsg(t, streams.NotificationsSentSubject, event.NotificationSent{
		EventID: 1, TelegramMessageID: "42", SentAt: time.Now(), DeliverySeq: 77,
	})
	handleNotification(ctx, db, sent)
	sent.assertCalls(t, 1, 0, 0)

	disc := discoveredMsg(t, 1, 77)
	handleDiscovered(ctx, db, disc)
	disc.assertCalls(t, 1, 0, 0)

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
