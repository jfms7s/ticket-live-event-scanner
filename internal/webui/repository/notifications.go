package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// timestampLayout is how the notifications and events tables store times:
// UTC, second precision.
const timestampLayout = "2006-01-02T15:04:05Z"

// GetNotificationsForEvent returns eventID's notifications, newest attempt
// first.
func GetNotificationsForEvent(ctx context.Context, db *sql.DB, eventID int64) ([]NotificationInEventResponse, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+notificationColumns+`
		FROM notifications
		WHERE event_id = ? AND channel = 'telegram'
		ORDER BY attempted_at DESC`, eventID)
	if err != nil {
		return nil, fmt.Errorf("query notifications: %w", err)
	}
	defer rows.Close()

	notifs := []NotificationInEventResponse{}
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, fmt.Errorf("scan notification row: %w", err)
		}
		notifs = append(notifs, n.NotificationInEventResponse)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return notifs, nil
}

// ListNotifications returns all notifications, optionally filtered by status.
// status can be "" (all), "pending", "sent", or "failed".
func ListNotifications(ctx context.Context, db *sql.DB, status string) ([]NotificationResponse, error) {
	// The invite emails are queued in the same table; the API lists the
	// Telegram notifications (an invite's state is on the event).
	query := `SELECT ` + notificationColumns + ` FROM notifications WHERE channel = 'telegram'`
	var args []any
	if status != "" {
		query += " AND status = ?"
		args = append(args, status)
	}
	query += " ORDER BY attempted_at DESC"

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query notifications: %w", err)
	}
	defer rows.Close()

	notifs := []NotificationResponse{}
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, fmt.Errorf("scan notification row: %w", err)
		}
		notifs = append(notifs, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return notifs, nil
}

// listNotificationsByEvent returns every notification grouped by event id,
// newest attempt first within each event, in a single query.
func listNotificationsByEvent(ctx context.Context, db *sql.DB) (map[int64][]NotificationInEventResponse, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+notificationColumns+`
		FROM notifications
		WHERE channel = 'telegram'
		ORDER BY event_id, attempted_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query notifications: %w", err)
	}
	defer rows.Close()

	byEvent := make(map[int64][]NotificationInEventResponse)
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, fmt.Errorf("scan notification row: %w", err)
		}
		byEvent[n.EventID] = append(byEvent[n.EventID], n.NotificationInEventResponse)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}
	return byEvent, nil
}

// notificationColumns is the column list scanNotification expects, in order.
const notificationColumns = `id, event_id, status, telegram_message_id, attempted_at, confirmed_at, error, triggered_by`

// scanNotification scans one notificationColumns row.
func scanNotification(s rowScanner) (NotificationResponse, error) {
	var n NotificationResponse
	err := s.Scan(&n.ID, &n.EventID, &n.Status, &n.TelegramMessageID, &n.AttemptedAt, &n.ConfirmedAt, &n.Error, &n.TriggeredBy)
	return n, err
}
