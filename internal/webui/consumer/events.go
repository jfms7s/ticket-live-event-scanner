// Package consumer runs the NATS JetStream consumers that feed the
// web-ui-api database: materializing discovered events and applying
// notification status updates.
package consumer

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/policy"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/jfms7s/ticket-live-event-scanner/internal/webui/repository"
	"github.com/nats-io/nats.go/jetstream"
)

// ConsumeEventsDiscovered reads from the events.discovered consumer and
// materializes events into the database.
func ConsumeEventsDiscovered(ctx context.Context, consumer jetstream.Consumer, db *sql.DB) {
	msgsChan, err := consumer.Messages()
	if err != nil {
		log.Printf("Failed to get messages from events consumer: %v", err)
		return
	}
	defer msgsChan.Stop()

	// Stop the message channel on shutdown; the goroutine ends with this
	// function, so a restart doesn't leave one behind.
	returned := make(chan struct{})
	defer close(returned)
	go func() {
		select {
		case <-ctx.Done():
			msgsChan.Stop()
		case <-returned:
		}
	}()

	// The message in hand runs to completion on a context shutdown doesn't
	// cancel, then Next returns an error and the loop ends.
	workCtx := context.WithoutCancel(ctx)

	for {
		msg, err := msgsChan.Next()
		if err != nil {
			log.Printf("Error receiving message: %v", err)
			return
		}

		handleDiscovered(workCtx, db, msg)
	}
}

// handleDiscovered materializes one events.discovered message and acks,
// naks (with backoff, see retryOnDBError) or terminates it. The message's EVENTS stream sequence becomes the
// notification row's delivery_seq; without metadata it is 0 (unknown).
func handleDiscovered(ctx context.Context, db *sql.DB, msg jetstream.Msg) {
	var disc event.Discovered
	if err := json.Unmarshal(msg.Data(), &disc); err != nil {
		log.Printf("Failed to unmarshal Discovered message: %v", err)
		// Malformed message will never become parseable on retry, so terminate it
		msg.Term()
		return
	}

	var deliverySeq uint64
	if meta, err := msg.Metadata(); err != nil {
		log.Printf("No metadata for event %d, recording it without delivery_seq: %v", disc.EventID, err)
	} else {
		deliverySeq = meta.Sequence.Stream
	}

	// Upsert event and insert pending notification in a transaction
	if err := repository.UpsertEventAndInsertNotification(ctx, db, &disc, deliverySeq); err != nil {
		retryOnDBError(msg, "materialize event", disc.EventID, err)
		return
	}

	ack(msg)
}

// retryOnDBError redelivers msg after a database error with the notifiers'
// linear backoff (5s × delivery number), so a short database outage is
// ridden out instead of burning every delivery in milliseconds. The last
// allowed delivery is logged as an ERROR and terminated. Without metadata
// the delivery is taken as the first.
func retryOnDBError(msg jetstream.Msg, what string, eventID int64, cause error) {
	delivered := 1
	if meta, err := msg.Metadata(); err == nil && meta.NumDelivered > 0 {
		delivered = int(meta.NumDelivered)
	}
	if policy.DecideAction(delivered, streams.DefaultMaxDeliver) == policy.ActionTerm {
		log.Printf("ERROR giving up on %s for event %d after %d deliveries: %s: %v", msg.Subject(), eventID, delivered, what, cause)
		if err := msg.Term(); err != nil {
			log.Printf("Error terminating %s for event %d: %v", msg.Subject(), eventID, err)
		}
		return
	}
	delay := policy.BackoffDelay(delivered)
	log.Printf("Failed to %s for event %d (delivery %d), retrying in %v: %v", what, eventID, delivered, delay, cause)
	if err := msg.NakWithDelay(delay); err != nil {
		log.Printf("Error nak'ing %s for event %d: %v", msg.Subject(), eventID, err)
	}
}

// ack acks msg, logging a failure (the message is then redelivered after
// AckWait).
func ack(msg jetstream.Msg) {
	if err := msg.Ack(); err != nil {
		log.Printf("Error acking %s: %v", msg.Subject(), err)
	}
}
