package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
)

// upsertEventAndInsertNotification upserts an event and inserts a pending notification.
// This is called when an events.discovered message is received.
// To ensure idempotency on redelivery (JetStream at-least-once delivery), we check
// if a pending notification already exists for this event. If it does, we skip insertion
// to avoid creating duplicate rows on message redelivery.
func upsertEventAndInsertNotification(ctx context.Context, db *sql.DB, disc *event.Discovered) error {
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

	// Check if a pending notification already exists for this event.
	// This is a simple idempotency check: if redelivery happens within a few seconds,
	// we won't create a duplicate pending row.
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

// updateNotificationSent updates the most recent pending notification for an event to sent.
// This is called when a notifications.sent message is received.
func updateNotificationSent(ctx context.Context, db *sql.DB, sent *event.NotificationSent) error {
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

// updateNotificationFailed updates the most recent pending notification for an event to failed.
// This is called when a notifications.failed message is received.
func updateNotificationFailed(ctx context.Context, db *sql.DB, failed *event.NotificationFailed) error {
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
		fmt.Printf("No pending notification found for event %d\n", failed.EventID)
	}

	return nil
}

// listEvents returns all events with their notifications, optionally filtered by status.
// status can be "" (all), "active", or "finished".
func listEvents(ctx context.Context, db *sql.DB, status string) ([]EventResponse, error) {
	query := `
		SELECT id, slug, title, venue, category, event_date, url, image_url, discovered_at
		FROM events
		ORDER BY discovered_at DESC
	`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}

	// Scan every row into a base struct first and close rows before issuing
	// any further queries. Calling getNotificationsForEvent (a second
	// db.QueryContext on the same *sql.DB) while these rows are still open
	// is an N+1-and-deadlock-prone pattern: under a constrained connection
	// pool (e.g. MaxOpenConns=1, as in tests) the nested query can never
	// get a connection because the outer Rows is holding the only one,
	// while the outer loop can't finish until the nested query returns.
	type eventBase struct {
		id                                   int64
		slug, title, url                     string
		venue, category, eventDate, imageURL *string
		discoveredAtStr                      string
		computedStatus                       string
	}
	var bases []eventBase
	now := time.Now()

	for rows.Next() {
		var b eventBase
		if err := rows.Scan(&b.id, &b.slug, &b.title, &b.venue, &b.category, &b.eventDate, &b.url, &b.imageURL, &b.discoveredAtStr); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan row: %w", err)
		}

		b.computedStatus = computeStatus(b.eventDate, now)
		if status != "" && b.computedStatus != status {
			continue
		}
		bases = append(bases, b)
	}

	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("rows error: %w", err)
	}
	rows.Close()

	events := make([]EventResponse, 0, len(bases))
	for _, b := range bases {
		notifs, err := getNotificationsForEvent(ctx, db, b.id)
		if err != nil {
			return nil, fmt.Errorf("get notifications for event %d: %w", b.id, err)
		}

		events = append(events, EventResponse{
			ID:            b.id,
			Slug:          b.slug,
			Title:         b.title,
			Venue:         b.venue,
			Category:      b.category,
			EventDate:     b.eventDate,
			URL:           b.url,
			ImageURL:      b.imageURL,
			DiscoveredAt:  b.discoveredAtStr,
			Status:        b.computedStatus,
			Notifications: notifs,
		})
	}

	return events, nil
}

// getEvent returns a single event by ID with its notifications.
func getEvent(ctx context.Context, db *sql.DB, id int64) (*EventResponse, error) {
	query := `
		SELECT id, slug, title, venue, category, event_date, url, image_url, discovered_at
		FROM events
		WHERE id = ?
	`

	var slug, title, url string
	var venue, category, eventDate, imageURL *string
	var discoveredAtStr string

	if err := db.QueryRowContext(ctx, query, id).Scan(&id, &slug, &title, &venue, &category, &eventDate, &url, &imageURL, &discoveredAtStr); err != nil {
		return nil, err
	}

	// Compute status
	computedStatus := computeStatus(eventDate, time.Now())

	// Get notifications for this event
	notifs, err := getNotificationsForEvent(ctx, db, id)
	if err != nil {
		return nil, fmt.Errorf("get notifications: %w", err)
	}

	return &EventResponse{
		ID:            id,
		Slug:          slug,
		Title:         title,
		Venue:         venue,
		Category:      category,
		EventDate:     eventDate,
		URL:           url,
		ImageURL:      imageURL,
		DiscoveredAt:  discoveredAtStr,
		Status:        computedStatus,
		Notifications: notifs,
	}, nil
}

// getNotificationsForEvent returns all notifications for a given event (without event_id field),
// ordered by attempted_at DESC (most recent first).
func getNotificationsForEvent(ctx context.Context, db *sql.DB, eventID int64) ([]NotificationInEventResponse, error) {
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

// listNotifications returns all notifications, optionally filtered by status.
// status can be "" (all), "pending", "sent", or "failed".
func listNotifications(ctx context.Context, db *sql.DB, status string) ([]NotificationResponse, error) {
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

// dateLayouts are tried in order when parsing a stored event_date. The
// column is declared DATE and written as plain "2006-01-02" text, but some
// SQLite driver implementations (observed with modernc.org/sqlite, used in
// tests) auto-detect DATE-typed columns and hand back RFC3339 on Scan
// instead of the raw text that was inserted. Trying both keeps this correct
// regardless of which behavior the driver in use (test or production)
// exhibits.
var dateLayouts = []string{"2006-01-02", time.RFC3339}

// computeStatus computes whether an event is "active" or "finished" based on its event_date.
func computeStatus(eventDate *string, now time.Time) string {
	if eventDate == nil || *eventDate == "" {
		// If no event_date, assume it's active
		return "active"
	}

	var eventTime time.Time
	var parsed bool
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, *eventDate); err == nil {
			eventTime = t
			parsed = true
			break
		}
	}
	if !parsed {
		// If we can't parse, assume it's active
		return "active"
	}

	// Check if event_date >= now's date
	if eventTime.After(now) || eventTime.Format("2006-01-02") == now.Format("2006-01-02") {
		return "active"
	}

	return "finished"
}
