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

// notificationsConsumerConfig is the durable web-ui-api-notifications
// consumer. It lists the subjects handleNotification handles explicitly:
// the email results have three tokens, which a "notifications.*" filter
// never matched. JetStream lets CreateOrUpdateConsumer change the filter of
// an existing durable pull consumer, which keeps its position.
func notificationsConsumerConfig() jetstream.ConsumerConfig {
	return jetstream.ConsumerConfig{
		Durable:        "web-ui-api-notifications",
		AckPolicy:      jetstream.AckExplicitPolicy,
		MaxDeliver:     streams.EventsConsumerMaxDeliver,
		AckWait:        streams.EventsConsumerAckWait,
		FilterSubjects: streams.NotificationsSubjects(),
	}
}

// CreateOrUpdateNotificationsConsumer creates or updates the durable
// consumer used by web-ui-api to read Telegram and email delivery results.
func CreateOrUpdateNotificationsConsumer(ctx context.Context, js jetstream.JetStream) (jetstream.Consumer, error) {
	stream, err := js.Stream(ctx, streams.NotificationsStreamName)
	if err != nil {
		return nil, fmt.Errorf("get %s stream: %w", streams.NotificationsStreamName, err)
	}

	consumer, err := stream.CreateOrUpdateConsumer(ctx, notificationsConsumerConfig())
	if err != nil {
		return nil, fmt.Errorf("create notifications consumer: %w", err)
	}

	return consumer, nil
}

// ConsumeNotifications reads from the notifications consumer and applies
// each delivery result to the database.
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
		handleNotification(ctx, db, msg)
	}
}

// handleNotification applies one delivery result and acks, naks or
// terminates msg: malformed bodies are terminated (they never parse on
// retry), database errors are nak'd for redelivery, and a subject it has
// no branch for is logged and acked.
func handleNotification(ctx context.Context, db *sql.DB, msg jetstream.Msg) {
	subject := msg.Subject()

	switch subject {
	case streams.NotificationsSentSubject:
		var sent event.NotificationSent
		if !decode(msg, &sent, "NotificationSent") {
			return
		}
		if err := repository.UpdateNotificationSent(ctx, db, &sent); err != nil {
			log.Printf("Failed to update notification sent for event %d: %v", sent.EventID, err)
			msg.Nak()
			return
		}

	case streams.NotificationsFailSubject:
		var failed event.NotificationFailed
		if !decode(msg, &failed, "NotificationFailed") {
			return
		}
		if err := repository.UpdateNotificationFailed(ctx, db, &failed); err != nil {
			log.Printf("Failed to update notification failed for event %d: %v", failed.EventID, err)
			msg.Nak()
			return
		}

	case streams.NotificationsEmailSentSubject:
		var sent event.EmailSent
		if !decode(msg, &sent, "EmailSent") {
			return
		}
		if err := repository.UpdateInviteSent(ctx, db, sent.EventID); err != nil {
			log.Printf("Failed to update invite sent for event %d: %v", sent.EventID, err)
			msg.Nak()
			return
		}

	case streams.NotificationsEmailFailSubject:
		var failed event.EmailFailed
		if !decode(msg, &failed, "EmailFailed") {
			return
		}
		if err := repository.UpdateInviteFailed(ctx, db, failed.EventID, failed.Error); err != nil {
			log.Printf("Failed to update invite failed for event %d: %v", failed.EventID, err)
			msg.Nak()
			return
		}

	default:
		log.Printf("WARN unhandled subject %s", subject)
	}

	msg.Ack()
}

// decode unmarshals msg into v, terminating msg and returning false if the
// body is malformed, since it will never become parseable on retry.
func decode(msg jetstream.Msg, v any, name string) bool {
	if err := json.Unmarshal(msg.Data(), v); err != nil {
		log.Printf("Failed to unmarshal %s message: %v", name, err)
		msg.Term()
		return false
	}
	return true
}
