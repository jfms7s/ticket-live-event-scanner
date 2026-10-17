package streams

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// natsJetStream connects to the NATS server in NATS_TEST_URL (a throwaway
// server with JetStream enabled), or skips the test when it isn't set.
func natsJetStream(t *testing.T) jetstream.JetStream {
	t.Helper()
	url := os.Getenv("NATS_TEST_URL")
	if url == "" {
		t.Skip("NATS_TEST_URL not set")
	}
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatalf("jetstream: %v", err)
	}
	ctx := context.Background()
	for _, name := range []string{EventsStreamName, NotificationsStreamName} {
		_ = js.DeleteStream(ctx, name)
	}
	_ = js.DeleteKeyValue(ctx, SeenBucketName)
	return js
}

// TestEnsureStreamsUpgradesTheDedupWindow starts from an EVENTS stream as
// an older image left it (no Duplicates, so the 2-minute default), checks
// EnsureStreams widens it in place, and that a repeated message ID is
// then rejected as a duplicate.
func TestEnsureStreamsUpgradesTheDedupWindow(t *testing.T) {
	js := natsJetStream(t)
	ctx := context.Background()

	old := EventsStreamConfig()
	old.Duplicates = 0
	if _, err := js.CreateStream(ctx, old); err != nil {
		t.Fatalf("create old stream: %v", err)
	}
	if err := EnsureStreams(ctx, js); err != nil {
		t.Fatalf("EnsureStreams: %v", err)
	}
	stream, err := js.Stream(ctx, EventsStreamName)
	if err != nil {
		t.Fatal(err)
	}
	if got := stream.CachedInfo().Config.Duplicates; got != 7*24*time.Hour {
		t.Fatalf("Duplicates after EnsureStreams = %v, want 168h", got)
	}

	for i, wantDup := range []bool{false, true} {
		ack, err := js.Publish(ctx, EventsSubject, []byte(`{}`), jetstream.WithMsgID("98164"))
		if err != nil {
			t.Fatalf("publish %d: %v", i+1, err)
		}
		if ack.Duplicate != wantDup {
			t.Errorf("publish %d Duplicate = %v, want %v", i+1, ack.Duplicate, wantDup)
		}
	}
}

func TestEnsureSeenBucket(t *testing.T) {
	js := natsJetStream(t)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if _, err := EnsureSeenBucket(ctx, js); err != nil {
			t.Fatalf("EnsureSeenBucket run %d: %v", i+1, err)
		}
	}
	if err := EnsureStreams(ctx, js); err != nil {
		t.Fatalf("EnsureStreams next to the bucket: %v", err)
	}
}
