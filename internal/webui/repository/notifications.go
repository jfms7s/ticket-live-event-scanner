package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
)

// UpsertEventAndInsertNotification upserts an event and records the pending
// notification for one events.discovered message. It is called by the
// events consumer.
//
// deliverySeq is the EVENTS stream sequence of that message, which
// telegram-notifier echoes back in NotificationSent/Failed. When it is
// non-zero the row is keyed by (event_id, delivery_seq): a redelivery of the
// same message finds its row and never adds another, and a result that
// arrived first already created the row, whose status is left alone.
//
// deliverySeq 0 means the sequence is unknown (no message metadata). Then
// the pre-delivery_seq rule applies: a ReasonDiscovered message is skipped
// when the event already has a pending row, and a manual retry always gets
// a new row (the project design doc (kept outside this repo), §6.4).
func UpsertEventAndInsertNotification(ctx context.Context, db *sql.DB, disc *event.Discovered, deliverySeq uint64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Upsert event (INSERT OR REPLACE in SQLite)
	upsertEventSQL := `
		INSERT INTO events (id, slug, title, venue, category, event_date, url, image_url, discovered_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			slug = excluded.slug,
			title = excluded.title,
			venue = excluded.venue,
			category = excluded.category,
			event_date = excluded.event_date,
			url = excluded.url,
			image_url = excluded.image_url
	`

	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	_, err = tx.ExecContext(ctx, upsertEventSQL,
		disc.EventID, disc.Slug, disc.Title, disc.Venue, disc.Category,
		disc.EventDate, disc.URL, disc.ImageURL, now,
	)
	if err != nil {
		return fmt.Errorf("upsert event: %w", err)
	}

	// Determine triggered_by from reason
	triggeredBy := "scraper"
	if disc.Reason == event.ReasonManualRetry {
		triggeredBy = "manual-retry"
	}

	if deliverySeq != 0 {
		seq := int64(deliverySeq)
		// A scraper message that finds a pending row from before delivery_seq
		// existed stamps it with its sequence instead of adding a row: the
		// old redelivery guard, kept for messages consumed by an older
		// web-ui-api and redelivered to this one. Manual retries always get
		// their own row.
		if disc.Reason == event.ReasonDiscovered {
			adopted, err := tx.ExecContext(ctx, `
				UPDATE notifications SET delivery_seq = ?
				WHERE id = (
					SELECT id FROM notifications
					WHERE event_id = ? AND status = 'pending' AND delivery_seq IS NULL
					ORDER BY attempted_at DESC, id DESC
					LIMIT 1
				)
				AND NOT EXISTS (
					SELECT 1 FROM notifications WHERE event_id = ? AND delivery_seq = ?
				)
			`, seq, disc.EventID, disc.EventID, seq)
			if err != nil {
				return fmt.Errorf("adopt legacy pending notification: %w", err)
			}
			if n, err := adopted.RowsAffected(); err != nil {
				return fmt.Errorf("rows affected: %w", err)
			} else if n > 0 {
				if err := tx.Commit(); err != nil {
					return fmt.Errorf("commit transaction: %w", err)
				}
				return nil
			}
		}

		// Never touch status: a result for this message may already be in.
		_, err = tx.ExecContext(ctx, `
			INSERT INTO notifications (event_id, status, triggered_by, attempted_at, delivery_seq)
			VALUES (?, 'pending', ?, ?, ?)
			ON CONFLICT(event_id, delivery_seq) DO UPDATE SET triggered_by = excluded.triggered_by
		`, disc.EventID, triggeredBy, now, seq)
		if err != nil {
			return fmt.Errorf("upsert notification: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit transaction: %w", err)
		}
		return nil
	}

	// No sequence: skip a ReasonDiscovered message when a pending row already
	// exists (likely a redelivery). A manual retry always gets a fresh row.
	if disc.Reason == event.ReasonDiscovered {
		var existingCount int
		checkSQL := `SELECT COUNT(*) FROM notifications WHERE event_id = ? AND status = 'pending'`
		if err := tx.QueryRowContext(ctx, checkSQL, disc.EventID).Scan(&existingCount); err != nil {
			return fmt.Errorf("check pending notification: %w", err)
		}

		if existingCount > 0 {
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("commit transaction: %w", err)
			}
			return nil
		}
	}

	insertNotifSQL := `
		INSERT INTO notifications (event_id, status, triggered_by, attempted_at)
		VALUES (?, 'pending', ?, ?)
	`

	_, err = tx.ExecContext(ctx, insertNotifSQL, disc.EventID, triggeredBy, now)
	if err != nil {
		return fmt.Errorf("insert notification: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// ErrEventNotMaterialized means a delivery result arrived before the
// events consumer stored its event, so there is no row to attach it to yet.
// The caller should redeliver it after a delay.
var ErrEventNotMaterialized = errors.New("event not materialized yet")

// notificationResult is a sent or failed delivery result to apply to the
// notification row of one events.discovered message.
type notificationResult struct {
	eventID     int64
	deliverySeq uint64
	status      string  // "sent" or "failed"
	messageID   *string // telegram_message_id; nil for failed
	errMsg      *string // error; nil for sent
	confirmedAt string
}

// applyNotificationResult records a delivery result.
//
// With a delivery sequence it resolves the row for that exact message:
//  1. the row keyed by (event_id, delivery_seq), when one exists;
//  2. otherwise the latest pending row of the event that has no
//     delivery_seq, a row written before delivery_seq existed (or without
//     message metadata), which it adopts by setting delivery_seq;
//  3. otherwise a new row in the result's status, since the result arrived
//     before its events.discovered message (that message's upsert then
//     finds the row and leaves its status alone). When the event itself is
//     not in the database yet, it returns ErrEventNotMaterialized and
//     writes nothing.
//
// "sent" wins over "failed": a failed result never overwrites a sent row,
// because Telegram did deliver the message.
//
// Limits: adoption (step 2) picks the latest legacy pending row, so while
// rows from before delivery_seq are still pending, a result for a newer
// message of the same event can resolve the older row; this only affects
// the rollout window. Sequences restart if the EVENTS stream is recreated,
// so a reused (event_id, delivery_seq) pair would match the old row.
//
// Without a delivery sequence (a result from a telegram-notifier that
// predates delivery_seq) it resolves the event's latest pending row, as
// before, and logs when there is none.
func applyNotificationResult(ctx context.Context, db *sql.DB, r notificationResult) error {
	if r.deliverySeq == 0 {
		return applyLegacyNotificationResult(ctx, db, r)
	}
	seq := int64(r.deliverySeq)

	// Steps 1 and 3 in one statement; the conflict target is the
	// (event_id, delivery_seq) unique index. Step 2 runs first only when no
	// row for this message exists yet.
	adopt, err := db.ExecContext(ctx, `
		UPDATE notifications
		SET status = ?, telegram_message_id = ?, error = ?, confirmed_at = ?, delivery_seq = ?
		WHERE id = (
			SELECT id FROM notifications
			WHERE event_id = ? AND status = 'pending' AND delivery_seq IS NULL
			ORDER BY attempted_at DESC, id DESC
			LIMIT 1
		)
		AND NOT EXISTS (
			SELECT 1 FROM notifications WHERE event_id = ? AND delivery_seq = ?
		)
	`, r.status, r.messageID, r.errMsg, r.confirmedAt, seq, r.eventID, r.eventID, seq)
	if err != nil {
		return fmt.Errorf("adopt legacy pending notification: %w", err)
	}
	if n, err := adopt.RowsAffected(); err != nil {
		return fmt.Errorf("rows affected: %w", err)
	} else if n > 0 {
		return nil
	}

	// Step 3 needs the event row (notifications.event_id references it, and
	// sqld may enforce that). If the events consumer hasn't materialized the
	// event yet, the caller retries later.
	var eventKnown bool
	if err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM events WHERE id = ?)`, r.eventID).Scan(&eventKnown); err != nil {
		return fmt.Errorf("check event exists: %w", err)
	}
	if !eventKnown {
		return ErrEventNotMaterialized
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO notifications (event_id, status, telegram_message_id, error, attempted_at, confirmed_at, delivery_seq)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(event_id, delivery_seq) DO UPDATE SET
			status = excluded.status,
			telegram_message_id = excluded.telegram_message_id,
			error = excluded.error,
			confirmed_at = excluded.confirmed_at
		WHERE NOT (excluded.status = 'failed' AND notifications.status = 'sent')
	`, r.eventID, r.status, r.messageID, r.errMsg, r.confirmedAt, r.confirmedAt, seq)
	if err != nil {
		return fmt.Errorf("upsert notification %s: %w", r.status, err)
	}
	return nil
}

