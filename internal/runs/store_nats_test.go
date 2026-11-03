package runs

import (
	"context"
	"os"
	"testing"

	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// TestReaderOnARealBucket writes and reads the runs bucket on the NATS
// server in NATS_TEST_URL (a throwaway server with JetStream enabled).
func TestReaderOnARealBucket(t *testing.T) {
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
	_ = js.DeleteKeyValue(ctx, streams.RunsBucketName)

	kv, err := streams.EnsureRunsBucket(ctx, js)
	if err != nil {
		t.Fatal(err)
	}
	r := NewReader(kv)

	// An empty bucket is no runs, not an error.
	sum, err := r.Summary(ctx, 10)
	if err != nil || len(sum.Latest) != 0 {
		t.Fatalf("Summary of an empty bucket = %+v, %v", sum, err)
	}

	if err := Write(ctx, kv, []Record{rec(1, 0, 0, ""), rec(2, 0, 3, ""), rec(1, 1, 0, ""), rec(2, 1, 0, "boom"), rec(1, 2, 0, "")}); err != nil {
		t.Fatal(err)
	}
	// A second bucket handle, as web-ui-api has next to the scraper.
	if _, err := streams.EnsureRunsBucket(ctx, js); err != nil {
		t.Fatalf("EnsureRunsBucket again: %v", err)
	}

	sum, err = r.Summary(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.Latest) != 1 || sum.Latest[0].HubID != 1 || !sum.Latest[0].Empty() {
		t.Errorf("Latest = %+v, want hub 1 (the only hub of the newest run), empty after 3 runs", sum.Latest)
	}
	if len(sum.Runs) != 5 {
		t.Errorf("Runs = %d records, want 5", len(sum.Runs))
	}
}
