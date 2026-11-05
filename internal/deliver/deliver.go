// Package deliver sends the notifications the outbox holds. One poller, in
// the one process, takes the rows that are due in order, sends each through
// its channel, and writes the outcome back to the row: sent, or a retry at
// the next backoff step, or parked as failed after the last attempt.
//
// A crash anywhere leaves the row pending and due, so the next start sends
// it again: a notification is never lost, and one that was sent just before
// the crash may be sent twice (the owner's choice, 2026-11-05: a missed
// notification is worse than a rare duplicate).
package deliver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/telegram"
	"github.com/jfms7s/ticket-live-event-scanner/internal/outbox"
	"github.com/jfms7s/ticket-live-event-scanner/internal/webui/repository"
)

// Senders deliver one notification each. A nil sender means that channel is
// disabled: its rows stay pending and are not touched.
type Senders struct {
	// Telegram posts a new session and returns the message id.
	Telegram func(ctx context.Context, d event.Discovered) (string, error)
	// Email sends a calendar invite and returns the Message-ID.
	Email func(ctx context.Context, p event.Purchased) (string, error)
}

// Poller sends due notifications.
type Poller struct {
	DB      *sql.DB
	Senders Senders
	// Interval is how often the table is looked at when nothing nudges.
	Interval time.Duration
	// TelegramGap is the minimum time between two Telegram sends.
	TelegramGap time.Duration
	// BatchSize bounds the rows taken per pass.
	BatchSize int
	// Now and Sleep are the clock; tests replace them.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error

	nudge          chan struct{}
	lastTelegram   time.Time
	warnedDisabled map[string]bool
}

// New returns a Poller with the usual settings.
func New(db *sql.DB, s Senders, telegramGap time.Duration) *Poller {
	return &Poller{
		DB: db, Senders: s, Interval: 5 * time.Second, TelegramGap: telegramGap, BatchSize: 20,
		Now: time.Now, Sleep: sleep,
		nudge: make(chan struct{}, 1), warnedDisabled: map[string]bool{},
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Nudge asks the poller to look at the table now. It never blocks.
func (p *Poller) Nudge() {
	select {
	case p.nudge <- struct{}{}:
	default:
	}
}

// Run polls until ctx is cancelled. The row in hand finishes before it
// returns, so a deploy does not cut a send short and then repeat it.
func (p *Poller) Run(ctx context.Context) {
	for ctx.Err() == nil {
		if n, err := p.ProcessDue(ctx); err != nil && ctx.Err() == nil {
			log.Printf("ERROR delivering notifications: %v", err)
		} else if n == p.BatchSize {
			continue // a full batch: there may be more due already
		}
		timer := time.NewTimer(p.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-p.nudge:
			timer.Stop()
		case <-timer.C:
		}
	}
}

type row struct {
	id       int64
	eventID  int64
	channel  string
	attempts int
}

// ProcessDue sends every notification that is due, up to BatchSize, and
// returns how many it took.
func (p *Poller) ProcessDue(ctx context.Context) (int, error) {
	rows, err := p.DB.QueryContext(ctx, `
		SELECT id, event_id, channel, attempts FROM notifications
		WHERE status = 'pending' AND next_attempt_at IS NOT NULL AND next_attempt_at <= ?
		ORDER BY id LIMIT ?`, outbox.Format(p.Now()), p.BatchSize)
	if err != nil {
		return 0, fmt.Errorf("select due notifications: %w", err)
	}
	var due []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.eventID, &r.channel, &r.attempts); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan: %w", err)
		}
		due = append(due, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	n := 0
	for _, r := range due {
		if ctx.Err() != nil {
			break
		}
		p.process(ctx, r)
		n++
	}
	return n, nil
}

// process makes one attempt at r.
func (p *Poller) process(ctx context.Context, r row) {
	send, enabled := p.sender(r.channel)
	if !enabled {
		if !p.warnedDisabled[r.channel] {
			p.warnedDisabled[r.channel] = true
			log.Printf("WARN the %s channel is disabled; its queued notifications wait", r.channel)
		}
		return
	}

	// Count the attempt before making it: a crash during the send then
	// shows as an attempt, and the row stays due.
	started := outbox.Format(p.Now())
	if _, err := p.DB.ExecContext(ctx, `
		UPDATE notifications SET attempts = attempts + 1, last_attempt_started_at = ?, attempted_at = ?
		WHERE id = ? AND status = 'pending'`, started, started, r.id); err != nil {
		log.Printf("ERROR starting notification %d: %v", r.id, err)
		return
	}
	attempts := r.attempts + 1

	id, err := send(ctx, r.eventID)
	if err != nil {
		p.recordFailure(ctx, r, attempts, err)
		return
	}
	p.recordSuccess(ctx, r, id)
}

// sender returns the function that sends row r's channel and whether the
// channel is enabled.
func (p *Poller) sender(channel string) (func(ctx context.Context, eventID int64) (string, error), bool) {
	switch channel {
	case outbox.ChannelTelegram:
		if p.Senders.Telegram == nil {
			return nil, false
		}
		return func(ctx context.Context, eventID int64) (string, error) {
			d, err := p.loadDiscovered(ctx, eventID)
			if err != nil {
				return "", err
			}
			if err := p.paceTelegram(ctx); err != nil {
				return "", err
			}
			id, err := p.Senders.Telegram(ctx, *d)
			p.lastTelegram = p.Now()
			return id, err
		}, true
	case outbox.ChannelEmail:
		if p.Senders.Email == nil {
			return nil, false
		}
		return func(ctx context.Context, eventID int64) (string, error) {
			d, err := p.loadDiscovered(ctx, eventID)
			if err != nil {
				return "", err
			}
			return p.Senders.Email(ctx, event.Purchased{
				EventID: d.EventID, Slug: d.Slug, Title: d.Title, Venue: d.Venue, Category: d.Category,
				EventDate: d.EventDate, URL: d.URL, ImageURL: d.ImageURL, PurchasedAt: p.Now().UTC(),
			})
		}, true
	}
	return func(context.Context, int64) (string, error) {
		return "", fmt.Errorf("unknown notification channel %q", channel)
	}, true
}

// paceTelegram waits until TelegramGap has passed since the last send.
func (p *Poller) paceTelegram(ctx context.Context) error {
	if p.TelegramGap <= 0 || p.lastTelegram.IsZero() {
		return nil
	}
	return p.Sleep(ctx, p.TelegramGap-p.Now().Sub(p.lastTelegram))
}

// errEventGone means the event row a notification points at no longer
// exists; retrying cannot help.
var errEventGone = errors.New("the event is gone")

func (p *Poller) loadDiscovered(ctx context.Context, eventID int64) (*event.Discovered, error) {
	var d event.Discovered
	var venue, category, date, image, hub sql.NullString
	err := p.DB.QueryRowContext(ctx, `
		SELECT id, slug, title, venue, category, event_date, url, image_url, hub_slug FROM events WHERE id = ?`, eventID).
		Scan(&d.EventID, &d.Slug, &d.Title, &venue, &category, &date, &d.URL, &image, &hub)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errEventGone
	}
	if err != nil {
		return nil, fmt.Errorf("load event %d: %w", eventID, err)
	}
	d.Venue, d.Category, d.EventDate, d.ImageURL, d.HubSlug = venue.String, category.String, date.String, image.String, hub.String
	d.Reason = event.ReasonDiscovered
	return &d, nil
}

