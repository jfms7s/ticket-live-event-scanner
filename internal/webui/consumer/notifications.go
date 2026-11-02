package consumer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/jfms7s/ticket-live-event-scanner/internal/webui/repository"
	"github.com/nats-io/nats.go/jetstream"
)

// ConsumeNotifications reads from the notifications consumer and applies
// each delivery result to the database.
func ConsumeNotifications(ctx context.Context, consumer jetstream.Consumer, db *sql.DB) {
	msgsChan, err := consumer.Messages()
	if err != nil {
		log.Printf("Failed to get messages from notifications consumer: %v", err)
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
		handleNotification(workCtx, db, msg)
	}
}

// handleNotification applies one delivery result and acks, naks or
// terminates msg: malformed bodies are terminated (they never parse on
// retry), database errors are retried with backoff (retryOnDBError), and
// a subject it has no branch for is logged and acked.
func handleNotification(ctx context.Context, db *sql.DB, msg jetstream.Msg) {
	subject := msg.Subject()

	switch subject {
	case streams.NotificationsSentSubject:
		var sent event.NotificationSent
		if !decode(msg, &sent, "NotificationSent") {
			return
		}
		if err := repository.UpdateNotificationSent(ctx, db, &sent); err != nil {
			if errors.Is(err, repository.ErrEventNotMaterialized) {
				retryUntilMaterialized(msg, sent.EventID)
				return
			}
			retryOnDBError(msg, "update notification sent", sent.EventID, err)
			return
		}

	case streams.NotificationsFailSubject:
		var failed event.NotificationFailed
		if !decode(msg, &failed, "NotificationFailed") {
			return
		}
		if err := repository.UpdateNotificationFailed(ctx, db, &failed); err != nil {
			if errors.Is(err, repository.ErrEventNotMaterialized) {
				retryUntilMaterialized(msg, failed.EventID)
				return
			}
			retryOnDBError(msg, "update notification failed", failed.EventID, err)
			return
		}

	case streams.NotificationsEmailSentSubject:
		var sent event.EmailSent
		if !decode(msg, &sent, "EmailSent") {
			return
		}
		if err := repository.UpdateInviteSent(ctx, db, sent.EventID); err != nil {
			retryOnDBError(msg, "update invite sent", sent.EventID, err)
			return
		}

	case streams.NotificationsEmailFailSubject:
		var failed event.EmailFailed
		if !decode(msg, &failed, "EmailFailed") {
			return
		}
		if err := repository.UpdateInviteFailed(ctx, db, failed.EventID, failed.Error); err != nil {
			retryOnDBError(msg, "update invite failed", failed.EventID, err)
			return
		}

	default:
		log.Printf("WARN unhandled subject %s", subject)
	}

	ack(msg)
}

// retryUntilMaterialized redelivers a Telegram result whose event the
// events consumer hasn't stored yet. The delay doubles from 15s (15, 30, 60,
// 120s: about four minutes over MaxDeliver), which covers the events
// consumer working through a backlog after a restart; the discovered
// message is normally a few seconds behind at most.
func retryUntilMaterialized(msg jetstream.Msg, eventID int64) {
	delivered := uint64(1)
	if meta, err := msg.Metadata(); err == nil && meta.NumDelivered > 0 {
		delivered = meta.NumDelivered
	}
	if delivered >= streams.DefaultMaxDeliver {
		log.Printf("WARN event %d still not materialized after %d deliveries; its Telegram result will be dropped", eventID, delivered)
	} else {
		log.Printf("Event %d not materialized yet; retrying its Telegram result (delivery %d)", eventID, delivered)
	}
	if err := msg.NakWithDelay(materializeRetryDelay(delivered)); err != nil {
		log.Printf("Error nak'ing Telegram result for event %d: %v", eventID, err)
	}
}

// materializeRetryDelay is 15s × 2^(delivered-1), capped at 5 minutes.
func materializeRetryDelay(delivered uint64) time.Duration {
	const maxDelay = 5 * time.Minute
	delay := 15 * time.Second
	for i := uint64(1); i < delivered && delay < maxDelay; i++ {
		delay *= 2
	}
	return min(delay, maxDelay)
}

// decode unmarshals msg into v, terminating msg and returning false if the
// body is malformed, since it will never become parseable on retry.
func decode(msg jetstream.Msg, v any, name string) bool {
	if err := json.Unmarshal(msg.Data(), v); err != nil {
		log.Printf("Failed to unmarshal %s message: %v", name, err)
		if err := msg.Term(); err != nil {
			log.Printf("Error terminating malformed %s message: %v", name, err)
		}
		return false
	}
	return true
}
