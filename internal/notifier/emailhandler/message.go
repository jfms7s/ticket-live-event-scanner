package emailhandler

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/email"
	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/policy"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/nats-io/nats.go/jetstream"
)

// HandleMessage processes a single purchased-event message.
func (h *Handler) HandleMessage(ctx context.Context, msg jetstream.Msg) error {
	var purchased event.Purchased
	if err := json.Unmarshal(msg.Data(), &purchased); err != nil {
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
		log.Printf("Failed to get message metadata for event %d: %v", purchased.EventID, err)
		// Fallback: try to read delivery count from message headers
		numDeliveredStr := msg.Headers().Get("Nats-Num-Delivered")
		if numDeliveredStr != "" {
			if parsed, parseErr := strconv.Atoi(numDeliveredStr); parseErr == nil {
				numDelivered = parsed
			} else {
				log.Printf("Failed to parse Nats-Num-Delivered header for event %d: %v", purchased.EventID, parseErr)
				numDelivered = 1 // Conservative default
			}
		} else {
			numDelivered = 1 // Conservative default if header is missing
		}
		log.Printf("Using fallback delivery count %d for event %d after metadata failure", numDelivered, purchased.EventID)

		if numDelivered >= streams.EventsConsumerMaxDeliver {
			log.Printf("Event %d reached max delivery attempts (%d) with metadata failure", purchased.EventID, numDelivered)
			if err := h.publishEmailFailed(ctx, purchased.EventID, fmt.Sprintf("metadata error: %v", err), numDelivered, uint64(purchased.EventID)); err != nil {
				log.Printf("Failed to publish notifications.email.failed for event %d: %v", purchased.EventID, err)
			}
			if err := msg.Term(); err != nil {
				log.Printf("Failed to terminate message for event %d: %v", purchased.EventID, err)
			}
			return nil
		}

		delay := policy.BackoffDelay(numDelivered)
		log.Printf("Retrying metadata failure for event %d (attempt %d/%d) in %v",
			purchased.EventID, numDelivered, streams.EventsConsumerMaxDeliver, delay)
		if err := msg.NakWithDelay(delay); err != nil {
			log.Printf("Failed to nak message for event %d: %v", purchased.EventID, err)
		}
		return nil
	}

	numDelivered = int(meta.NumDelivered)
	natsSeq = meta.Sequence.Stream

	// Check if we've recently sent an email for this event to prevent duplicates
	// if NATS publish failed and the message was redelivered.
	h.mu.RLock()
	recent, exists := h.recentlySent[purchased.EventID]
	h.mu.RUnlock()

	if exists && time.Since(recent.sentAt) < 1*time.Minute && recent.natsSeq == natsSeq {
		log.Printf("Event %d was recently sent (message_id: %s), attempting to republish notifications.email.sent",
			purchased.EventID, recent.messageID)
		if err := h.publishEmailSent(ctx, purchased.EventID, recent.messageID, natsSeq); err != nil {
			log.Printf("Failed to republish notifications.email.sent for event %d (attempt %d/%d): %v",
				purchased.EventID, numDelivered, streams.EventsConsumerMaxDeliver, err)

			if numDelivered >= streams.EventsConsumerMaxDeliver {
				_ = h.publishEmailFailed(ctx, purchased.EventID, err.Error(), numDelivered, natsSeq)
				if err := msg.Term(); err != nil {
					log.Printf("Failed to terminate message for event %d: %v", purchased.EventID, err)
				}
				log.Printf("Final attempt failed to republish notification for event %d after %d attempts",
					purchased.EventID, numDelivered)
				return nil
			}

			delay := policy.BackoffDelay(numDelivered)
			log.Printf("Retrying republish in %v", delay)
			if err := msg.NakWithDelay(delay); err != nil {
				log.Printf("Failed to nak message for event %d: %v", purchased.EventID, err)
			}
			return nil
		}
		if err := msg.Ack(); err != nil {
			log.Printf("Failed to ack message for event %d: %v", purchased.EventID, err)
			return err
		}
		log.Printf("Successfully republished notification for event %d (cached message_id: %s)", purchased.EventID, recent.messageID)
		return nil
	}

	// Try to send the calendar-invite email
	messageID, sendErr := email.SendCalendarEmail(ctx, h.smtp, h.to, purchased)

	if sendErr == nil {
		// Success: IMMEDIATELY record in cache to protect against NATS publish failures
		h.mu.Lock()
		h.recentlySent[purchased.EventID] = &recentSend{
			natsSeq:   natsSeq,
			messageID: messageID,
			sentAt:    time.Now(),
		}
		h.mu.Unlock()

		if err := h.publishEmailSent(ctx, purchased.EventID, messageID, natsSeq); err != nil {
			log.Printf("Failed to publish notifications.email.sent for event %d (attempt %d/%d): %v",
				purchased.EventID, numDelivered, streams.EventsConsumerMaxDeliver, err)

			if numDelivered >= streams.EventsConsumerMaxDeliver {
				if err := h.publishEmailFailed(ctx, purchased.EventID, err.Error(), numDelivered, natsSeq); err != nil {
					log.Printf("Failed to publish notifications.email.failed for event %d (final attempt): %v",
						purchased.EventID, err)
				}
				if err := msg.Term(); err != nil {
					log.Printf("Failed to terminate message for event %d: %v", purchased.EventID, err)
				}
				log.Printf("Final attempt failed to publish notification for event %d after %d attempts",
					purchased.EventID, numDelivered)
				return nil
			}

			delay := policy.BackoffDelay(numDelivered)
			if err := msg.NakWithDelay(delay); err != nil {
				log.Printf("Failed to nak message for event %d: %v", purchased.EventID, err)
			}
			return nil
		}

		if err := msg.Ack(); err != nil {
			log.Printf("Failed to ack message for event %d: %v", purchased.EventID, err)
			return err
		}
		log.Printf("Successfully sent calendar email for event %d (message_id: %s)", purchased.EventID, messageID)
		return nil
	}

	// Failure: decide whether to nak with backoff or term
	action := policy.DecideAction(numDelivered, streams.EventsConsumerMaxDeliver)

	if action == policy.ActionNak {
		delay := policy.BackoffDelay(numDelivered)
		log.Printf("Failed to send calendar email for event %d (attempt %d/%d): %v. Retrying in %v",
			purchased.EventID, numDelivered, streams.EventsConsumerMaxDeliver, sendErr, delay)
		if err := msg.NakWithDelay(delay); err != nil {
			log.Printf("Failed to nak message for event %d with delay: %v", purchased.EventID, err)
		}
		return nil
	}

	if err := h.publishEmailFailed(ctx, purchased.EventID, sendErr.Error(), numDelivered, natsSeq); err != nil {
		log.Printf("Failed to publish notifications.email.failed for event %d: %v", purchased.EventID, err)
	}

	if err := msg.Term(); err != nil {
		log.Printf("Failed to terminate message for event %d: %v", purchased.EventID, err)
	}

	log.Printf("Final delivery attempt failed for event %d after %d attempts: %v",
		purchased.EventID, numDelivered, sendErr)

	return nil
}
