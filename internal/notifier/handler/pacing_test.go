package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/policy"
	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/telegram"
)

func TestRetryDelay(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		delivered int
		want      time.Duration
	}{
		{"plain error uses backoff", errors.New("telegram API returned 500"), 2, policy.BackoffDelay(2)},
		{"rate limit longer than backoff", &telegram.RateLimitedError{RetryAfter: 40 * time.Second}, 1, 41 * time.Second},
		{"rate limit shorter than backoff", &telegram.RateLimitedError{RetryAfter: 2 * time.Second}, 4, policy.BackoffDelay(4)},
		{"wrapped rate limit", fmtWrap(&telegram.RateLimitedError{RetryAfter: 30 * time.Second}), 1, 31 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := retryDelay(tt.err, tt.delivered); got != tt.want {
				t.Errorf("retryDelay = %v, want %v", got, tt.want)
			}
		})
	}
}

func fmtWrap(err error) error { return errors.Join(errors.New("send"), err) }

func TestParseMinSendInterval(t *testing.T) {
	if d, err := ParseMinSendInterval(""); err != nil || d != DefaultMinSendInterval {
		t.Errorf(`ParseMinSendInterval("") = %v, %v; want %v`, d, err, DefaultMinSendInterval)
	}
	if d, err := ParseMinSendInterval("500ms"); err != nil || d != 500*time.Millisecond {
		t.Errorf(`ParseMinSendInterval("500ms") = %v, %v`, d, err)
	}
	if d, err := ParseMinSendInterval("0"); err != nil || d != 0 {
		t.Errorf(`ParseMinSendInterval("0") = %v, %v; want 0 (off)`, d, err)
	}
	for _, bad := range []string{"3", "fast", "-1s"} {
		if _, err := ParseMinSendInterval(bad); err == nil {
			t.Errorf("ParseMinSendInterval(%q) accepted, want an error", bad)
		}
	}
}

func TestPacerWaitsTheRemainderOfTheInterval(t *testing.T) {
	p := newPacer(50 * time.Millisecond)
	if err := p.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	p.sent()

	start := time.Now()
	if err := p.wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Errorf("second send waited %v, want about 50ms", elapsed)
	}
}

func TestPacerFirstSendDoesNotWait(t *testing.T) {
	p := newPacer(time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := p.wait(ctx); err != nil {
		t.Fatalf("first wait = %v, want nil", err)
	}
}

func TestPacerSkipsWaitAfterTheInterval(t *testing.T) {
	now := time.Unix(1000, 0)
	p := newPacer(3 * time.Second)
	p.now = func() time.Time { return now }
	p.sent()
	now = now.Add(4 * time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if err := p.wait(ctx); err != nil {
		t.Fatalf("wait after the interval = %v, want nil", err)
	}
}

func TestPacerWaitEndsOnShutdown(t *testing.T) {
	p := newPacer(time.Hour)
	p.sent()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait = %v, want context.Canceled", err)
	}
}

// TestHandleMessageRateLimitNaksWithRetryAfter runs the whole failure
// path: a 429 on the first delivery is nak'ed for at least retry_after.
func TestHandleMessageRateLimitNaksWithRetryAfter(t *testing.T) {
	pub := &fakePublisher{}
	h := newTestHandler(pub, &fakeSender{err: &telegram.RateLimitedError{RetryAfter: 37 * time.Second}})
	m := newMsg(t, 1, 10)

	if err := h.HandleMessage(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if len(m.nakDelays) != 1 || m.nakDelays[0] < 37*time.Second {
		t.Errorf("nak delays = %v, want one of at least retry_after (37s)", m.nakDelays)
	}
	if m.terms != 0 || len(pub.calls) != 0 {
		t.Errorf("terms = %d, publishes = %d; want 0 and 0 with deliveries left", m.terms, len(pub.calls))
	}
}

// TestHandleMessagePacingInterruptedByShutdown checks that a message
// waiting for its send slot is handed back, not sent, on shutdown.
func TestHandleMessagePacingInterruptedByShutdown(t *testing.T) {
	s := &fakeSender{messageID: "1"}
	h := newTestHandler(&fakePublisher{}, s)
	h.SetMinSendInterval(time.Hour)
	h.pace.sent()
	m := newMsg(t, 1, 10)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := h.HandleMessage(ctx, m); err != nil {
		t.Fatal(err)
	}
	if len(s.calls) != 0 || m.naks != 1 || m.acks != 0 {
		t.Errorf("sends = %d, naks = %d, acks = %d; want 0, 1, 0", len(s.calls), m.naks, m.acks)
	}
}
