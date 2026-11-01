package emailhandler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/nats-io/nats.go/jetstream"
)

func (h *Handler) publishEmailSent(ctx context.Context, eventID int64, messageID string, natsSeq uint64) error {
	sent := event.EmailSent{
		EventID:   eventID,
		MessageID: messageID,
		SentAt:    now(),
	}

	data, err := json.Marshal(sent)
	if err != nil {
		return fmt.Errorf("marshal notifications.email.sent: %w", err)
	}

	msgID := fmt.Sprintf("email-sent-%d-%d", eventID, natsSeq)
	pubCtx, cancel := resultCtx(ctx)
	defer cancel()
	_, err = h.js.Publish(pubCtx, streams.NotificationsEmailSentSubject, data, jetstream.WithMsgID(msgID))
	if err != nil {
		return fmt.Errorf("publish notifications.email.sent: %w", err)
	}

	return nil
}

func (h *Handler) publishEmailFailed(ctx context.Context, eventID int64, errMsg string, attempts int, natsSeq uint64) error {
	failed := event.EmailFailed{
		EventID:  eventID,
		Error:    errMsg,
		FailedAt: now(),
		Attempts: attempts,
	}

	data, err := json.Marshal(failed)
	if err != nil {
		return fmt.Errorf("marshal notifications.email.failed: %w", err)
	}

	msgID := fmt.Sprintf("email-failed-%d-%d", eventID, natsSeq)
	pubCtx, cancel := resultCtx(ctx)
	defer cancel()
	_, err = h.js.Publish(pubCtx, streams.NotificationsEmailFailSubject, data, jetstream.WithMsgID(msgID))
	if err != nil {
		return fmt.Errorf("publish notifications.email.failed: %w", err)
	}

	return nil
}

// now returns the current UTC time (extracted to allow mocking in tests).
func now() time.Time {
	return time.Now().UTC()
}

// resultCtx is the context for publishing a delivery result. The result
// must go out even when shutdown has just cancelled the send (the send may
// already have happened), so it ignores ctx's cancellation and has its own
// short bound instead.
func resultCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), resultPublishTimeout)
}

// resultPublishTimeout bounds one delivery-result publish.
const resultPublishTimeout = 2 * time.Second
