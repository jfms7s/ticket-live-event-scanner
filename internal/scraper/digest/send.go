package digest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"
	"unicode/utf8"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/nats-io/nats.go/jetstream"
)

// Publisher is the part of jetstream.JetStream Send uses.
type Publisher interface {
	Publish(ctx context.Context, subject string, payload []byte, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error)
}

// Send builds this week's digest and publishes it once on
// streams.DigestSubject (its own DIGEST stream) for the Telegram channel. The
// message ID is "digest-<ISO week>", so a second publish in the same week
// (a manual re-run, a retried Job) is dropped by the stream's 7-day dedup
// window.
func Send(ctx context.Context, db *sql.DB, runLog RunLog, pub Publisher, now time.Time) error {
	d, err := Build(ctx, db, runLog, now)
	if err != nil {
		return fmt.Errorf("build digest: %w", err)
	}
	text := Format(d)
	if n := utf8.RuneCountInString(text); n > MaxMessageLen {
		return fmt.Errorf("digest is %d characters, over Telegram's %d", n, MaxMessageLen)
	}
	data, err := json.Marshal(event.Digest{Week: d.Week, Text: text, GeneratedAt: now.UTC()})
	if err != nil {
		return fmt.Errorf("marshal digest: %w", err)
	}
	ack, err := pub.Publish(ctx, streams.DigestSubject, data, jetstream.WithMsgID("digest-"+d.Week))
	if err != nil {
		return fmt.Errorf("publish digest: %w", err)
	}
	if ack != nil && ack.Duplicate {
		log.Printf("Digest for %s was already published this week; not sent again", d.Week)
		return nil
	}
	log.Printf("Published the digest for %s (%d upcoming, %d new)", d.Week, d.UpcomingTotal, d.NewCount)
	return nil
}
