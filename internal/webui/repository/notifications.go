package repository

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
)

// UpsertEventAndInsertNotification upserts an event and inserts a pending notification.
// This is called when an events.discovered message is received.
// To ensure idempotency on redelivery (JetStream at-least-once delivery), we check
// if a pending notification already exists for this event. If it does, we skip insertion
// to avoid creating duplicate rows on message redelivery.
func UpsertEventAndInsertNotification(ctx context.Context, db *sql.DB, disc *event.Discovered) error {
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

	// Check if a pending notification already exists for this event, but only for
	// ReasonDiscovered (scraper redelivery). For ReasonManualRetry (user action),
	// always create a fresh pending row per design.md §6.4.
	if disc.Reason == event.ReasonDiscovered {
		var existingCount int
		checkSQL := `SELECT COUNT(*) FROM notifications WHERE event_id = ? AND status = 'pending'`
		if err := tx.QueryRowContext(ctx, checkSQL, disc.EventID).Scan(&existingCount); err != nil {
			return fmt.Errorf("check pending notification: %w", err)
		}

		// If a pending notification already exists, this is likely a redelivery. Skip insertion.
		if existingCount > 0 {
			if err := tx.Commit(); err != nil {
				return fmt.Errorf("commit transaction: %w", err)
			}
			return nil
		}
	}

	// Determine triggered_by from reason
	triggeredBy := "scraper"
	if disc.Reason == event.ReasonManualRetry {
		triggeredBy = "manual-retry"
	}

	// Insert pending notification
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

// UpdateNotificationSent updates the most recent pending notification for an event to sent.
// This is called when a notifications.sent message is received.
func UpdateNotificationSent(ctx context.Context, db *sql.DB, sent *event.NotificationSent) error {
	updateSQL := `
		UPDATE notifications
		SET status = 'sent', telegram_message_id = ?, confirmed_at = ?
		WHERE id = (
			SELECT id FROM notifications
			WHERE event_id = ? AND status = 'pending'
			ORDER BY attempted_at DESC
			LIMIT 1
		)
	`

	confirmedAt := sent.SentAt.UTC().Format("2006-01-02T15:04:05Z")
	result, err := db.ExecContext(ctx, updateSQL, sent.TelegramMessageID, confirmedAt, sent.EventID)
	if err != nil {
		return fmt.Errorf("update notification sent: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		// No pending notification found - this can happen if the notification was
		// already marked as failed or sent, or if the event doesn't exist
		// This is not necessarily an error, just log and continue
		log.Printf("No pending notification found for event %d (sent status expected)", sent.EventID)
	}

	return nil
}

// UpdateNotificationFailed updates the most recent pending notification for an event to failed.
// This is called when a notifications.failed message is received.
func UpdateNotificationFailed(ctx context.Context, db *sql.DB, failed *event.NotificationFailed) error {
	updateSQL := `
		UPDATE notifications
		SET status = 'failed', error = ?, confirmed_at = ?
		WHERE id = (
			SELECT id FROM notifications
			WHERE event_id = ? AND status = 'pending'
			ORDER BY attempted_at DESC
			LIMIT 1
		)
	`

	confirmedAt := failed.FailedAt.UTC().Format("2006-01-02T15:04:05Z")
	result, err := db.ExecContext(ctx, updateSQL, failed.Error, confirmedAt, failed.EventID)
	if err != nil {
		return fmt.Errorf("update notification failed: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		// No pending notification found - similar to sent case
		log.Printf("No pending notification found for event %d (failed status expected)", failed.EventID)
	}

	return nil
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
