package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func main() {
	// Load environment variables
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = "nats://localhost:4222"
	}

	telegramBotToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if telegramBotToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN environment variable is required")
	}

	telegramChatID := os.Getenv("TELEGRAM_CHAT_ID")
	if telegramChatID == "" {
		log.Fatal("TELEGRAM_CHAT_ID environment variable is required")
	}

	// Connect to NATS
	nc, err := nats.Connect(natsURL)
	if err != nil {
		log.Fatalf("Failed to connect to NATS: %v", err)
	}
	defer nc.Close()

	// Get JetStream context
	js, err := jetstream.New(nc)
	if err != nil {
		log.Fatalf("Failed to create JetStream context: %v", err)
	}

	// Setup context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Ensure streams exist
	if err := streams.EnsureStreams(ctx, js); err != nil {
		log.Fatalf("Failed to ensure streams: %v", err)
	}

	// Get or create consumer
	consumer, err := streams.EnsureEventsConsumer(ctx, js)
	if err != nil {
		log.Fatalf("Failed to ensure consumer: %v", err)
	}

	// Create handler
	handler := NewHandler(telegramBotToken, telegramChatID, js)

	// Setup signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start cache cleanup goroutine (removes stale entries every 30 seconds)
	go handler.cleanupCache(ctx)

	// Start consuming messages
	go func() {
		<-sigChan
		log.Println("Received shutdown signal, gracefully stopping...")
		cancel()
	}()

	// Consume messages
	log.Println("Starting to consume events...")
	consumeMessages(ctx, consumer, handler)
	log.Println("Shutdown complete")
}

func consumeMessages(ctx context.Context, consumer jetstream.Consumer, handler *Handler) {
	// Use Consume with a callback handler
	// Consume() is non-blocking; it registers the callback and returns immediately
	consumeCtx, err := consumer.Consume(
		func(msg jetstream.Msg) {
			// Process the message in the callback
			if err := handler.HandleMessage(ctx, msg); err != nil {
				log.Printf("Error handling message: %v", err)
			}
		},
		jetstream.ConsumeErrHandler(func(consumeCtx jetstream.ConsumeContext, err error) {
			log.Printf("Consume error: %v", err)
		}),
	)

	if err != nil {
		log.Fatalf("Failed to consume messages: %v", err)
	}

	// Wait for context cancellation
	<-ctx.Done()

	// Stop message delivery cleanly before closing the connection
	// This ensures in-flight callbacks complete and acks are sent
	consumeCtx.Stop()
}

