// Package outbox is the queue of notifications to send. A notification is
// a row in the notifications table, written in the same transaction as the
// thing that caused it (a discovered session, a purchase, a retry), so it
// cannot be lost or doubled between "decided" and "queued". Package deliver
// sends the rows that are due.
package outbox

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
)

// Channels a notification can go to.
const (
	ChannelTelegram = "telegram"
	ChannelEmail    = "email"
)

// timestampLayout is how the tables store times: UTC, second precision.
const timestampLayout = "2006-01-02T15:04:05Z"

// Format returns t the way the tables store it.
func Format(t time.Time) string { return t.UTC().Format(timestampLayout) }

// MaxAttempts is how many times a notification is tried before it is
// parked as failed (the dashboard's Retry then queues a fresh one).
const MaxAttempts = 6

// Backoff is the wait after the n-th failed attempt (n starting at 1):
// 1 m, 5 m, 30 m, 2 h, 12 h. The sixth failure parks the row.
var Backoff = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 12 * time.Hour}

// BackoffAfter returns the wait after the attempts-th failed attempt.
func BackoffAfter(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	if attempts > len(Backoff) {
		attempts = len(Backoff)
	}
	return Backoff[attempts-1]
}

// Execer is a *sql.DB or a *sql.Tx.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// Queue enqueues notifications. Nudge, when set, is called after a commit
// that added a row, so the poller sends it now instead of at its next tick.
type Queue struct {
	DB    *sql.DB
	Now   func() time.Time
	Nudge func()
}

func (q *Queue) now() time.Time {
	if q.Now != nil {
		return q.Now()
	}
	return time.Now()
}

// Wake asks the poller to look at the table now (a transaction outside this
// package queued something). It is a no-op without a Nudge.
func (q *Queue) Wake() { q.nudge() }

func (q *Queue) nudge() {
	if q.Nudge != nil {
		q.Nudge()
	}
}

// upsertEventSQL stores a discovered session. An event already stored keeps
// its hub when the new message carries none (a retry of an older event).
const upsertEventSQL = `
	INSERT INTO events (id, slug, title, venue, category, event_date, url, image_url, discovered_at, hub_slug)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''))
	ON CONFLICT(id) DO UPDATE SET
		slug = excluded.slug,
		title = excluded.title,
		venue = excluded.venue,
		category = excluded.category,
		event_date = excluded.event_date,
		url = excluded.url,
		image_url = excluded.image_url,
		hub_slug = COALESCE(excluded.hub_slug, events.hub_slug)
`

// EnqueueDiscovered stores the session and queues its Telegram notification
// in one transaction. It reports whether a notification was queued: false
// means this session already had one (the scraper ran twice over it, or two
// processes overlapped), and nothing was sent twice because nothing was
// queued twice.
func (q *Queue) EnqueueDiscovered(ctx context.Context, d *event.Discovered) (bool, error) {
	tx, err := q.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	now := Format(q.now())
	if _, err := tx.ExecContext(ctx, upsertEventSQL,
		d.EventID, d.Slug, d.Title, d.Venue, d.Category, d.EventDate, d.URL, d.ImageURL, now, d.HubSlug); err != nil {
		return false, fmt.Errorf("store event %d: %w", d.EventID, err)
	}
	queued, err := enqueue(ctx, tx, d.EventID, ChannelTelegram, "discovered", "scraper", now)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit: %w", err)
	}
	if queued {
		q.nudge()
	}
	return queued, nil
}

// EnqueueRetry queues another Telegram notification for a stored event (the
// dashboard's Retry). The earlier rows stay as the history. It returns
// sql.ErrNoRows if there is no visible event with that ID.
func (q *Queue) EnqueueRetry(ctx context.Context, eventID int64) error {
	tx, err := q.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	var one int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM events WHERE id = ? AND hidden = 0`, eventID).Scan(&one); err != nil {
		return err
	}
	now := q.now()
	key := "retry-" + strconv.FormatInt(now.UnixNano(), 10)
	if _, err := enqueue(ctx, tx, eventID, ChannelTelegram, key, "manual-retry", Format(now)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	q.nudge()
	return nil
}

// EnqueueInvite queues a calendar-invite email for eventID on tx, the
// transaction that marks the event purchased. Ticking, unticking and ticking
// again queues a second invite, as it always has.
func EnqueueInvite(ctx context.Context, tx Execer, eventID int64, now time.Time) error {
	key := "purchased-" + strconv.FormatInt(now.UnixNano(), 10)
	_, err := enqueue(ctx, tx, eventID, ChannelEmail, key, "purchase", Format(now))
	return err
}

// enqueue inserts one due notification; (event, channel, dedup key) is
// unique, so the same key twice queues once.
func enqueue(ctx context.Context, x Execer, eventID int64, channel, dedupKey, triggeredBy, now string) (bool, error) {
	res, err := x.ExecContext(ctx, `
		INSERT INTO notifications (event_id, status, triggered_by, attempted_at, channel, dedup_key, attempts, next_attempt_at)
		VALUES (?, 'pending', ?, ?, ?, ?, 0, ?)
		ON CONFLICT(event_id, channel, dedup_key) DO NOTHING`,
		eventID, triggeredBy, now, channel, dedupKey, now)
	if err != nil {
		return false, fmt.Errorf("queue %s notification for event %d: %w", channel, eventID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}
	return n > 0, nil
}
