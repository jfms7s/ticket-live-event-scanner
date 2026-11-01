package handler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/policy"
	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/telegram"
)

// DefaultMinSendInterval keeps sends under Telegram's limit of about 20
// messages a minute to one group.
const DefaultMinSendInterval = 3 * time.Second

// rateLimitJitter is added to Telegram's retry_after so the retry lands
// after the window, not on its edge.
const rateLimitJitter = time.Second

// ParseMinSendInterval reads TELEGRAM_MIN_SEND_INTERVAL: a Go duration,
// DefaultMinSendInterval when empty, 0 to turn pacing off.
func ParseMinSendInterval(v string) (time.Duration, error) {
	if v == "" {
		return DefaultMinSendInterval, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("TELEGRAM_MIN_SEND_INTERVAL: %w", err)
	}
	if d < 0 {
		return 0, fmt.Errorf("TELEGRAM_MIN_SEND_INTERVAL: %v is negative", d)
	}
	return d, nil
}

// retryDelay is how long to wait before redelivering a message whose send
// failed with err on delivery numDelivered: at least Telegram's
// retry_after (plus jitter) for a rate limit, the usual linear backoff
// otherwise.
func retryDelay(err error, numDelivered int) time.Duration {
	backoff := policy.BackoffDelay(numDelivered)
	var rl *telegram.RateLimitedError
	if errors.As(err, &rl) {
		return max(rl.RetryAfter+rateLimitJitter, backoff)
	}
	return backoff
}

// pacer enforces a minimum interval between sends.
type pacer struct {
	interval time.Duration
	now      func() time.Time

	mu   sync.Mutex
	last time.Time
}

func newPacer(interval time.Duration) *pacer {
	return &pacer{interval: interval, now: time.Now}
}

// wait blocks until interval has passed since the last send, or ctx ends.
func (p *pacer) wait(ctx context.Context) error {
	if p == nil || p.interval <= 0 {
		return nil
	}
	p.mu.Lock()
	last := p.last
	p.mu.Unlock()
	if last.IsZero() {
		return nil
	}
	remaining := p.interval - p.now().Sub(last)
	if remaining <= 0 {
		return nil
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// sent records a send attempt (successful or not: both count against
// Telegram's limit).
func (p *pacer) sent() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.last = p.now()
	p.mu.Unlock()
}
