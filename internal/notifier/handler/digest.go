package handler

import (
	"context"
	"encoding/json"
	"log"
	"sync"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/telegram"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/nats-io/nats.go/jetstream"
)

// DigestHandler sends the weekly digest (scraper.digest) to the chat as
// is. Unlike events, a digest has no delivery result to report: it is sent
// and acked, retried with backoff on a failed send, and dropped after
// streams.DefaultMaxDeliver attempts (next week's digest follows).
type DigestHandler struct {
	send func(ctx context.Context, text string) (string, error)

	// sentWeeks remembers the weeks this process has sent, so a
	// redelivery after a send whose ack was lost isn't sent again. In
	// memory only: a restart in that gap can still send twice.
	mu        sync.Mutex
	sentWeeks map[string]bool
}

// NewDigest returns a DigestHandler sending to chatID.
func NewDigest(botToken, chatID string) *DigestHandler {
	return &DigestHandler{
		send: func(ctx context.Context, text string) (string, error) {
			return telegram.SendText(ctx, botToken, chatID, text)
		},
		sentWeeks: map[string]bool{},
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

	h.mu.Lock()
	already := h.sentWeeks[d.Week]
	h.mu.Unlock()
	if already {
		log.Printf("Digest %s already sent; acking the redelivery", d.Week)
		return msg.Ack()
	}

	messageID, err := h.send(ctx, d.Text)
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
		delay := retryDelay(err, numDelivered)
		log.Printf("Failed to send digest %s (attempt %d/%d): %v. Retrying in %v",
			d.Week, numDelivered, streams.DefaultMaxDeliver, err, delay)
		if nerr := msg.NakWithDelay(delay); nerr != nil {
			log.Printf("Failed to nak digest %s: %v", d.Week, nerr)
		}
		return nil
	}

	h.mu.Lock()
	h.sentWeeks[d.Week] = true
	h.mu.Unlock()
	log.Printf("Sent digest %s (message_id: %s)", d.Week, messageID)
	return msg.Ack()
}
