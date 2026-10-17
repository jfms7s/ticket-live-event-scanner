package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ListEvents returns every visible (not hidden) event with its
// notifications and its status computed as of now, optionally filtered by
// status: "" (all), "active", or "finished".
func ListEvents(ctx context.Context, db *sql.DB, status string, now time.Time) ([]EventResponse, error) {
	query := `
		SELECT id, slug, title, venue, category, event_date, url, image_url, discovered_at, purchased,
		       invite_status, invite_error
		FROM events
		WHERE hidden = 0
		ORDER BY discovered_at DESC
	`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}

	// Scan every row into a base struct first and close rows before issuing
	// any further queries. Issuing the notifications query (a second
	// db.QueryContext on the same *sql.DB) while these rows are still open
	// would deadlock: under a constrained connection
	// pool (e.g. MaxOpenConns=1, as in tests) the nested query can never
	// get a connection because the outer Rows is holding the only one,
	// while the outer loop can't finish until the nested query returns.
	type eventBase struct {
		id                                   int64
		slug, title, url                     string
		venue, category, eventDate, imageURL *string
		discoveredAtStr                      string
		purchased                            bool
		inviteStatus, inviteError            *string
		computedStatus                       string
	}
	var bases []eventBase

	for rows.Next() {
		var b eventBase
		if err := rows.Scan(&b.id, &b.slug, &b.title, &b.venue, &b.category, &b.eventDate, &b.url, &b.imageURL, &b.discoveredAtStr, &b.purchased, &b.inviteStatus, &b.inviteError); err != nil {
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

	// One query for every event's notifications, not one per event.
	byEvent, err := listNotificationsByEvent(ctx, db)
	if err != nil {
		return nil, err
	}

	events := make([]EventResponse, 0, len(bases))
	for _, b := range bases {
		notifs := byEvent[b.id]
		if notifs == nil {
			notifs = []NotificationInEventResponse{}
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
			InviteStatus:  b.inviteStatus,
			InviteError:   b.inviteError,
		})
	}

	return events, nil
}

// GetEvent returns one visible event with its notifications and its status
// computed as of now. A hidden event is reported as sql.ErrNoRows.
func GetEvent(ctx context.Context, db *sql.DB, id int64, now time.Time) (*EventResponse, error) {
	query := `
		SELECT id, slug, title, venue, category, event_date, url, image_url, discovered_at, purchased,
		       invite_status, invite_error
		FROM events
		WHERE id = ? AND hidden = 0
	`

	var slug, title, url string
	var venue, category, eventDate, imageURL *string
	var discoveredAtStr string
	var purchased bool
	var inviteStatus, inviteError *string

	if err := db.QueryRowContext(ctx, query, id).Scan(&id, &slug, &title, &venue, &category, &eventDate, &url, &imageURL, &discoveredAtStr, &purchased, &inviteStatus, &inviteError); err != nil {
		return nil, err
	}

	// Compute status
	computedStatus := computeStatus(eventDate, now)

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
		InviteStatus:  inviteStatus,
		InviteError:   inviteError,
	}, nil
}

// SetEventPurchased updates the purchased flag for a single event. It
// returns sql.ErrNoRows if no visible event with that ID exists. Marking an event
// purchased also sets its invite to pending (clearing any earlier error) in
// the same UPDATE, since the caller then publishes a new invite request.
// Unmarking leaves the invite state alone: an invite already sent stays sent.
func SetEventPurchased(ctx context.Context, db *sql.DB, id int64, purchased bool) error {
	query := `UPDATE events SET purchased = ? WHERE id = ? AND hidden = 0`
	if purchased {
		query = `UPDATE events SET purchased = ?, invite_status = 'pending', invite_error = NULL WHERE id = ? AND hidden = 0`
	}
	result, err := db.ExecContext(ctx, query, purchased, id)
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

// HideEvent is the dashboard's Remove: it hides an event from the API and
// keeps its row and notifications. The row is how the scraper knows the
// event was already notified, so deleting it would notify the event again
// on the next run while it is still listed. It returns sql.ErrNoRows if no
// visible event with that ID exists.
func HideEvent(ctx context.Context, db *sql.DB, id int64) error {
	result, err := db.ExecContext(ctx, `UPDATE events SET hidden = 1 WHERE id = ? AND hidden = 0`, id)
	if err != nil {
		return fmt.Errorf("hide event: %w", err)
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
