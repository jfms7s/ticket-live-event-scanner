package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ListEvents returns every visible (not hidden) event with its
// notifications and its status computed as of now, optionally filtered by
// status: "" (all), "active", or "finished".
func ListEvents(ctx context.Context, db *sql.DB, status string, now time.Time) ([]EventResponse, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+eventColumns+`
		FROM events
		WHERE hidden = 0
		ORDER BY discovered_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}

	// Scan every row first and close rows before issuing any further
	// queries. Issuing the notifications query (a second db.QueryContext on
	// the same *sql.DB) while these rows are still open would deadlock:
	// under a constrained connection pool (e.g. MaxOpenConns=1, as in tests)
	// the nested query can never get a connection because the outer Rows is
	// holding the only one, while the outer loop can't finish until the
	// nested query returns.
	var events []EventResponse
	for rows.Next() {
		e, err := scanEvent(rows, now)
		if err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan row: %w", err)
		}
		if status != "" && e.Status != status {
			continue
		}
		events = append(events, e)
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

	for i := range events {
		if notifs := byEvent[events[i].ID]; notifs != nil {
			events[i].Notifications = notifs
		}
	}
	if events == nil {
		events = []EventResponse{}
	}

	return events, nil
}

// GetEvent returns one visible event with its notifications and its status
// computed as of now. A hidden event is reported as sql.ErrNoRows.
func GetEvent(ctx context.Context, db *sql.DB, id int64, now time.Time) (*EventResponse, error) {
	row := db.QueryRowContext(ctx, `SELECT `+eventColumns+`
		FROM events
		WHERE id = ? AND hidden = 0`, id)
	e, err := scanEvent(row, now)
	if err != nil {
		return nil, err
	}

	notifs, err := GetNotificationsForEvent(ctx, db, id)
	if err != nil {
		return nil, fmt.Errorf("get notifications: %w", err)
	}
	e.Notifications = notifs

	return &e, nil
}

// rowScanner is the Scan method shared by *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

// eventColumns is the column list scanEvent expects, in order.
const eventColumns = `id, slug, title, venue, category, event_date, url, image_url, discovered_at, purchased,
		       invite_status, invite_error, hub_slug`

// scanEvent scans one eventColumns row, computes its status as of now and
// starts it with an empty notification list. Errors (sql.ErrNoRows
// included) are returned unwrapped.
func scanEvent(s rowScanner, now time.Time) (EventResponse, error) {
	var e EventResponse
	if err := s.Scan(&e.ID, &e.Slug, &e.Title, &e.Venue, &e.Category, &e.EventDate, &e.URL, &e.ImageURL,
		&e.DiscoveredAt, &e.Purchased, &e.InviteStatus, &e.InviteError, &e.HubSlug); err != nil {
		return EventResponse{}, err
	}
	e.Status = computeStatus(e.EventDate, now)
	e.Notifications = []NotificationInEventResponse{}
	return e, nil
}

// SetEventPurchased sets the purchased flag for a single event and reports
// whether it changed. It returns sql.ErrNoRows if no visible event with
// that ID exists. Setting a flag that is already set changes nothing, so a
// double submit doesn't publish a second invite. Marking an unpurchased
// event purchased also sets its invite to pending (clearing any earlier
// error) in the same UPDATE, since the caller then publishes a new invite
// request. Unmarking leaves the invite state alone: an invite already sent
// stays sent.
func SetEventPurchased(ctx context.Context, db *sql.DB, id int64, purchased bool) (changed bool, err error) {
	query := `UPDATE events SET purchased = 0 WHERE id = ? AND hidden = 0 AND purchased <> 0`
	if purchased {
		query = `UPDATE events SET purchased = 1, invite_status = 'pending', invite_error = NULL WHERE id = ? AND hidden = 0 AND purchased = 0`
	}
	result, err := db.ExecContext(ctx, query, id)
	if err != nil {
		return false, fmt.Errorf("update purchased: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}
	if rows > 0 {
		return true, nil
	}

	// Nothing changed: either the flag already had this value or there is
	// no such visible event.
	var one int
	err = db.QueryRowContext(ctx, `SELECT 1 FROM events WHERE id = ? AND hidden = 0`, id).Scan(&one)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, sql.ErrNoRows
		}
		return false, fmt.Errorf("check event exists: %w", err)
	}
	return false, nil
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
