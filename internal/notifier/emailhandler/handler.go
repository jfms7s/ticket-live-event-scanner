// Package emailhandler processes purchased-event messages: sending a
// calendar-invite email for each and publishing the resulting sent/failed
// status back to NATS JetStream, with retry/backoff and redelivery
// deduplication.
package emailhandler

import (
	"context"
	"sync"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/email"
	"github.com/nats-io/nats.go/jetstream"
)

// Handler processes purchased events.
type Handler struct {
	smtp email.SMTPConfig
	to   []string
	js   jetstream.JetStream
	// recentlySent tracks recently sent emails by event_id to prevent
	// duplicates if NATS publish fails and the message is redelivered.
	mu           sync.RWMutex
	recentlySent map[int64]*recentSend
}

type recentSend struct {
	natsSeq   uint64 // NATS stream sequence to prevent cache hit on manual retries
	messageID string
	sentAt    time.Time
}

// New creates a new message handler.
func New(smtpCfg email.SMTPConfig, to []string, js jetstream.JetStream) *Handler {
	return &Handler{
		smtp:         smtpCfg,
		to:           to,
		js:           js,
		recentlySent: make(map[int64]*recentSend),
	}
}

// CleanupCache periodically removes stale entries from the cache to prevent memory leaks.
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
