package digest

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// TestSendMessageIDOnARealStream publishes twice in one week on the NATS
// server in NATS_TEST_URL: the stream keeps one message, ID digest-<week>.
func TestSendMessageIDOnARealStream(t *testing.T) {
	url := os.Getenv("NATS_TEST_URL")
	if url == "" {
		t.Skip("NATS_TEST_URL not set")
	}
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_ = js.DeleteStream(ctx, streams.EventsStreamName)
	if err := streams.EnsureStreams(ctx, js); err != nil {
		t.Fatal(err)
	}

	db := testDB(t)
	for i := 0; i < 2; i++ {
		if err := Send(ctx, db, fakeLog{}, js, sunday.Add(time.Duration(i)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	stream, err := js.Stream(ctx, streams.EventsStreamName)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := stream.GetLastMsgForSubject(ctx, streams.DigestSubject)
	if err != nil {
		t.Fatal(err)
	}
	if id := msg.Header.Get("Nats-Msg-Id"); id != "digest-2025-W45" {
		t.Errorf("message ID = %q, want digest-2025-W45", id)
	}
	info, err := stream.Info(ctx, jetstream.WithSubjectFilter(streams.DigestSubject))
	if err != nil {
		t.Fatal(err)
	}
	if n := info.State.Subjects[streams.DigestSubject]; n != 1 {
		t.Errorf("stream holds %d digests, want 1", n)
	}
}
