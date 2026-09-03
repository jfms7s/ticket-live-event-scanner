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

// CreateOrUpdateNotificationsConsumer creates or updates the durable
// consumer used by web-ui-api to read notifications.sent and notifications.failed.
func CreateOrUpdateNotificationsConsumer(ctx context.Context, js jetstream.JetStream) (jetstream.Consumer, error) {
	stream, err := js.Stream(ctx, streams.NotificationsStreamName)
	if err != nil {
		return nil, fmt.Errorf("get %s stream: %w", streams.NotificationsStreamName, err)
	}

	consumer, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:       "web-ui-api-notifications",
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    streams.EventsConsumerMaxDeliver,
		AckWait:       streams.EventsConsumerAckWait,
		FilterSubject: streams.NotificationsAllSubjects,
	})
	if err != nil {
		return nil, fmt.Errorf("create notifications consumer: %w", err)
	}

	return consumer, nil
}

// ConsumeNotifications reads from the notifications consumer and updates
// notification statuses in the database.
func ConsumeNotifications(ctx context.Context, consumer jetstream.Consumer, db *sql.DB) {
	msgsChan, err := consumer.Messages()
	if err != nil {
		log.Printf("Failed to get messages from notifications consumer: %v", err)
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

		// Determine message type by subject
		subject := msg.Subject()

		if subject == streams.NotificationsSentSubject {
			var sent event.NotificationSent
			if err := json.Unmarshal(msg.Data(), &sent); err != nil {
				log.Printf("Failed to unmarshal NotificationSent message: %v", err)
				// Malformed message will never become parseable on retry, so terminate it
				msg.Term()
				continue
			}

			if err := repository.UpdateNotificationSent(ctx, db, &sent); err != nil {
				log.Printf("Failed to update notification sent for event %d: %v", sent.EventID, err)
				msg.Nak()
				continue
			}
		} else if subject == streams.NotificationsFailSubject {
			var failed event.NotificationFailed
			if err := json.Unmarshal(msg.Data(), &failed); err != nil {
				log.Printf("Failed to unmarshal NotificationFailed message: %v", err)
				// Malformed message will never become parseable on retry, so terminate it
				msg.Term()
				continue
			}

			if err := repository.UpdateNotificationFailed(ctx, db, &failed); err != nil {
				log.Printf("Failed to update notification failed for event %d: %v", failed.EventID, err)
				msg.Nak()
				continue
			}
		}

		msg.Ack()
	}
}
