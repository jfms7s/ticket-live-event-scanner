// Package shutdown holds the small pieces the services share to stop
// cleanly on SIGTERM: wait for in-flight work with a bound, and restart a
// long-running loop until shutdown.
package shutdown

import (
	"context"
	"log"
	"time"
)

// WaitOrTimeout waits until done is closed or d has passed. It reports
// whether done was closed in time.
func WaitOrTimeout(done <-chan struct{}, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

// RunWithRestart runs fn until ctx is cancelled. When fn returns while ctx
// is still live, it logs and runs fn again after delay. It returns as soon
// as ctx is cancelled, including during the delay.
func RunWithRestart(ctx context.Context, name string, fn func(context.Context), delay time.Duration) {
	for {
		fn(ctx)
		if ctx.Err() != nil {
			return
		}
		log.Printf("%s exited, restarting in %v...", name, delay)
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
