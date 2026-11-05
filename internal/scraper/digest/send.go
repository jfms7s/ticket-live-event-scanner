package digest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"
	"unicode/utf8"

	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/telegram"
)

// Sender posts the digest text to the chat and returns the message id.
type Sender func(ctx context.Context, text string) (string, error)

// Send builds this week's digest and posts it once. A row in the digests
// table (by ISO week) records that the week's digest went out, so a second
// call the same week (a manual --digest-once, a retry after a restart) posts
// nothing.
//
// A send that failed with an unknown outcome (the request may have reached
// Telegram) is recorded as sent and not retried: a second post to a shared
// channel is worse than a missed weekly summary. Any other failure is
// returned, for the caller to retry.
func Send(ctx context.Context, db *sql.DB, runLog RunLog, send Sender, now time.Time) error {
	d, err := Build(ctx, db, runLog, now)
	if err != nil {
		return fmt.Errorf("build digest: %w", err)
	}

	var done int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM digests WHERE week = ?`, d.Week).Scan(&done); err != nil {
		return fmt.Errorf("check digests: %w", err)
	}
	if done > 0 {
		log.Printf("Digest for %s was already sent this week; not sent again", d.Week)
		return nil
	}

	text := Format(d)
	if n := utf8.RuneCountInString(text); n > MaxMessageLen {
		return fmt.Errorf("digest is %d characters, over Telegram's %d", n, MaxMessageLen)
	}

	id, err := send(ctx, text)
	switch {
	case errors.Is(err, telegram.ErrDeliveryUnknown):
		log.Printf("Digest %s may or may not have been sent (%v); not retrying", d.Week, err)
	case err != nil:
		return fmt.Errorf("send digest: %w", err)
	default:
		log.Printf("Sent the digest for %s (message_id: %s; %d upcoming, %d new)", d.Week, id, d.UpcomingTotal, d.NewCount)
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO digests (week, sent_at) VALUES (?, ?)`,
		d.Week, now.UTC().Format("2006-01-02T15:04:05Z")); err != nil {
		return fmt.Errorf("record digest %s as sent: %w", d.Week, err)
	}
	return nil
}
