package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/telegram"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/nats-io/nats.go/jetstream"
)

// DigestHandler sends the weekly digest (scraper.digest) to the chat as
// is. Unlike events, a digest has no delivery result to report: it is sent
// and acked, retried over about an hour when Telegram refused it, and
// dropped after streams.DefaultMaxDeliver attempts, when it may have been
// posted already (a timeout), or when it is over 12 hours old.
type DigestHandler struct {
	send func(ctx context.Context, text string) (string, error)

	// sentWeeks remembers the weeks this process has sent, so a
	// redelivery after a send whose ack was lost isn't sent again. In
	// memory only: a restart in that gap can still send twice.
	mu        sync.Mutex
	sentWeeks map[string]bool

	now func() time.Time
}

// maxDigestAge is how old a digest may be when it is sent: one held while
// the Telegram channel was down is stale news by the next morning.
const maxDigestAge = 12 * time.Hour

// digestRetryDelays space the digest's redeliveries over about an hour, so
// a short Telegram outage doesn't cost the week's digest.
var digestRetryDelays = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute}

// digestRetryDelay is the wait before redelivery attempt numDelivered+1,
// at least Telegram's retry_after for a rate limit.
func digestRetryDelay(err error, numDelivered int) time.Duration {
	i := min(max(numDelivered-1, 0), len(digestRetryDelays)-1)
	delay := digestRetryDelays[i]
	var rl *telegram.RateLimitedError
	if errors.As(err, &rl) && rl.RetryAfter > delay {
		delay = rl.RetryAfter
	}
	return delay
}

func (h *DigestHandler) markSent(week string) {
	h.mu.Lock()
	h.sentWeeks[week] = true
	h.mu.Unlock()
}

// NewDigest returns a DigestHandler sending to chatID.
func NewDigest(botToken, chatID string) *DigestHandler {
	return &DigestHandler{
		send: func(ctx context.Context, text string) (string, error) {
			return telegram.SendText(ctx, botToken, chatID, text)
		},
		sentWeeks: map[string]bool{},
		now:       time.Now,
	}
}

// HandleMessage sends one digest message.
func (h *DigestHandler) HandleMessage(ctx context.Context, msg jetstream.Msg) error {
	var d event.Digest
	if err := json.Unmarshal(msg.Data(), &d); err != nil || d.Text == "" {
		log.Printf("Dropping a malformed digest message (err: %v)", err)
		if err := msg.Term(); err != nil {
			log.Printf("Failed to term malformed digest: %v", err)
		}
		return nil
	}

	if age := h.now().Sub(d.GeneratedAt); !d.GeneratedAt.IsZero() && age > maxDigestAge {
		log.Printf("Dropping digest %s: built %v ago, too old to be news", d.Week, age.Round(time.Minute))
		if err := msg.Term(); err != nil {
			log.Printf("Failed to term stale digest %s: %v", d.Week, err)
		}
		return nil
	}

	h.mu.Lock()
	already := h.sentWeeks[d.Week]
	h.mu.Unlock()
	if already {
		log.Printf("Digest %s already sent; acking the redelivery", d.Week)
		return msg.Ack()
	}

	messageID, err := h.send(ctx, d.Text)
	if errors.Is(err, telegram.ErrDeliveryUnknown) {
		// It may have been posted: a retry could post it twice to a
		// shared channel. Missing one weekly digest is the lesser harm.
		h.markSent(d.Week)
		log.Printf("Digest %s may or may not have been sent (%v); not retrying", d.Week, err)
		if terr := msg.Term(); terr != nil {
			log.Printf("Failed to term digest %s: %v", d.Week, terr)
		}
		return nil
	}
	if err != nil {
		numDelivered := 1
		if meta, merr := msg.Metadata(); merr == nil {
			numDelivered = int(meta.NumDelivered)
		}
		if numDelivered >= streams.DefaultMaxDeliver {
			log.Printf("Giving up on digest %s after %d attempts: %v", d.Week, numDelivered, err)
			if terr := msg.Term(); terr != nil {
				log.Printf("Failed to term digest %s: %v", d.Week, terr)
			}
			return nil
		}
		delay := digestRetryDelay(err, numDelivered)
		log.Printf("Failed to send digest %s (attempt %d/%d): %v. Retrying in %v",
			d.Week, numDelivered, streams.DefaultMaxDeliver, err, delay)
		if nerr := msg.NakWithDelay(delay); nerr != nil {
			log.Printf("Failed to nak digest %s: %v", d.Week, nerr)
		}
		return nil
	}

	h.markSent(d.Week)
	log.Printf("Sent digest %s (message_id: %s)", d.Week, messageID)
	return msg.Ack()
}
