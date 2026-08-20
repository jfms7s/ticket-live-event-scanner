package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/nats-io/nats.go/jetstream"
)

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

// now returns the current UTC time (extracted to allow mocking in tests)
func now() time.Time {
	return time.Now().UTC()
}
