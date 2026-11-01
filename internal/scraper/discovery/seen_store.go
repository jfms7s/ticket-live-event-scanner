package discovery

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// LoadKnownIDs populates the scraper's in-memory set of seen event IDs, so
// discovery can skip events it has already published.
func (s *Scraper) LoadKnownIDs(ctx context.Context) error {
	ids, err := s.seen.Load(ctx)
	if err != nil {
		return err
	}
	s.knownIDs = ids
	log.Printf("Loaded %d known event IDs", len(s.knownIDs))

	// The skipped set only saves requests, so a bucket that can't be read
	// means re-fetching those sessions, not failing the run.
	skipped, err := s.skipped.Load(ctx)
	if err != nil {
		log.Printf("WARN %v; rejected sessions will be fetched again", err)
		skipped = map[int64]bool{}
	}
	s.skipIDs = skipped
	if len(skipped) > 0 {
		log.Printf("Loaded %d recently rejected event IDs", len(skipped))
	}
	return nil
}

// seenKV is the part of jetstream.KeyValue the seen store uses.
type seenKV interface {
	Put(ctx context.Context, key string, value []byte) (uint64, error)
	ListKeys(ctx context.Context, opts ...jetstream.WatchOpt) (jetstream.KeyLister, error)
}

// bucketSeenStore keeps "seen" in the scraper's own JetStream KV bucket
// (streams.SeenBucketName), keyed by event ID and written right after each
// successful publish, so it doesn't depend on web-ui-api having consumed
// anything.
//
// Load also reads the IDs in web-ui-api's events table (hidden rows
// included). That covers events published before the bucket existed, or a
// bucket that was lost; the bucket alone covers everything after.
type bucketSeenStore struct {
	kv seenKV
	db *sql.DB
}

func newSeenStore(kv seenKV, db *sql.DB) seenStore {
	return bucketSeenStore{kv: kv, db: db}
}

func (b bucketSeenStore) Load(ctx context.Context) (map[int64]bool, error) {
	ids, err := b.loadBucket(ctx)
	if err != nil {
		return nil, err
	}
	fromBucket := len(ids)
	if err := b.loadEventsTable(ctx, ids); err != nil {
		return nil, err
	}
	log.Printf("Seen event IDs: %d in the seen bucket, %d in total with the events table", fromBucket, len(ids))
	return ids, nil
}

func (b bucketSeenStore) loadBucket(ctx context.Context) (map[int64]bool, error) {
	ids, err := listIDs(ctx, b.kv)
	if err != nil {
		return nil, fmt.Errorf("seen bucket: %w", err)
	}
	return ids, nil
}

// listIDs reads every key of a bucket keyed by event ID.
func listIDs(ctx context.Context, kv seenKV) (map[int64]bool, error) {
	lister, err := kv.ListKeys(ctx)
	if err != nil {
		return nil, fmt.Errorf("list keys: %w", err)
	}
	defer lister.Stop()

	ids := make(map[int64]bool)
	for key := range lister.Keys() {
		id, err := strconv.ParseInt(key, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("key %q is not an event ID", key)
		}
		ids[id] = true
	}
	// ListKeys closes its channel without an error when ctx ends, so a
	// short list here may be a cut-off one. Treating it as complete would
	// publish the missing events again.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("list keys: %w", err)
	}
	return ids, nil
}

func (b bucketSeenStore) loadEventsTable(ctx context.Context, ids map[int64]bool) error {
	rows, err := b.db.QueryContext(ctx, "SELECT id FROM events")
	if err != nil {
		// web-ui-api creates the schema; before it has started once the
		// table doesn't exist, and there is nothing in it to know.
		if strings.Contains(err.Error(), "no such table") {
			log.Printf("events table not created yet; using the seen bucket only")
			return nil
		}
		return fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan event id: %w", err)
		}
		ids[id] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate events: %w", err)
	}
	return nil
}

// Mark stores id in the seen bucket, with the time as the value for
// whoever inspects the bucket by hand.
func (b bucketSeenStore) Mark(ctx context.Context, id int64) error {
	if _, err := b.kv.Put(ctx, strconv.FormatInt(id, 10), []byte(time.Now().UTC().Format(time.RFC3339))); err != nil {
		return fmt.Errorf("mark event %d seen: %w", id, err)
	}
	return nil
}
