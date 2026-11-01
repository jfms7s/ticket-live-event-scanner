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

// publishNotificationSent and publishNotificationFailed take natsSeq, the
// EVENTS stream sequence of the events.discovered message being handled, or
// 0 when it is unknown. It goes out as the result's DeliverySeq (0 is
// omitted, so web-ui-api falls back to the latest pending row) and is part
// of the publish message ID.
func (h *Handler) publishNotificationSent(ctx context.Context, eventID int64, messageID string, natsSeq uint64) error {
	sent := event.NotificationSent{
		EventID:           eventID,
		TelegramMessageID: messageID,
		SentAt:            now(),
		DeliverySeq:       natsSeq,
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
		// DeliverySeq: as on NotificationSent.
		DeliverySeq: natsSeq,
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