// applyLegacyNotificationResult resolves the event's most recent pending
// row: the matching rule for results that carry no delivery sequence.
func applyLegacyNotificationResult(ctx context.Context, db *sql.DB, r notificationResult) error {
	result, err := db.ExecContext(ctx, `
		UPDATE notifications
		SET status = ?, telegram_message_id = COALESCE(?, telegram_message_id), error = COALESCE(?, error), confirmed_at = ?
		WHERE id = (
			SELECT id FROM notifications
			WHERE event_id = ? AND status = 'pending'
			ORDER BY attempted_at DESC, id DESC
			LIMIT 1
		)
	`, r.status, r.messageID, r.errMsg, r.confirmedAt, r.eventID)
	if err != nil {
		return fmt.Errorf("update notification %s: %w", r.status, err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		// Already resolved, or the event's discovered message hasn't been
		// consumed yet; without a delivery sequence there is no row to key on.
		log.Printf("No pending notification found for event %d (%s status expected, no delivery_seq)", r.eventID, r.status)
	}
	return nil
}

// UpdateNotificationSent records a notifications.sent result.
func UpdateNotificationSent(ctx context.Context, db *sql.DB, sent *event.NotificationSent) error {
	messageID := sent.TelegramMessageID
	return applyNotificationResult(ctx, db, notificationResult{
		eventID:     sent.EventID,
		deliverySeq: sent.DeliverySeq,
		status:      "sent",
		messageID:   &messageID,
		confirmedAt: sent.SentAt.UTC().Format("2006-01-02T15:04:05Z"),
	})
}

// UpdateNotificationFailed records a notifications.failed result.
func UpdateNotificationFailed(ctx context.Context, db *sql.DB, failed *event.NotificationFailed) error {
	errMsg := failed.Error
	return applyNotificationResult(ctx, db, notificationResult{
		eventID:     failed.EventID,
		deliverySeq: failed.DeliverySeq,
		status:      "failed",
		errMsg:      &errMsg,
		confirmedAt: failed.FailedAt.UTC().Format("2006-01-02T15:04:05Z"),
	})
}

// GetNotificationsForEvent returns all notifications for a given event (without event_id field),
// ordered by attempted_at DESC (most recent first).
func GetNotificationsForEvent(ctx context.Context, db *sql.DB, eventID int64) ([]NotificationInEventResponse, error) {
	query := `
		SELECT id, status, telegram_message_id, attempted_at, confirmed_at, error, triggered_by
		FROM notifications
		WHERE event_id = ?
		ORDER BY attempted_at DESC
	`

	rows, err := db.QueryContext(ctx, query, eventID)
	if err != nil {
		return nil, fmt.Errorf("query notifications: %w", err)
	}
	defer rows.Close()

	var notifs []NotificationInEventResponse
	for rows.Next() {
		var id int64
		var status string
		var telegramMessageID *string
		var attemptedAtStr string
		var confirmedAtStr *string
		var errorMsg *string
		var triggeredBy string

		if err := rows.Scan(&id, &status, &telegramMessageID, &attemptedAtStr, &confirmedAtStr, &errorMsg, &triggeredBy); err != nil {
			return nil, fmt.Errorf("scan notification row: %w", err)
		}

		notifs = append(notifs, NotificationInEventResponse{
			ID:                id,
			Status:            status,
			TelegramMessageID: telegramMessageID,
			AttemptedAt:       attemptedAtStr,
			ConfirmedAt:       confirmedAtStr,
			Error:             errorMsg,
			TriggeredBy:       triggeredBy,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	if notifs == nil {
		notifs = []NotificationInEventResponse{}
	}

	return notifs, nil
}

// ListNotifications returns all notifications, optionally filtered by status.
// status can be "" (all), "pending", "sent", or "failed".
func ListNotifications(ctx context.Context, db *sql.DB, status string) ([]NotificationResponse, error) {
	query := `
		SELECT id, event_id, status, telegram_message_id, attempted_at, confirmed_at, error, triggered_by
		FROM notifications
	`

	if status != "" {
		query += " WHERE status = ?"
	}

	query += " ORDER BY attempted_at DESC"

	var rows *sql.Rows
	var err error

	if status != "" {
		rows, err = db.QueryContext(ctx, query, status)
	} else {
		rows, err = db.QueryContext(ctx, query)
	}

	if err != nil {
		return nil, fmt.Errorf("query notifications: %w", err)
	}
	defer rows.Close()

	var notifs []NotificationResponse
	for rows.Next() {
		var id int64
		var eventID int64
		var status string
		var telegramMessageID *string
		var attemptedAtStr string
		var confirmedAtStr *string
		var errorMsg *string
		var triggeredBy string

		if err := rows.Scan(&id, &eventID, &status, &telegramMessageID, &attemptedAtStr, &confirmedAtStr, &errorMsg, &triggeredBy); err != nil {
			return nil, fmt.Errorf("scan notification row: %w", err)
		}

		notifs = append(notifs, NotificationResponse{
			ID:                id,
			EventID:           eventID,
			Status:            status,
			TelegramMessageID: telegramMessageID,
			AttemptedAt:       attemptedAtStr,
			ConfirmedAt:       confirmedAtStr,
			Error:             errorMsg,
			TriggeredBy:       triggeredBy,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	if notifs == nil {
		notifs = []NotificationResponse{}
	}

	return notifs, nil
}

// listNotificationsByEvent returns every notification grouped by event id,
// newest attempt first within each event, in a single query.
func listNotificationsByEvent(ctx context.Context, db *sql.DB) (map[int64][]NotificationInEventResponse, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, event_id, status, telegram_message_id, attempted_at, confirmed_at, error, triggered_by
		FROM notifications
		ORDER BY event_id, attempted_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query notifications: %w", err)
	}
	defer rows.Close()

	byEvent := make(map[int64][]NotificationInEventResponse)
	for rows.Next() {
		var n NotificationInEventResponse
		var eventID int64
		if err := rows.Scan(&n.ID, &eventID, &n.Status, &n.TelegramMessageID, &n.AttemptedAt, &n.ConfirmedAt, &n.Error, &n.TriggeredBy); err != nil {
			return nil, fmt.Errorf("scan notification row: %w", err)
		}
		byEvent[eventID] = append(byEvent[eventID], n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return byEvent, nil
}
