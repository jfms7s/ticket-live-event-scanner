// Package handler processes discovered-event messages: sending a Telegram
// notification for each and publishing the resulting sent/failed status
// back to NATS JetStream, with retry/backoff and redelivery deduplication.
package handler

import (
	"context"
	"sync"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/telegram"
	"github.com/nats-io/nats.go/jetstream"
)

// publisher is the part of jetstream.JetStream the handler uses, so tests
// can substitute a fake.
type publisher interface {
	Publish(ctx context.Context, subject string, data []byte, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error)
}

// Handler processes discovered events
type Handler struct {
	js publisher
	// send delivers one notification to Telegram and returns its message_id.
	send func(ctx context.Context, d event.Discovered) (string, error)
	// recentlySent tracks recently sent Telegram messages by event_id to prevent
	// duplicates if NATS publish fails and the message is redelivered.
	// Key: event_id, Value: (message_id, sent_time)
	mu           sync.RWMutex
	recentlySent map[int64]*recentSend
}

type recentSend struct {
	natsSeq   uint64 // NATS stream sequence to prevent cache hit on manual retries
	messageID string
	sentAt    time.Time
}

// New creates a new message handler
func New(botToken, chatID string, js jetstream.JetStream) *Handler {
	return &Handler{
		js: js,
		send: func(ctx context.Context, d event.Discovered) (string, error) {
			return telegram.SendTelegramMessage(ctx, botToken, chatID, d)
		},
		recentlySent: make(map[int64]*recentSend),
	}
}

// CleanupCache periodically removes stale entries from the cache to prevent memory leaks
func (h *Handler) CleanupCache(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.mu.Lock()
			now := time.Now()
			for eventID, entry := range h.recentlySent {
				// Remove entries older than 2 minutes
				if now.Sub(entry.sentAt) > 2*time.Minute {
					delete(h.recentlySent, eventID)
				}
			}
			h.mu.Unlock()
		}
	}
}