func (p *Poller) recordSuccess(ctx context.Context, r row, messageID string) {
	now := outbox.Format(p.Now())
	var msg any
	if r.channel == outbox.ChannelTelegram {
		msg = messageID
	}
	res, err := p.DB.ExecContext(ctx, `
		UPDATE notifications SET status = 'sent', telegram_message_id = ?, confirmed_at = ?, error = NULL, next_attempt_at = NULL
		WHERE id = ? AND status = 'pending'`, msg, now, r.id)
	if err != nil {
		// The message went out; the row stays due and goes out again after
		// a restart. Say so loudly: this is the duplicate window.
		log.Printf("ERROR notification %d (%s, event %d) was sent but not recorded: %v", r.id, r.channel, r.eventID, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return
	}
	if r.channel == outbox.ChannelEmail {
		if err := repository.UpdateInviteSent(ctx, p.DB, r.eventID); err != nil {
			log.Printf("ERROR recording the sent invite for event %d: %v", r.eventID, err)
		}
	}
	log.Printf("Sent %s notification %d for event %d", r.channel, r.id, r.eventID)
}

func (p *Poller) recordFailure(ctx context.Context, r row, attempts int, cause error) {
	now := p.Now()
	msg := cause.Error()
	if errors.Is(cause, errEventGone) || attempts >= outbox.MaxAttempts {
		if _, err := p.DB.ExecContext(ctx, `
			UPDATE notifications SET status = 'failed', error = ?, confirmed_at = ?, next_attempt_at = NULL
			WHERE id = ? AND status = 'pending'`, msg, outbox.Format(now), r.id); err != nil {
			log.Printf("ERROR parking notification %d: %v", r.id, err)
			return
		}
		if r.channel == outbox.ChannelEmail {
			if err := repository.UpdateInviteFailed(ctx, p.DB, r.eventID, msg); err != nil {
				log.Printf("ERROR recording the failed invite for event %d: %v", r.eventID, err)
			}
		}
		log.Printf("Giving up on %s notification %d for event %d after %d attempt(s): %v", r.channel, r.id, r.eventID, attempts, cause)
		return
	}

	wait := outbox.BackoffAfter(attempts)
	var rl *telegram.RateLimitedError
	if errors.As(cause, &rl) {
		wait = max(wait, rl.RetryAfter+time.Second)
	}
	if _, err := p.DB.ExecContext(ctx, `
		UPDATE notifications SET error = ?, next_attempt_at = ? WHERE id = ? AND status = 'pending'`,
		msg, outbox.Format(now.Add(wait)), r.id); err != nil {
		log.Printf("ERROR scheduling the retry of notification %d: %v", r.id, err)
		return
	}
	log.Printf("Failed to send %s notification %d for event %d (attempt %d/%d): %v. Retrying in %v",
		r.channel, r.id, r.eventID, attempts, outbox.MaxAttempts, cause, wait)
}
