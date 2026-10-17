package discovery

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// TestSeenStoreAgainstNATS runs the seen store on a real KV bucket (a
// throwaway server in NATS_TEST_URL; skipped when unset): an empty bucket
// loads as empty without hanging, and a marked ID survives into a new
// store, as it must across scraper runs.
func TestSeenStoreAgainstNATS(t *testing.T) {
	url := os.Getenv("NATS_TEST_URL")
	if url == "" {
		t.Skip("NATS_TEST_URL not set")
	}
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = js.DeleteKeyValue(ctx, streams.SeenBucketName)
	kv, err := streams.EnsureSeenBucket(ctx, js)
	if err != nil {
		t.Fatal(err)
	}
	db := emptyDB(t)

	ids, err := newSeenStore(kv, db).Load(ctx)
	if err != nil || len(ids) != 0 {
		t.Fatalf("Load on an empty bucket = %v, %v; want empty, nil", ids, err)
	}

	if err := newSeenStore(kv, db).Mark(ctx, 98164); err != nil {
		t.Fatal(err)
	}
	ids, err = newSeenStore(kv, db).Load(ctx)
	if err != nil || len(ids) != 1 || !ids[98164] {
		t.Fatalf("Load after Mark = %v, %v; want {98164}, nil", ids, err)
	}
}
