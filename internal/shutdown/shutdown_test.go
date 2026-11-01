package shutdown

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitOrTimeoutDone(t *testing.T) {
	done := make(chan struct{})
	close(done)
	if !WaitOrTimeout(done, time.Second) {
		t.Fatal("WaitOrTimeout = false for a closed channel, want true")
	}
}

func TestWaitOrTimeoutTimesOut(t *testing.T) {
	start := time.Now()
	if WaitOrTimeout(make(chan struct{}), 20*time.Millisecond) {
		t.Fatal("WaitOrTimeout = true for a channel never closed, want false")
	}
	if elapsed := time.Since(start); elapsed < 20*time.Millisecond {
		t.Errorf("returned after %v, before the timeout", elapsed)
	}
}

func TestRunWithRestartRestartsUntilCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var runs atomic.Int32
	finished := make(chan struct{})
	go func() {
		RunWithRestart(ctx, "test", func(context.Context) {
			if runs.Add(1) == 3 {
				cancel()
			}
		}, time.Millisecond)
		close(finished)
	}()
	if !WaitOrTimeout(finished, time.Second) {
		t.Fatal("RunWithRestart did not return after cancel")
	}
	if n := runs.Load(); n != 3 {
		t.Errorf("fn ran %d times, want 3", n)
	}
}

// TestRunWithRestartReturnsMidDelay checks that a cancel during the
// restart delay ends the loop promptly instead of after the delay.
func TestRunWithRestartReturnsMidDelay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ran := make(chan struct{}, 1)
	finished := make(chan struct{})
	go func() {
		RunWithRestart(ctx, "test", func(context.Context) { ran <- struct{}{} }, time.Hour)
		close(finished)
	}()
	<-ran
	cancel()
	if !WaitOrTimeout(finished, time.Second) {
		t.Fatal("RunWithRestart still waiting out the restart delay after cancel")
	}
}

func TestRunWithRestartReturnsWhenFnSeesCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var runs atomic.Int32
	finished := make(chan struct{})
	go func() {
		RunWithRestart(ctx, "test", func(ctx context.Context) {
			runs.Add(1)
			cancel()
			<-ctx.Done()
		}, time.Hour)
		close(finished)
	}()
	if !WaitOrTimeout(finished, time.Second) {
		t.Fatal("RunWithRestart did not return")
	}
	if n := runs.Load(); n != 1 {
		t.Errorf("fn ran %d times, want 1", n)
	}
}
