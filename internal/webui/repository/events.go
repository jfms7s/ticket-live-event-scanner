package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ListEvents returns all events with their notifications, optionally filtered by status.
// status can be "" (all), "active", or "finished".
func ListEvents(ctx context.Context, db *sql.DB, status string) ([]EventResponse, error) {
	query := `
		SELECT id, slug, title, venue, category, event_date, url, image_url, discovered_at, purchased
		FROM events
		ORDER BY discovered_at DESC
	`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}

	// Scan every row into a base struct first and close rows before issuing
	// any further queries. Calling GetNotificationsForEvent (a second
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
		purchased                            bool
		computedStatus                       string
	}
	var bases []eventBase
	now := time.Now()

	for rows.Next() {
		var b eventBase
		if err := rows.Scan(&b.id, &b.slug, &b.title, &b.venue, &b.category, &b.eventDate, &b.url, &b.imageURL, &b.discoveredAtStr, &b.purchased); err != nil {
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
		notifs, err := GetNotificationsForEvent(ctx, db, b.id)
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
			Purchased:     b.purchased,
			Status:        b.computedStatus,
			Notifications: notifs,
		})
	}

	return events, nil
}

// GetEvent returns a single event by ID with its notifications.
func GetEvent(ctx context.Context, db *sql.DB, id int64) (*EventResponse, error) {
	query := `
		SELECT id, slug, title, venue, category, event_date, url, image_url, discovered_at, purchased
		FROM events
		WHERE id = ?
	`

	var slug, title, url string
	var venue, category, eventDate, imageURL *string
	var discoveredAtStr string
	var purchased bool

	if err := db.QueryRowContext(ctx, query, id).Scan(&id, &slug, &title, &venue, &category, &eventDate, &url, &imageURL, &discoveredAtStr, &purchased); err != nil {
		return nil, err
	}

	// Compute status
	computedStatus := computeStatus(eventDate, time.Now())

	// Get notifications for this event
	notifs, err := GetNotificationsForEvent(ctx, db, id)
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
		Purchased:     purchased,
		Status:        computedStatus,
		Notifications: notifs,
	}, nil
}

// SetEventPurchased updates the purchased flag for a single event. It
// returns sql.ErrNoRows if no event with that ID exists.
func SetEventPurchased(ctx context.Context, db *sql.DB, id int64, purchased bool) error {
	result, err := db.ExecContext(ctx, `UPDATE events SET purchased = ? WHERE id = ?`, purchased, id)
	if err != nil {
		return fmt.Errorf("update purchased: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// DeleteEvent removes an event and its notifications from the database.
// It returns sql.ErrNoRows if no event with that ID exists. Notifications
// are deleted explicitly in the same transaction since SQLite foreign keys
// are not enforced here (no PRAGMA foreign_keys=ON), so there is no
// cascading delete to rely on.
func DeleteEvent(ctx context.Context, db *sql.DB, id int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM notifications WHERE event_id = ?`, id); err != nil {
		return fmt.Errorf("delete notifications: %w", err)
	}

	result, err := tx.ExecContext(ctx, `DELETE FROM events WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete event: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if rows == 0 {
		return sql.ErrNoRows
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}
