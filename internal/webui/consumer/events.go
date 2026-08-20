// Package consumer runs the NATS JetStream consumers that feed the
// web-ui-api database: materializing discovered events and applying
// notification status updates.
package consumer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/jfms7s/ticket-live-event-scanner/internal/webui/repository"
	"github.com/nats-io/nats.go/jetstream"
)

// CreateOrUpdateEventsConsumer creates or updates the durable consumer
// used by web-ui-api to read events.discovered.
func CreateOrUpdateEventsConsumer(ctx context.Context, js jetstream.JetStream) (jetstream.Consumer, error) {
	stream, err := js.Stream(ctx, streams.EventsStreamName)
	if err != nil {
		return nil, fmt.Errorf("get %s stream: %w", streams.EventsStreamName, err)
	}

	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       "web-ui-api-events",
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    streams.EventsConsumerMaxDeliver,
		AckWait:       streams.EventsConsumerAckWait,
		FilterSubject: streams.EventsSubject,
	})
	if err != nil {
		return nil, fmt.Errorf("create events consumer: %w", err)
	}

	return consumer, nil
}

// ConsumeEventsDiscovered reads from the events.discovered consumer and
// materializes events into the database.
func ConsumeEventsDiscovered(ctx context.Context, consumer jetstream.Consumer, db *sql.DB) {
	msgsChan, err := consumer.Messages()
	if err != nil {
		log.Printf("Failed to get messages from events consumer: %v", err)
		return
	}
	defer msgsChan.Stop()

	// Watcher goroutine to stop the message channel on context cancellation
	go func() {
		<-ctx.Done()
		msgsChan.Stop()
	}()

	for {
		msg, err := msgsChan.Next()
		if err != nil {
			log.Printf("Error receiving message: %v", err)
			return
		}

		var disc event.Discovered
		if err := json.Unmarshal(msg.Data(), &disc); err != nil {
			log.Printf("Failed to unmarshal Discovered message: %v", err)
			// Malformed message will never become parseable on retry, so terminate it
			msg.Term()
			continue
		}

		// Upsert event and insert pending notification in a transaction
		if err := repository.UpsertEventAndInsertNotification(ctx, db, &disc); err != nil {
			log.Printf("Failed to materialize event %d: %v", disc.EventID, err)
			msg.Nak()
			continue
		}

		msg.Ack()
	}
}