// Handler processes discovered events
type Handler struct {
	telegramBotToken string
	telegramChatID   string
	js               jetstream.JetStream
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

// NewHandler creates a new message handler
func NewHandler(botToken, chatID string, js jetstream.JetStream) *Handler {
	return &Handler{
		telegramBotToken: botToken,
		telegramChatID:   chatID,
		js:               js,
		recentlySent:     make(map[int64]*recentSend),
	}
}

// cleanupCache periodically removes stale entries from the cache to prevent memory leaks
func (h *Handler) cleanupCache(ctx context.Context) {
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

// HandleMessage processes a single event message
func (h *Handler) HandleMessage(ctx context.Context, msg jetstream.Msg) error {
	var discovered event.Discovered
	if err := json.Unmarshal(msg.Data(), &discovered); err != nil {
		// Malformed message, ack it so we don't reprocess forever
		log.Printf("Failed to unmarshal message: %v, acking anyway", err)
		if err := msg.Ack(); err != nil {
			log.Printf("Failed to ack malformed message: %v", err)
		}
		return nil
	}

	// Get message metadata - critical for determining delivery count and making decisions
	meta, err := msg.Metadata()
	var numDelivered int
	var natsSeq uint64

	if err != nil {
		log.Printf("Failed to get message metadata for event %d: %v", discovered.EventID, err)
		// Fallback: try to read delivery count from message headers
		numDeliveredStr := msg.Headers().Get("Nats-Num-Delivered")
		if numDeliveredStr != "" {
			// Parse the header value
			if parsed, parseErr := strconv.Atoi(numDeliveredStr); parseErr == nil {
				numDelivered = parsed
			} else {
				log.Printf("Failed to parse Nats-Num-Delivered header for event %d: %v", discovered.EventID, parseErr)
				numDelivered = 1 // Conservative default
			}
		} else {
			numDelivered = 1 // Conservative default if header is missing
		}
		log.Printf("Using fallback delivery count %d for event %d after metadata failure", numDelivered, discovered.EventID)

		// Check if we've hit the delivery limit
		if numDelivered >= streams.EventsConsumerMaxDeliver {
			// Final attempt: publish notification.failed and term
			// Use event ID as fallback sequence when metadata is unavailable
			log.Printf("Event %d reached max delivery attempts (%d) with metadata failure", discovered.EventID, numDelivered)
			if err := h.publishNotificationFailed(ctx, discovered.EventID, fmt.Sprintf("metadata error: %v", err), numDelivered, uint64(discovered.EventID)); err != nil {
				log.Printf("Failed to publish notification.failed for event %d: %v", discovered.EventID, err)
			}
			if err := msg.Term(); err != nil {
				log.Printf("Failed to terminate message for event %d: %v", discovered.EventID, err)
			}
			return nil
		}

		// Retry with exponential backoff
		delay := BackoffDelay(numDelivered)
		log.Printf("Retrying metadata failure for event %d (attempt %d/%d) in %v",
			discovered.EventID, numDelivered, streams.EventsConsumerMaxDeliver, delay)
		if err := msg.NakWithDelay(delay); err != nil {
			log.Printf("Failed to nak message for event %d: %v", discovered.EventID, err)
		}
		return nil
	}

	numDelivered = int(meta.NumDelivered)
	natsSeq = meta.Sequence.Stream

	// Check if we've recently sent a message for this event to prevent duplicates
	// if NATS publish failed and the message was redelivered.
	// Verify NATS sequence matches to ensure it's the same message (not a manual retry).
	h.mu.RLock()
	recent, exists := h.recentlySent[discovered.EventID]
	h.mu.RUnlock()

	if exists && time.Since(recent.sentAt) < 1*time.Minute && recent.natsSeq == natsSeq {
		// We recently sent this event; republish the notification with the cached message ID
		// But still respect the MaxDeliver policy for this redelivery attempt
		log.Printf("Event %d was recently sent (message_id: %s), attempting to republish notifications.sent",
			discovered.EventID, recent.messageID)
		if err := h.publishNotificationSent(ctx, discovered.EventID, recent.messageID, natsSeq); err != nil {
			log.Printf("Failed to republish notification.sent for event %d (attempt %d/%d): %v",
				discovered.EventID, numDelivered, streams.EventsConsumerMaxDeliver, err)

			// Respect MaxDeliver policy for the republish attempt
			if numDelivered >= streams.EventsConsumerMaxDeliver {
				// Final attempt: publish notification.failed and term
				_ = h.publishNotificationFailed(ctx, discovered.EventID, err.Error(), numDelivered, natsSeq)
				if err := msg.Term(); err != nil {
					log.Printf("Failed to terminate message for event %d: %v", discovered.EventID, err)
				}
				log.Printf("Final attempt failed to republish notification for event %d after %d attempts",
					discovered.EventID, numDelivered)
				return nil
			}

			// Retry with exponential backoff
			delay := BackoffDelay(numDelivered)
			log.Printf("Retrying republish in %v", delay)
			if err := msg.NakWithDelay(delay); err != nil {
				log.Printf("Failed to nak message for event %d: %v", discovered.EventID, err)
			}
			return nil
		}
		if err := msg.Ack(); err != nil {
			log.Printf("Failed to ack message for event %d: %v", discovered.EventID, err)
			return err
		}
		log.Printf("Successfully republished notification for event %d (cached message_id: %s)", discovered.EventID, recent.messageID)
		return nil
	}

	// Try to send Telegram message
	telegramMessageID, telegramErr := SendTelegramMessage(ctx, h.telegramBotToken, h.telegramChatID, discovered)

	if telegramErr == nil {
		// Success: IMMEDIATELY record in cache to protect against NATS publish failures
		// This ensures that even if the next step (NATS publish) fails, we won't send a duplicate
		// Telegram message on redelivery
		h.mu.Lock()
		h.recentlySent[discovered.EventID] = &recentSend{
			natsSeq:   natsSeq,
			messageID: telegramMessageID,
			sentAt:    time.Now(),
		}
		h.mu.Unlock()

		// Now attempt to publish notification.sent to NATS (this may fail without affecting duplicate prevention)
		if err := h.publishNotificationSent(ctx, discovered.EventID, telegramMessageID, natsSeq); err != nil {
			log.Printf("Failed to publish notification.sent for event %d (attempt %d/%d): %v",
				discovered.EventID, numDelivered, streams.EventsConsumerMaxDeliver, err)

			// If publish fails, respect MaxDeliver policy for retry
			if numDelivered >= streams.EventsConsumerMaxDeliver {
				// Final attempt: publish notification.failed and term
				if err := h.publishNotificationFailed(ctx, discovered.EventID, err.Error(), numDelivered, natsSeq); err != nil {
					log.Printf("Failed to publish notification.failed for event %d (final attempt): %v",
						discovered.EventID, err)
				}
				if err := msg.Term(); err != nil {
					log.Printf("Failed to terminate message for event %d: %v", discovered.EventID, err)
				}
				log.Printf("Final attempt failed to publish notification for event %d after %d attempts",
					discovered.EventID, numDelivered)
				return nil
			}

			// Retry with exponential backoff
			delay := BackoffDelay(numDelivered)
			if err := msg.NakWithDelay(delay); err != nil {
				log.Printf("Failed to nak message for event %d: %v", discovered.EventID, err)
			}
			return nil
		}

		if err := msg.Ack(); err != nil {
			log.Printf("Failed to ack message for event %d: %v", discovered.EventID, err)
			return err
		}
		log.Printf("Successfully sent notification for event %d (message_id: %s)", discovered.EventID, telegramMessageID)
		return nil
	}

	// Failure: decide whether to nak with backoff or term
	action := DecideAction(numDelivered, streams.EventsConsumerMaxDeliver)

	if action == ActionNak {
		// Redelivery attempts remaining, nak with exponential backoff
		delay := BackoffDelay(numDelivered)
		log.Printf("Failed to send notification for event %d (attempt %d/%d): %v. Retrying in %v",
			discovered.EventID, numDelivered, streams.EventsConsumerMaxDeliver, telegramErr, delay)
		if err := msg.NakWithDelay(delay); err != nil {
			log.Printf("Failed to nak message for event %d with delay: %v", discovered.EventID, err)
		}
		return nil
	}

	// Final attempt failed: publish notification.failed and term
	if err := h.publishNotificationFailed(ctx, discovered.EventID, telegramErr.Error(), numDelivered, natsSeq); err != nil {
		log.Printf("Failed to publish notification.failed for event %d: %v", discovered.EventID, err)
	}

	if err := msg.Term(); err != nil {
		log.Printf("Failed to terminate message for event %d: %v", discovered.EventID, err)
	}

	log.Printf("Final delivery attempt failed for event %d after %d attempts: %v",
		discovered.EventID, numDelivered, telegramErr)

	return nil
}

func (h *Handler) publishNotificationSent(ctx context.Context, eventID int64, messageID string, natsSeq uint64) error {
	sent := event.NotificationSent{
		EventID:           eventID,
		TelegramMessageID: messageID,
		SentAt:            now(),
	}

	data, err := json.Marshal(sent)
	if err != nil {
		return fmt.Errorf("marshal notification.sent: %w", err)
	}

	msgID := fmt.Sprintf("sent-%d-%d", eventID, natsSeq)
	_, err = h.js.Publish(ctx, streams.NotificationsSentSubject, data, jetstream.WithMsgID(msgID))
	if err != nil {
		return fmt.Errorf("publish notification.sent: %w", err)
	}

	return nil
}

func (h *Handler) publishNotificationFailed(ctx context.Context, eventID int64, errMsg string, attempts int, natsSeq uint64) error {
	failed := event.NotificationFailed{
		EventID:  eventID,
		Error:    errMsg,
		FailedAt: now(),
		Attempts: attempts,
	}

	data, err := json.Marshal(failed)
	if err != nil {
		return fmt.Errorf("marshal notification.failed: %w", err)
	}

	msgID := fmt.Sprintf("failed-%d-%d", eventID, natsSeq)
	_, err = h.js.Publish(ctx, streams.NotificationsFailSubject, data, jetstream.WithMsgID(msgID))
	if err != nil {
		return fmt.Errorf("publish notification.failed: %w", err)
	}

	return nil
}
