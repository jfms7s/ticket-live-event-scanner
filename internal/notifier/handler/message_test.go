package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// fakeMsg is a jetstream.Msg that records the ack-family calls the handler
// makes and returns configurable data, headers and metadata.
type fakeMsg struct {
	data    []byte
	headers nats.Header
	meta    *jetstream.MsgMetadata
	metaErr error
	ackErr  error

	acks       int
	naks       int
	nakDelays  []time.Duration
	terms      int
	termReason []string
}

func (m *fakeMsg) Metadata() (*jetstream.MsgMetadata, error) {
	if m.metaErr != nil {
		return nil, m.metaErr
	}
	return m.meta, nil
}
func (m *fakeMsg) Data() []byte                    { return m.data }
func (m *fakeMsg) Headers() nats.Header            { return m.headers }
func (m *fakeMsg) Subject() string                 { return "events.discovered" }
func (m *fakeMsg) Reply() string                   { return "" }
func (m *fakeMsg) Ack() error                      { m.acks++; return m.ackErr }
func (m *fakeMsg) DoubleAck(context.Context) error { m.acks++; return m.ackErr }
func (m *fakeMsg) Nak() error                      { m.naks++; return nil }
func (m *fakeMsg) NakWithDelay(d time.Duration) error {
	m.nakDelays = append(m.nakDelays, d)
	return nil
}
func (m *fakeMsg) InProgress() error { return nil }
func (m *fakeMsg) Term() error       { m.terms++; return nil }
func (m *fakeMsg) TermWithReason(r string) error {
	m.terms++
	m.termReason = append(m.termReason, r)
	return nil
}

// pubCall is one recorded Publish. jetstream's publish options are opaque
// (they mutate an unexported struct), so the message ID can't be read back;
// the call records the option count and the decoded event_id instead.
type pubCall struct {
	subject     string
	eventID     int64
	deliverySeq uint64
	nOpts       int
}

// fakePublisher records publishes and fails those whose subject is in fail.
type fakePublisher struct {
	calls []pubCall
	fail  map[string]bool
}

func (p *fakePublisher) Publish(_ context.Context, subject string, data []byte, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	var body struct {
		EventID     int64  `json:"event_id"`
		DeliverySeq uint64 `json:"delivery_seq"`
	}
	_ = json.Unmarshal(data, &body)
	p.calls = append(p.calls, pubCall{subject: subject, eventID: body.EventID, deliverySeq: body.DeliverySeq, nOpts: len(opts)})
	if p.fail[subject] {
		return nil, fmt.Errorf("publish to %s failed", subject)
	}
	return &jetstream.PubAck{Stream: "NOTIFICATIONS"}, nil
}

func (p *fakePublisher) subjects() []string {
	out := make([]string, len(p.calls))
	for i, c := range p.calls {
		out[i] = c.subject
	}
	return out
}

// fakeSender stands in for telegram.SendTelegramMessage.
type fakeSender struct {
	messageID string
	err       error
	calls     []event.Discovered
}

func (s *fakeSender) send(_ context.Context, d event.Discovered) (string, error) {
	s.calls = append(s.calls, d)
	if s.err != nil {
		return "", s.err
	}
	return s.messageID, nil
}

const testEventID int64 = 4242

func newTestHandler(pub *fakePublisher, s *fakeSender) *Handler {
	return &Handler{
		js:           pub,
		send:         s.send,
		recentlySent: make(map[int64]*recentSend),
	}
}

