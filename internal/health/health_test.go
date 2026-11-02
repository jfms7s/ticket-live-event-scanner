package health

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func readStamp(t *testing.T, path string) (int64, bool) {
	t.Helper()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, false
	}
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		t.Fatalf("parse %q: %v", b, err)
	}
	return n, true
}

func TestHeartbeatWritesWhenCheckPasses(t *testing.T) {
	path := filepath.Join(t.TempDir(), "healthy")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		Heartbeat(ctx, path, 10*time.Millisecond, func(context.Context) error { return nil })
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if stamp, ok := readStamp(t, path); ok {
			if d := time.Now().Unix() - stamp; d < 0 || d > 5 {
				t.Fatalf("stamp %d is not the current time", stamp)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("health file was never written")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Heartbeat did not return after cancel")
	}
}

func TestHeartbeatSkipsWriteWhenCheckFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "healthy")
	if err := os.WriteFile(path, []byte("12345\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		Heartbeat(ctx, path, 5*time.Millisecond, func(context.Context) error {
			calls.Add(1)
			return errors.New("nats down")
		})
		close(done)
	}()
	for calls.Load() < 3 {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	stamp, ok := readStamp(t, path)
	if !ok || stamp != 12345 {
		t.Fatalf("health file changed after failing checks: %d, %v", stamp, ok)
	}
}

func TestHeartbeatChecksImmediately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "healthy")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Heartbeat(ctx, path, time.Hour, func(context.Context) error { return nil })

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, ok := readStamp(t, path); ok {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("first check waited for the interval")
		}
		time.Sleep(5 * time.Millisecond)
	}
}
