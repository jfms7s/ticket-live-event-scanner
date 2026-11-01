package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
)

// notifRow is one notifications row as these tests check it.
type notifRow struct {
	status      string
	deliverySeq sql.NullInt64
	messageID   sql.NullString
	errMsg      sql.NullString
	triggeredBy string
}

// notifRows returns the event's notification rows in insert order.
func notifRows(t *testing.T, db *sql.DB, eventID int64) []notifRow {
	t.Helper()
	rows, err := db.Query(`SELECT status, delivery_seq, telegram_message_id, error, triggered_by
		FROM notifications WHERE event_id = ? ORDER BY id`, eventID)
	if err != nil {
		t.Fatalf("query notifications: %v", err)
	}
	defer rows.Close()
	var out []notifRow
	for rows.Next() {
		var r notifRow
		if err := rows.Scan(&r.status, &r.deliverySeq, &r.messageID, &r.errMsg, &r.triggeredBy); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

// statusBySeq maps delivery_seq to status (0 for NULL).
func statusBySeq(rows []notifRow) map[int64]string {
	m := map[int64]string{}
	for _, r := range rows {
		m[r.deliverySeq.Int64] = r.status
	}
	return m
}

func discovered(id int64, reason string) *event.Discovered {
	return &event.Discovered{EventID: id, Slug: "s", Title: "T", URL: "https://example.test/e", Reason: reason}
}

func sentResult(id int64, seq uint64, msgID string) *event.NotificationSent {
	return &event.NotificationSent{EventID: id, TelegramMessageID: msgID, SentAt: time.Now(), DeliverySeq: seq}
}

func failedResult(id int64, seq uint64) *event.NotificationFailed {
	return &event.NotificationFailed{EventID: id, Error: "telegram 500", FailedAt: time.Now(), Attempts: 5, DeliverySeq: seq}
}

func mustUpsert(t *testing.T, db *sql.DB, d *event.Discovered, seq uint64) {
	t.Helper()
	if err := UpsertEventAndInsertNotification(context.Background(), db, d, seq); err != nil {
		t.Fatalf("UpsertEventAndInsertNotification(seq %d): %v", seq, err)
	}
}

func mustSent(t *testing.T, db *sql.DB, s *event.NotificationSent) {
	t.Helper()
	if err := UpdateNotificationSent(context.Background(), db, s); err != nil {
		t.Fatalf("UpdateNotificationSent: %v", err)
	}
}

func mustFailed(t *testing.T, db *sql.DB, f *event.NotificationFailed) {
	t.Helper()
	if err := UpdateNotificationFailed(context.Background(), db, f); err != nil {
		t.Fatalf("UpdateNotificationFailed: %v", err)
	}
}

// QA scenario 1: the sent result is consumed before its events.discovered
// message (they arrive on independent consumers). For a new event there is
// no event row yet, so the result is refused for redelivery; once the
// discovered message is in, the redelivered result resolves the row.
func TestDeliverySeq_SentBeforeDiscoveredEndsSent(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	err := UpdateNotificationSent(context.Background(), db, sentResult(1, 10, "42"))
	if !errors.Is(err, ErrEventNotMaterialized) {
		t.Fatalf("result before its event: err = %v, want ErrEventNotMaterialized", err)
	}
	if rows := notifRows(t, db, 1); len(rows) != 0 {
		t.Fatalf("refused result wrote rows: %+v", rows)
	}

	mustUpsert(t, db, discovered(1, event.ReasonDiscovered), 10)
	mustSent(t, db, sentResult(1, 10, "42")) // redelivery

	rows := notifRows(t, db, 1)
	if len(rows) != 1 {
		t.Fatalf("rows = %+v, want exactly one", rows)
	}
	r := rows[0]
	if r.status != "sent" || r.deliverySeq.Int64 != 10 || r.messageID.String != "42" || r.triggeredBy != "scraper" {
		t.Errorf("row = %+v, want sent, seq 10, message 42, triggered_by scraper", r)
	}
}

// When the event is already known (a manual retry), a result that arrives
// before its discovered message creates the row, and the discovered upsert
// then leaves its status alone.
func TestDeliverySeq_ResultBeforeRetryCreatesRow(t *testing.T) {
	for _, status := range []string{"sent", "failed"} {
		t.Run(status, func(t *testing.T) {
			db := setupTestDB(t)
			defer db.Close()

			mustUpsert(t, db, discovered(1, event.ReasonDiscovered), 9)
			mustSent(t, db, sentResult(1, 9, "first"))

			if status == "sent" {
				mustSent(t, db, sentResult(1, 10, "42"))
			} else {
				mustFailed(t, db, failedResult(1, 10))
			}
			mustUpsert(t, db, discovered(1, event.ReasonManualRetry), 10)

			rows := notifRows(t, db, 1)
			if len(rows) != 2 {
				t.Fatalf("rows = %+v, want two", rows)
			}
			r := rows[1]
			if r.status != status || r.deliverySeq.Int64 != 10 || r.triggeredBy != "manual-retry" {
				t.Errorf("retry row = %+v, want %s, seq 10, manual-retry", r, status)
			}
		})
	}
}

// With foreign keys enforced (sqld may enforce them), the result-first
// paths still work: nothing is inserted before the event exists.
func TestDeliverySeq_ForeignKeysOn(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
	var fk int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("foreign_keys = %d (%v), want 1", fk, err)
	}

	if err := UpdateNotificationFailed(context.Background(), db, failedResult(1, 10)); !errors.Is(err, ErrEventNotMaterialized) {
		t.Fatalf("err = %v, want ErrEventNotMaterialized", err)
	}
	mustUpsert(t, db, discovered(1, event.ReasonDiscovered), 10)
	mustSent(t, db, sentResult(1, 11, "retry")) // a later message's result, event known
	mustUpsert(t, db, discovered(1, event.ReasonManualRetry), 11)
	mustFailed(t, db, failedResult(1, 10))

	got := statusBySeq(notifRows(t, db, 1))
	if len(got) != 2 || got[10] != "failed" || got[11] != "sent" {
		t.Fatalf("rows by seq = %v, want 10 failed and 11 sent", got)
	}
}

// QA scenario 2: events.discovered is redelivered after its row was
// resolved (lost ack, restart). No orphan pending row may appear.
func TestDeliverySeq_DiscoveredRedeliveredAfterResolveAddsNoPendingRow(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	d := discovered(1, event.ReasonDiscovered)
	mustUpsert(t, db, d, 10)
	mustSent(t, db, sentResult(1, 10, "42"))
	mustUpsert(t, db, d, 10) // redelivery

	rows := notifRows(t, db, 1)
	if len(rows) != 1 || rows[0].status != "sent" {
		t.Fatalf("rows = %+v, want one sent row", rows)
	}
}

// QA scenario 3: two manual retries in the same second. Each result must
// resolve the row of its own message, not whichever wins a timestamp tie.
func TestDeliverySeq_TwoManualRetriesSameSecondResolveTheirOwnRows(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	mustUpsert(t, db, discovered(1, event.ReasonManualRetry), 11)
	mustUpsert(t, db, discovered(1, event.ReasonManualRetry), 12)

	mustSent(t, db, sentResult(1, 11, "first"))
	got := statusBySeq(notifRows(t, db, 1))
	if got[11] != "sent" || got[12] != "pending" {
		t.Fatalf("after sent for seq 11: %v, want 11 sent and 12 pending", got)
	}

	mustFailed(t, db, failedResult(1, 12))
	got = statusBySeq(notifRows(t, db, 1))
	if got[11] != "sent" || got[12] != "failed" {
		t.Fatalf("after failed for seq 12: %v, want 11 sent and 12 failed", got)
	}
}

// A failed result for a message never overwrites its sent row: Telegram did
// deliver it (e.g. a sent publish that errored client-side but landed).
func TestDeliverySeq_FailedDoesNotOverwriteSent(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	mustUpsert(t, db, discovered(1, event.ReasonDiscovered), 20)
	mustSent(t, db, sentResult(1, 20, "42"))
	mustFailed(t, db, failedResult(1, 20))

	rows := notifRows(t, db, 1)
	if len(rows) != 1 || rows[0].status != "sent" || rows[0].messageID.String != "42" {
		t.Fatalf("rows = %+v, want one sent row with message 42", rows)
	}

	// Redelivered sent result is idempotent.
	mustSent(t, db, sentResult(1, 20, "42"))
	if rows := notifRows(t, db, 1); len(rows) != 1 || rows[0].status != "sent" {
		t.Fatalf("after redelivered sent: %+v", rows)
	}
}

// A result with a delivery_seq resolves a pending row written before
// delivery_seq existed (NULL), instead of adding a second row and leaving
// the old one pending; the row adopts the sequence, so a later redelivery
// of the discovered message still finds it.
func TestDeliverySeq_ResultAdoptsLegacyPendingRow(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	mustUpsert(t, db, discovered(1, event.ReasonDiscovered), 0) // legacy row, delivery_seq NULL
	mustSent(t, db, sentResult(1, 30, "42"))

	rows := notifRows(t, db, 1)
	if len(rows) != 1 || rows[0].status != "sent" || rows[0].deliverySeq.Int64 != 30 {
		t.Fatalf("rows = %+v, want the legacy row sent with seq 30", rows)
	}

	mustUpsert(t, db, discovered(1, event.ReasonDiscovered), 30)
	if rows := notifRows(t, db, 1); len(rows) != 1 || rows[0].status != "sent" {
		t.Fatalf("after redelivery: %+v, want one sent row", rows)
	}
}

// A NotificationSent body without delivery_seq (from a telegram-notifier
// that predates it) still decodes and resolves the event's latest pending
// row, as before.
func TestDeliverySeq_ResultWithoutSeqResolvesLatestPending(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	mustUpsert(t, db, discovered(1, event.ReasonManualRetry), 0)
	mustUpsert(t, db, discovered(1, event.ReasonManualRetry), 0)

	var sent event.NotificationSent
	if err := json.Unmarshal([]byte(`{"event_id":1,"telegram_message_id":"9","sent_at":"2026-11-01T10:00:00Z"}`), &sent); err != nil {
		t.Fatalf("decode legacy NotificationSent: %v", err)
	}
	if sent.DeliverySeq != 0 {
		t.Fatalf("DeliverySeq = %d, want 0", sent.DeliverySeq)
	}
	mustSent(t, db, &sent)

	rows := notifRows(t, db, 1)
	if len(rows) != 2 || rows[0].status != "pending" || rows[1].status != "sent" || rows[1].messageID.String != "9" {
		t.Fatalf("rows = %+v, want the older pending and the latest sent", rows)
	}
}

// Without metadata the discovered path keeps the old redelivery guard: a
// ReasonDiscovered message for an event with a pending row adds nothing.
func TestDeliverySeq_DiscoveredWithoutSeqKeepsPendingGuard(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	d := discovered(1, event.ReasonDiscovered)
	mustUpsert(t, db, d, 0)
	mustUpsert(t, db, d, 0)

	if rows := notifRows(t, db, 1); len(rows) != 1 || rows[0].status != "pending" || rows[0].deliverySeq.Valid {
		t.Fatalf("rows = %+v, want one pending row without delivery_seq", rows)
	}
}

// Delivery sequences are per message, not per event: the same sequence on
// two events is two rows.
func TestDeliverySeq_SameSeqOnDifferentEventsIsTwoRows(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	mustUpsert(t, db, discovered(1, event.ReasonDiscovered), 40)
	mustUpsert(t, db, discovered(2, event.ReasonDiscovered), 40)
	mustSent(t, db, sentResult(2, 40, "x"))

	if r := notifRows(t, db, 1); len(r) != 1 || r[0].status != "pending" {
		t.Errorf("event 1 rows = %+v, want one pending", r)
	}
	if r := notifRows(t, db, 2); len(r) != 1 || r[0].status != "sent" {
		t.Errorf("event 2 rows = %+v, want one sent", r)
	}
}

// A sent result after a failed one for the same message wins.
func TestDeliverySeq_SentOverwritesFailed(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	mustUpsert(t, db, discovered(1, event.ReasonDiscovered), 20)
	mustFailed(t, db, failedResult(1, 20))
	mustSent(t, db, sentResult(1, 20, "42"))

	rows := notifRows(t, db, 1)
	if len(rows) != 1 || rows[0].status != "sent" || rows[0].messageID.String != "42" || rows[0].errMsg.Valid {
		t.Fatalf("rows = %+v, want one sent row with message 42 and no error", rows)
	}
}

// Rollout: an older web-ui-api stored a scraper message's row without a
// sequence and died before acking, and this one gets the redelivery. The
// redelivery stamps the existing pending row instead of adding a second one.
func TestDeliverySeq_RedeliveryAdoptsLegacyPendingRow(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	d := discovered(1, event.ReasonDiscovered)
	mustUpsert(t, db, d, 0)  // older web-ui-api
	mustUpsert(t, db, d, 50) // redelivery to this one
	mustSent(t, db, sentResult(1, 50, "42"))

	rows := notifRows(t, db, 1)
	if len(rows) != 1 || rows[0].status != "sent" || rows[0].deliverySeq.Int64 != 50 {
		t.Fatalf("rows = %+v, want one sent row with seq 50", rows)
	}
}

// A manual retry never adopts an older pending row: it is a new send.
func TestDeliverySeq_ManualRetryDoesNotAdoptLegacyRow(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	mustUpsert(t, db, discovered(1, event.ReasonDiscovered), 0)
	mustUpsert(t, db, discovered(1, event.ReasonManualRetry), 60)

	rows := notifRows(t, db, 1)
	if len(rows) != 2 || rows[0].deliverySeq.Valid || rows[1].deliverySeq.Int64 != 60 {
		t.Fatalf("rows = %+v, want the legacy row untouched and a new row with seq 60", rows)
	}
}
