package runs

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// Putter is the part of jetstream.KeyValue Write uses.
type Putter interface {
	Put(ctx context.Context, key string, value []byte) (uint64, error)
}

// Write stores every record, carrying on past a failed one so the others
// are still recorded, and returns the failures joined.
func Write(ctx context.Context, kv Putter, records []Record) error {
	var errs []error
	for _, r := range records {
		data, err := json.Marshal(r)
		if err != nil {
			errs = append(errs, fmt.Errorf("marshal run record %s: %w", Key(r), err))
			continue
		}
		if _, err := kv.Put(ctx, Key(r), data); err != nil {
			errs = append(errs, fmt.Errorf("put run record %s: %w", Key(r), err))
		}
	}
	return errors.Join(errs...)
}

// KV is the part of jetstream.KeyValue the Reader uses.
type KV interface {
	ListKeys(ctx context.Context, opts ...jetstream.WatchOpt) (jetstream.KeyLister, error)
	Get(ctx context.Context, key string) (jetstream.KeyValueEntry, error)
}

// EmptyRunsThreshold is how many successful runs in a row a hub must list
// no future session before it counts as empty (it has likely run dry).
const EmptyRunsThreshold = 3

// maxWalk bounds how many older records of one hub a read fetches while
// looking for its last success and its last non-empty run.
const maxWalk = 48

// HubStatus is a hub of the newest run: its newest record plus what the
// older ones say.
type HubStatus struct {
	Record
	// LastSuccessAt is when the hub last finished a run without an error,
	// among the records the bucket still holds (about a week); nil when
	// there is none.
	LastSuccessAt *time.Time `json:"last_success_at"`
	// UpcomingSessions is the future session count of the hub's newest
	// successful run (0 when there is none).
	UpcomingSessions int `json:"upcoming_sessions"`
	// ConsecutiveEmptyRuns counts the hub's successful runs in a row,
	// newest first, that listed no future session. Failed runs in between
	// are skipped, not counted. It is a lower bound once it reaches the
	// walk limit.
	ConsecutiveEmptyRuns int `json:"consecutive_empty_runs"`
}

// Empty reports whether the hub has listed no future session for
// EmptyRunsThreshold successful runs in a row.
func (h HubStatus) Empty() bool { return h.ConsecutiveEmptyRuns >= EmptyRunsThreshold }

// Summary is what GET /api/runs serves.
type Summary struct {
	// Latest has one entry per hub of the newest run, ordered by hub slug.
	Latest []HubStatus `json:"latest"`
	// Runs has the newest records, newest first.
	Runs []Record `json:"runs"`
}

// Reader reads the runs bucket. Records never change once written (the key
// holds the run's start time), so it caches decoded records by key and
// fetches only keys it has not seen. It is safe for concurrent use.
type Reader struct {
	kv KV

	mu    sync.Mutex
	cache map[string]Record
}

// NewReader returns a Reader over kv.
func NewReader(kv KV) *Reader {
	return &Reader{kv: kv, cache: map[string]Record{}}
}

// Summary returns the newest run's hubs and the newest limit records.
func (r *Reader) Summary(ctx context.Context, limit int) (Summary, error) {
	keys, err := r.listKeys(ctx)
	if err != nil {
		return Summary{}, err
	}
	sum := Summary{Latest: []HubStatus{}, Runs: []Record{}}
	if len(keys) == 0 {
		return sum, nil
	}

	// Newest first; within a run, by hub id so the order is stable.
	slices.SortFunc(keys, func(a, b key) int {
		if c := cmp.Compare(b.startMS, a.startMS); c != 0 {
			return c
		}
		return cmp.Compare(a.hubID, b.hubID)
	})

	byHub := map[int64][]key{}
	for _, k := range keys {
		byHub[k.hubID] = append(byHub[k.hubID], k)
	}
	newest := keys[0].startMS
	for _, k := range keys {
		if k.startMS != newest {
			break
		}
		status, ok, err := r.hubStatus(ctx, byHub[k.hubID])
		if err != nil {
			return Summary{}, err
		}
		if ok {
			sum.Latest = append(sum.Latest, status)
		}
	}
	slices.SortFunc(sum.Latest, func(a, b HubStatus) int { return cmp.Compare(a.HubSlug, b.HubSlug) })

	for _, k := range keys {
		if len(sum.Runs) >= limit {
			break
		}
		rec, ok, err := r.get(ctx, k)
		if err != nil {
			return Summary{}, err
		}
		if ok {
			sum.Runs = append(sum.Runs, rec)
		}
	}
	return sum, nil
}

