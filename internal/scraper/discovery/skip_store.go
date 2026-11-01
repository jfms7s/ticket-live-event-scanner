package discovery

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
)

// errSkip marks a per-session data problem (an unparseable date, a missing
// title, URL or slug). Such a session is skipped with a warning and does
// not fail the run: one bad listing must not turn an hourly scrape into
// three (the CronJob retries a failed run). Transport and structural
// problems still fail the run.
var errSkip = errors.New("skipped")

// skipStore remembers sessions the parser rejected, so later runs don't
// fetch them every hour. Entries expire (see streams.SkippedBucketConfig),
// after which the session is fetched and parsed again.
type skipStore interface {
	// Load returns the IDs of sessions rejected recently.
	Load(ctx context.Context) (map[int64]bool, error)
	// Mark records that id was rejected, and why.
	Mark(ctx context.Context, id int64, reason string) error
}

// bucketSkipStore keeps the skipped set in the scraper's skipped KV bucket
// (streams.SkippedBucketName): key event ID, value the reason.
type bucketSkipStore struct{ kv seenKV }

func newSkipStore(kv seenKV) skipStore { return bucketSkipStore{kv: kv} }

func (b bucketSkipStore) Load(ctx context.Context) (map[int64]bool, error) {
	ids, err := listIDs(ctx, b.kv)
	if err != nil {
		return nil, fmt.Errorf("skipped bucket: %w", err)
	}
	return ids, nil
}

func (b bucketSkipStore) Mark(ctx context.Context, id int64, reason string) error {
	if _, err := b.kv.Put(ctx, strconv.FormatInt(id, 10), []byte(reason)); err != nil {
		return fmt.Errorf("mark event %d skipped: %w", id, err)
	}
	return nil
}

// noSkipStore remembers nothing: every rejected session is fetched again
// on the next run.
type noSkipStore struct{}

func (noSkipStore) Load(context.Context) (map[int64]bool, error) { return map[int64]bool{}, nil }
func (noSkipStore) Mark(context.Context, int64, string) error    { return nil }

// skip records a rejected session: counted, logged and remembered. A
// failure to remember only costs a re-fetch next run, so it is logged and
// not returned.
func (s *Scraper) skip(ctx context.Context, id int64, err error) {
	s.Stats.Skipped++
	s.skipIDs[id] = true
	log.Printf("WARN skipping event %d: %v", id, err)
	if markErr := s.skipped.Mark(ctx, id, err.Error()); markErr != nil {
		log.Printf("WARN %v; it will be fetched again next run", markErr)
	}
}
