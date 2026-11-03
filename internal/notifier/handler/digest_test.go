package handler

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/nats-io/nats.go/jetstream"
)

func digestMsg(t *testing.T, week, text string, delivered uint64) *fakeMsg {
	t.Helper()
	data, err := json.Marshal(event.Digest{Week: week, Text: text})
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
	return &DigestHandler{send: s.send, sentWeeks: map[string]bool{}}
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
	if len(msg.nakDelays) != 1 || msg.acks+msg.terms != 0 {
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