// Since returns every record of a run that started at or after since,
// oldest first.
func (r *Reader) Since(ctx context.Context, since time.Time) ([]Record, error) {
	keys, err := r.listKeys(ctx)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(keys, func(a, b key) int {
		if c := cmp.Compare(a.startMS, b.startMS); c != 0 {
			return c
		}
		return cmp.Compare(a.hubID, b.hubID)
	})
	sinceMS := since.UnixMilli()
	var out []Record
	for _, k := range keys {
		if k.startMS < sinceMS {
			continue
		}
		rec, ok, err := r.get(ctx, k)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, rec)
		}
	}
	return out, nil
}

// hubStatus builds the status of the hub whose keys are given newest
// first. ok is false when the newest record is gone (expired between the
// listing and the read).
func (r *Reader) hubStatus(ctx context.Context, keys []key) (HubStatus, bool, error) {
	newest, ok, err := r.get(ctx, keys[0])
	if err != nil || !ok {
		return HubStatus{}, false, err
	}
	status := HubStatus{Record: newest}

	counting := true // still in the run of empty successes
	for i, k := range keys {
		if i >= maxWalk || (!counting && status.LastSuccessAt != nil) {
			break
		}
		rec, ok, err := r.get(ctx, k)
		if err != nil {
			return HubStatus{}, false, err
		}
		if !ok || rec.Failed() {
			continue
		}
		if status.LastSuccessAt == nil {
			at := rec.FinishedAt
			status.LastSuccessAt = &at
			status.UpcomingSessions = rec.FutureSessions
		}
		if counting {
			if rec.FutureSessions == 0 {
				status.ConsecutiveEmptyRuns++
			} else {
				counting = false
			}
		}
	}
	return status, true, nil
}

// listKeys lists and parses every key, dropping cache entries for keys
// that have expired. A key that doesn't parse is logged and ignored.
func (r *Reader) listKeys(ctx context.Context) ([]key, error) {
	lister, err := r.kv.ListKeys(ctx)
	if errors.Is(err, jetstream.ErrNoKeysFound) {
		r.prune(nil)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list run keys: %w", err)
	}
	defer lister.Stop()

	var keys []key
	listed := map[string]bool{}
	for s := range lister.Keys() {
		k, err := parseKey(s)
		if err != nil {
			log.Printf("WARN ignoring %v", err)
			continue
		}
		keys = append(keys, k)
		listed[s] = true
	}
	// ListKeys closes its channel without an error when ctx ends, so a
	// short list may be a cut-off one.
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("list run keys: %w", err)
	}
	r.prune(listed)
	return keys, nil
}

// prune drops cached records whose key is no longer listed.
func (r *Reader) prune(listed map[string]bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for k := range r.cache {
		if !listed[k] {
			delete(r.cache, k)
		}
	}
}

// get returns the record at k, from the cache or the bucket. ok is false
// when the key is gone or its value doesn't decode (logged).
func (r *Reader) get(ctx context.Context, k key) (Record, bool, error) {
	s := k.String()
	r.mu.Lock()
	rec, hit := r.cache[s]
	r.mu.Unlock()
	if hit {
		return rec, true, nil
	}

	entry, err := r.kv.Get(ctx, s)
	if errors.Is(err, jetstream.ErrKeyNotFound) || errors.Is(err, jetstream.ErrKeyDeleted) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("get run record %s: %w", s, err)
	}
	if err := json.Unmarshal(entry.Value(), &rec); err != nil {
		log.Printf("WARN ignoring run record %s: %v", s, err)
		return Record{}, false, nil
	}

	r.mu.Lock()
	r.cache[s] = rec
	r.mu.Unlock()
	return rec, true, nil
}