func discoveredJSON(t *testing.T, id int64) []byte {
	t.Helper()
	b, err := json.Marshal(event.Discovered{EventID: id, Slug: "show", Title: "Show", URL: "https://example.test/show"})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newMsg(t *testing.T, delivered, seq uint64) *fakeMsg {
	t.Helper()
	return &fakeMsg{
		data: discoveredJSON(t, testEventID),
		meta: &jetstream.MsgMetadata{
			NumDelivered: delivered,
			Sequence:     jetstream.SequencePair{Stream: seq, Consumer: delivered},
		},
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalDurations(a, b []time.Duration) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// outcome is what one HandleMessage call did to the message, the publisher
// and the sender.
type outcome struct {
	acks      int
	nakDelays []time.Duration
	terms     int
	publishes []string
	sends     int
	// deliverySeq is the delivery_seq every published result must carry.
	deliverySeq uint64
}

func checkOutcome(t *testing.T, m *fakeMsg, pub *fakePublisher, s *fakeSender, want outcome) {
	t.Helper()
	if m.acks != want.acks {
		t.Errorf("acks = %d, want %d", m.acks, want.acks)
	}
	if m.naks != 0 {
		t.Errorf("immediate Nak called %d times, want 0", m.naks)
	}
	if !equalDurations(m.nakDelays, want.nakDelays) {
		t.Errorf("NakWithDelay calls = %v, want %v", m.nakDelays, want.nakDelays)
	}
	if m.terms != want.terms {
		t.Errorf("terms = %d, want %d", m.terms, want.terms)
	}
	if got := pub.subjects(); !equalStrings(got, want.publishes) {
		t.Errorf("published subjects = %v, want %v", got, want.publishes)
	}
	for _, c := range pub.calls {
		if c.eventID != testEventID {
			t.Errorf("published event_id = %d on %s, want %d", c.eventID, c.subject, testEventID)
		}
		if c.deliverySeq != want.deliverySeq {
			t.Errorf("published delivery_seq = %d on %s, want %d", c.deliverySeq, c.subject, want.deliverySeq)
		}
		if c.nOpts != 1 {
			t.Errorf("publish on %s passed %d options, want 1 (WithMsgID)", c.subject, c.nOpts)
		}
	}
	if len(s.calls) != want.sends {
		t.Errorf("Telegram sends = %d, want %d", len(s.calls), want.sends)
	}
}

func TestHandleMessage(t *testing.T) {
	sendErr := errors.New("telegram API returned 500")
	metaErr := errors.New("metadata unavailable")

	type tc struct {
		name   string
		msg    func(t *testing.T) *fakeMsg
		sendOK bool
		fail   []string
		want   outcome
	}
	cases := []tc{
		{
			name:   "success acks and publishes sent",
			msg:    func(t *testing.T) *fakeMsg { return newMsg(t, 1, 10) },
			sendOK: true,
			want:   outcome{acks: 1, publishes: []string{streams.NotificationsSentSubject}, sends: 1, deliverySeq: 10},
		},
		{
			name: "send fails on final delivery publishes failed and terms",
			msg:  func(t *testing.T) *fakeMsg { return newMsg(t, uint64(streams.EventsConsumerMaxDeliver), 10) },
			want: outcome{terms: 1, publishes: []string{streams.NotificationsFailSubject}, sends: 1, deliverySeq: 10},
		},
		{
			name: "malformed JSON acks without sending",
			msg: func(t *testing.T) *fakeMsg {
				m := newMsg(t, 1, 10)
				m.data = []byte("{not json")
				return m
			},
			want: outcome{acks: 1},
		},
		{
			name: "metadata error with Nats-Num-Delivered 2 naks with 10s",
			msg: func(t *testing.T) *fakeMsg {
				m := newMsg(t, 0, 0)
				m.metaErr = metaErr
				m.headers = nats.Header{"Nats-Num-Delivered": []string{"2"}}
				return m
			},
			want: outcome{nakDelays: []time.Duration{10 * time.Second}},
		},
		{
			name: "metadata error at max delivery publishes failed and terms",
			msg: func(t *testing.T) *fakeMsg {
				m := newMsg(t, 0, 0)
				m.metaErr = metaErr
				m.headers = nats.Header{"Nats-Num-Delivered": []string{strconv.Itoa(streams.EventsConsumerMaxDeliver)}}
				return m
			},
			want: outcome{terms: 1, publishes: []string{streams.NotificationsFailSubject}, deliverySeq: 0}, // stream sequence unknown
		},
		{
			name: "metadata error without header assumes first delivery",
			msg: func(t *testing.T) *fakeMsg {
				m := newMsg(t, 0, 0)
				m.metaErr = metaErr
				return m
			},
			want: outcome{nakDelays: []time.Duration{5 * time.Second}},
		},
		{
			name: "metadata error with unparsable header assumes first delivery",
			msg: func(t *testing.T) *fakeMsg {
				m := newMsg(t, 0, 0)
				m.metaErr = metaErr
				m.headers = nats.Header{"Nats-Num-Delivered": []string{"many"}}
				return m
			},
			want: outcome{nakDelays: []time.Duration{5 * time.Second}},
		},
	}
	for n := 1; n < streams.EventsConsumerMaxDeliver; n++ {
		delivered := uint64(n)
		cases = append(cases, tc{
			name: fmt.Sprintf("send fails at delivery %d naks with backoff", n),
			msg:  func(t *testing.T) *fakeMsg { return newMsg(t, delivered, 10) },
			want: outcome{nakDelays: []time.Duration{time.Duration(n) * 5 * time.Second}, sends: 1},
		})
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pub := &fakePublisher{fail: map[string]bool{}}
			for _, s := range c.fail {
				pub.fail[s] = true
			}
			s := &fakeSender{messageID: "777"}
			if !c.sendOK {
				s.err = sendErr
			}
			h := newTestHandler(pub, s)
			m := c.msg(t)

			if err := h.HandleMessage(context.Background(), m); err != nil {
				t.Fatalf("HandleMessage returned %v, want nil", err)
			}
			checkOutcome(t, m, pub, s, c.want)
		})
	}
}

// TestHandleMessage_RedeliveryAfterSentPublishFails covers the dedup cache:
// Telegram succeeded but notifications.sent failed, so the message is nak'd;
// its redelivery (same stream sequence) must republish sent without sending
// to Telegram again.
func TestHandleMessage_RedeliveryAfterSentPublishFails(t *testing.T) {
	pub := &fakePublisher{fail: map[string]bool{streams.NotificationsSentSubject: true}}
	s := &fakeSender{messageID: "777"}
	h := newTestHandler(pub, s)

	first := newMsg(t, 2, 10)
	if err := h.HandleMessage(context.Background(), first); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	checkOutcome(t, first, pub, s, outcome{deliverySeq: 10,
		nakDelays: []time.Duration{10 * time.Second},
		publishes: []string{streams.NotificationsSentSubject},
		sends:     1,
	})

	pub.fail = map[string]bool{}
	pub.calls = nil
	redelivery := newMsg(t, 3, 10)
	if err := h.HandleMessage(context.Background(), redelivery); err != nil {
		t.Fatalf("redelivery: %v", err)
	}
	checkOutcome(t, redelivery, pub, s, outcome{deliverySeq: 10,
		acks:      1,
		publishes: []string{streams.NotificationsSentSubject},
		sends:     1, // still the one send from the first delivery
	})
}

// TestHandleMessage_DifferentStreamSequenceSendsAgain checks that the cache
// is keyed on the stream sequence too: the same event arriving as a new
// stream message (a manual retry) is sent to Telegram again.
func TestHandleMessage_DifferentStreamSequenceSendsAgain(t *testing.T) {
	pub := &fakePublisher{}
	s := &fakeSender{messageID: "777"}
	h := newTestHandler(pub, s)

	if err := h.HandleMessage(context.Background(), newMsg(t, 1, 10)); err != nil {
		t.Fatal(err)
	}
	again := newMsg(t, 1, 11)
	if err := h.HandleMessage(context.Background(), again); err != nil {
		t.Fatal(err)
	}
	if len(s.calls) != 2 {
		t.Fatalf("Telegram sends = %d, want 2", len(s.calls))
	}
	if again.acks != 1 {
		t.Errorf("acks on second message = %d, want 1", again.acks)
	}
	if got := pub.subjects(); !equalStrings(got, []string{streams.NotificationsSentSubject, streams.NotificationsSentSubject}) {
		t.Errorf("published subjects = %v", got)
	}
}

// TestHandleMessage_StaleCacheEntrySendsAgain checks that a cache entry
// older than a minute is ignored even when the stream sequence matches.
func TestHandleMessage_StaleCacheEntrySendsAgain(t *testing.T) {
	pub := &fakePublisher{}
	s := &fakeSender{messageID: "777"}
	h := newTestHandler(pub, s)
	h.recentlySent[testEventID] = &recentSend{natsSeq: 10, messageID: "old", sentAt: time.Now().Add(-2 * time.Minute)}

	m := newMsg(t, 2, 10)
	if err := h.HandleMessage(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	checkOutcome(t, m, pub, s, outcome{deliverySeq: 10, acks: 1, publishes: []string{streams.NotificationsSentSubject}, sends: 1})
}

// TestHandleMessage_CachedRepublishFails covers a redelivery that hits the
// cache but whose sent republish fails again: nak before the last delivery,
// failed + Term on it. Telegram is never called.
func TestHandleMessage_CachedRepublishFails(t *testing.T) {
	for _, c := range []struct {
		delivered uint64
		want      outcome
	}{
		{3, outcome{deliverySeq: 10, nakDelays: []time.Duration{15 * time.Second}, publishes: []string{streams.NotificationsSentSubject}}},
		{uint64(streams.EventsConsumerMaxDeliver), outcome{deliverySeq: 10, terms: 1, publishes: []string{streams.NotificationsSentSubject, streams.NotificationsFailSubject}}},
	} {
		t.Run(fmt.Sprintf("delivery_%d", c.delivered), func(t *testing.T) {
			pub := &fakePublisher{fail: map[string]bool{streams.NotificationsSentSubject: true}}
			s := &fakeSender{messageID: "777"}
			h := newTestHandler(pub, s)
			h.recentlySent[testEventID] = &recentSend{natsSeq: 10, messageID: "777", sentAt: time.Now()}

			m := newMsg(t, c.delivered, 10)
			if err := h.HandleMessage(context.Background(), m); err != nil {
				t.Fatal(err)
			}
			checkOutcome(t, m, pub, s, c.want)
		})
	}
}

// TestHandleMessage_AckErrorIsReturned checks that a failed Ack after a
// successful send is surfaced to the caller.
func TestHandleMessage_AckErrorIsReturned(t *testing.T) {
	pub := &fakePublisher{}
	s := &fakeSender{messageID: "777"}
	h := newTestHandler(pub, s)
	m := newMsg(t, 1, 10)
	m.ackErr = errors.New("ack failed")

	if err := h.HandleMessage(context.Background(), m); err == nil {
		t.Fatal("HandleMessage returned nil, want the ack error")
	}
}

// TestHandleMessage_SentPublishFailsOnFinalAttempt_PublishesFailed pins
// today's behaviour, it does not endorse it: Telegram delivered the message,
// but notifications.sent failed on the last delivery, so the handler
// publishes notifications.failed and terms. The event is reported as failed
// although the channel received it. See the obsidian-vault note
// personal/projects/ticket-live-event-scanner/issues/review-telegram-handler-untested
// (and review-notification-rows-stuck-pending for the row-matching fix).
func TestHandleMessage_SentPublishFailsOnFinalAttempt_PublishesFailed(t *testing.T) {
	pub := &fakePublisher{fail: map[string]bool{streams.NotificationsSentSubject: true}}
	s := &fakeSender{messageID: "777"}
	h := newTestHandler(pub, s)

	m := newMsg(t, uint64(streams.EventsConsumerMaxDeliver), 10)
	if err := h.HandleMessage(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	checkOutcome(t, m, pub, s, outcome{deliverySeq: 10,
		terms:     1,
		publishes: []string{streams.NotificationsSentSubject, streams.NotificationsFailSubject},
		sends:     1,
	})
}

// TestNewWiresDefaults checks that New installs a sender and the given
// JetStream as the publisher.
func TestNewWiresDefaults(t *testing.T) {
	h := New("token", "chat", nil)
	if h.send == nil {
		t.Error("New left send nil")
	}
	if h.recentlySent == nil {
		t.Error("New left recentlySent nil")
	}
}
