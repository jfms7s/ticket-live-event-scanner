package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/telegram"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/nats-io/nats.go/jetstream"
)

var testNow = time.Date(2025, 11, 9, 19, 5, 0, 0, time.UTC)

func digestMsg(t *testing.T, week, text string, delivered uint64) *fakeMsg {
	t.Helper()
	data, err := json.Marshal(event.Digest{Week: week, Text: text, GeneratedAt: testNow.Add(-5 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	return &fakeMsg{data: data, meta: &jetstream.MsgMetadata{NumDelivered: delivered}}
}

type textSender struct {
	texts []string
	err   error
}

func (s *textSender) send(_ context.Context, text string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	s.texts = append(s.texts, text)
	return "1", nil
}

func newTestDigest(s *textSender) *DigestHandler {
	return &DigestHandler{send: s.send, sentWeeks: map[string]bool{}, now: func() time.Time { return testNow }}
}

func TestDigestIsSentAsIsAndAcked(t *testing.T) {
	s := &textSender{}
	h := newTestDigest(s)
	msg := digestMsg(t, "2025-W45", "<b>Week</b>", 1)
	if err := h.HandleMessage(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if len(s.texts) != 1 || s.texts[0] != "<b>Week</b>" || msg.acks != 1 {
		t.Errorf("texts %v, acks %d", s.texts, msg.acks)
	}

	// A redelivery of the same week (ack lost) is acked, not sent again.
	again := digestMsg(t, "2025-W45", "<b>Week</b>", 2)
	if err := h.HandleMessage(context.Background(), again); err != nil {
		t.Fatal(err)
	}
	if len(s.texts) != 1 || again.acks != 1 {
		t.Errorf("redelivery sent %d texts, acks %d; want 1 text total and an ack", len(s.texts), again.acks)
	}
}

func TestDigestSendFailureIsRetriedThenDropped(t *testing.T) {
	s := &textSender{err: errors.New("telegram API returned 500")}
	h := newTestDigest(s)

	msg := digestMsg(t, "2025-W45", "x", 1)
	_ = h.HandleMessage(context.Background(), msg)
	if len(msg.nakDelays) != 1 || msg.nakDelays[0] != time.Minute || msg.acks+msg.terms != 0 {
		t.Errorf("first failure: naks %v acks %d terms %d, want one delayed nak", msg.nakDelays, msg.acks, msg.terms)
	}

	last := digestMsg(t, "2025-W45", "x", 5)
	_ = h.HandleMessage(context.Background(), last)
	if last.terms != 1 || len(last.nakDelays) != 0 {
		t.Errorf("last attempt: terms %d naks %v, want term", last.terms, last.nakDelays)
	}
}

func TestDigestMalformedIsTermed(t *testing.T) {
	h := newTestDigest(&textSender{})
	for _, data := range []string{"not json", `{"week":"2025-W45"}`} {
		msg := &fakeMsg{data: []byte(data)}
		_ = h.HandleMessage(context.Background(), msg)
		if msg.terms != 1 {
			t.Errorf("%q: terms %d, want 1", data, msg.terms)
		}
	}
}

func TestDigestRetryDelays(t *testing.T) {
	if d := digestRetryDelay(errors.New("500"), 4); d != 30*time.Minute {
		t.Errorf("delay after attempt 4 = %v, want 30m", d)
	}
	if d := digestRetryDelay(&telegram.RateLimitedError{RetryAfter: 2 * time.Hour}, 1); d != 2*time.Hour {
		t.Errorf("rate-limited delay = %v, want Telegram's retry_after", d)
	}
}

// A timeout may have posted the digest: it is not retried.
func TestDigestAmbiguousFailureIsNotRetried(t *testing.T) {
	s := &textSender{err: fmt.Errorf("%w: context deadline exceeded", telegram.ErrDeliveryUnknown)}
	h := newTestDigest(s)
	msg := digestMsg(t, "2025-W45", "x", 1)
	_ = h.HandleMessage(context.Background(), msg)
	if msg.terms != 1 || len(msg.nakDelays) != 0 {
		t.Errorf("terms %d naks %v, want a term and no retry", msg.terms, msg.nakDelays)
	}
}

func TestDigestTooOldIsDropped(t *testing.T) {
	s := &textSender{}
	h := newTestDigest(s)
	data, _ := json.Marshal(event.Digest{Week: "2025-W45", Text: "x", GeneratedAt: testNow.Add(-13 * time.Hour)})
	msg := &fakeMsg{data: data, meta: &jetstream.MsgMetadata{NumDelivered: 1}}
	_ = h.HandleMessage(context.Background(), msg)
	if len(s.texts) != 0 || msg.terms != 1 {
		t.Errorf("sent %d, terms %d; want a stale digest dropped", len(s.texts), msg.terms)
	}
}
