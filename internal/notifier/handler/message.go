package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/policy"
	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/telegram"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/nats-io/nats.go/jetstream"
)

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
		delay := policy.BackoffDelay(numDelivered)
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
			delay := policy.BackoffDelay(numDelivered)
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
	telegramMessageID, telegramErr := telegram.SendTelegramMessage(ctx, h.telegramBotToken, h.telegramChatID, discovered)

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
			delay := policy.BackoffDelay(numDelivered)
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
	action := policy.DecideAction(numDelivered, streams.EventsConsumerMaxDeliver)

	if action == policy.ActionNak {
		// Redelivery attempts remaining, nak with exponential backoff
		delay := policy.BackoffDelay(numDelivered)
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
