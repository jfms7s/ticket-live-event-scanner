package runs

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// memKV is an in-memory stand-in for the runs bucket.
type memKV struct {
	mu      sync.Mutex
	vals    map[string][]byte
	gets    int
	listErr error
	getErr  error
}

func newMemKV() *memKV { return &memKV{vals: map[string][]byte{}} }

func (m *memKV) Put(_ context.Context, key string, value []byte) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.vals[key] = value
	return uint64(len(m.vals)), nil
}

func (m *memKV) Get(_ context.Context, key string) (jetstream.KeyValueEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gets++
	if m.getErr != nil {
		return nil, m.getErr
	}
	v, ok := m.vals[key]
	if !ok {
		return nil, jetstream.ErrKeyNotFound
	}
	return entry{key: key, value: v}, nil
}

func (m *memKV) ListKeys(context.Context, ...jetstream.WatchOpt) (jetstream.KeyLister, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, m.listErr
	}
	ch := make(chan string, len(m.vals))
	for k := range m.vals {
		ch <- k
	}
	close(ch)
	return lister{ch}, nil
}

func (m *memKV) delete(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.vals, key)
}

type lister struct{ ch chan string }

func (l lister) Keys() <-chan string { return l.ch }
func (lister) Stop() error           { return nil }

type entry struct {
	key   string
	value []byte
}

func (e entry) Bucket() string                  { return "SCRAPER_RUNS" }
func (e entry) Key() string                     { return e.key }
func (e entry) Value() []byte                   { return e.value }
func (e entry) Revision() uint64                { return 1 }
func (e entry) Created() time.Time              { return time.Time{} }
func (e entry) Delta() uint64                   { return 0 }
func (e entry) Operation() jetstream.KeyValueOp { return jetstream.KeyValuePut }

var t0 = time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)

// rec is hub's record for the run started n hours after t0.
func rec(hub int64, n int, future int, errMsg string) Record {
	start := t0.Add(time.Duration(n) * time.Hour)
	return Record{
		RunID:          RunID(start),
		HubID:          hub,
		HubSlug:        slugOf(hub),
		StartedAt:      start,
		FinishedAt:     start.Add(30 * time.Second),
		SessionsSeen:   future + 2,
		FutureSessions: future,
		Error:          errMsg,
	}
}

func slugOf(hub int64) string {
	switch hub {
	case 1:
		return "aveiro-1"
	case 2:
		return "maia-2"
	}
	return "hub"
}

func write(t *testing.T, kv *memKV, recs ...Record) {
	t.Helper()
	if err := Write(context.Background(), kv, recs); err != nil {
		t.Fatal(err)
	}
}

func TestKeyRoundTrip(t *testing.T) {
	r := rec(98164, 0, 1, "")
	k, err := parseKey(Key(r))
	if err != nil {
		t.Fatal(err)
	}
	if k.hubID != 98164 || k.startMS != t0.UnixMilli() {
		t.Errorf("parseKey(%q) = %+v", Key(r), k)
	}
	for _, bad := range []string{"98164", "x.1", "1.x", ""} {
		if _, err := parseKey(bad); err == nil {
			t.Errorf("parseKey(%q) = nil error, want one", bad)
		}
	}
}

func TestSummaryOfAnEmptyBucket(t *testing.T) {
	sum, err := NewReader(newMemKV()).Summary(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Latest == nil || sum.Runs == nil || len(sum.Latest)+len(sum.Runs) != 0 {
		t.Errorf("Summary = %+v, want empty non-nil slices (JSON [])", sum)
	}
}

func TestSummaryLatestIsTheNewestRunPerHub(t *testing.T) {
	kv := newMemKV()
	write(t, kv,
		rec(1, 0, 3, ""), rec(2, 0, 4, ""),
		rec(1, 1, 3, ""), rec(2, 1, 0, "fetch hub page 2: timeout"),
		// A hub dropped from the config: its last record is hours older
		// than the newest run.
		rec(9, -5, 5, ""),
	)
	sum, err := NewReader(kv).Summary(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.Latest) != 2 {
		t.Fatalf("Latest = %+v, want hubs 1 and 2", sum.Latest)
	}
	aveiro, maia := sum.Latest[0], sum.Latest[1]
	if aveiro.HubID != 1 || maia.HubID != 2 {
		t.Fatalf("Latest order = %d, %d, want by slug (aveiro, maia)", aveiro.HubID, maia.HubID)
	}
	if !maia.Failed() || maia.StartedAt != t0.Add(time.Hour) {
		t.Errorf("maia newest = %+v, want the failed run at t0+1h", maia.Record)
	}
	if maia.LastSuccessAt == nil || !maia.LastSuccessAt.Equal(t0.Add(30*time.Second)) {
		t.Errorf("maia LastSuccessAt = %v, want the t0 run's finish", maia.LastSuccessAt)
	}
	if maia.UpcomingSessions != 4 {
		t.Errorf("maia UpcomingSessions = %d, want 4 from its last success", maia.UpcomingSessions)
	}
	if aveiro.Failed() || aveiro.UpcomingSessions != 3 {
		t.Errorf("aveiro = %+v, want ok with 3 upcoming", aveiro)
	}

	if len(sum.Runs) != 3 {
		t.Fatalf("Runs has %d records, want the limit 3", len(sum.Runs))
	}
	if !sum.Runs[0].StartedAt.Equal(t0.Add(time.Hour)) || !sum.Runs[2].StartedAt.Equal(t0) {
		t.Errorf("Runs not newest first: %v .. %v", sum.Runs[0].StartedAt, sum.Runs[2].StartedAt)
	}
}

// TestConsecutiveEmptyRuns is the empty-hub rule as a table over one hub's
// history, oldest first.
func TestConsecutiveEmptyRuns(t *testing.T) {
	type run struct {
		future int
		err    string
	}
	ok := func(n int) run { return run{future: n} }
	fail := run{err: "boom"}
	tests := []struct {
		name      string
		history   []run
		wantCount int
		wantEmpty bool
	}{
		{"never empty", []run{ok(2), ok(3)}, 0, false},
		{"two empty runs", []run{ok(2), ok(0), ok(0)}, 2, false},
		{"third empty run", []run{ok(2), ok(0), ok(0), ok(0)}, 3, true},
		{"refilled", []run{ok(0), ok(0), ok(0), ok(1)}, 0, false},
		{"failures in between are skipped", []run{ok(0), fail, ok(0), fail, ok(0)}, 3, true},
		{"failing now, empty before", []run{ok(0), ok(0), ok(0), fail}, 3, true},
		{"only failures", []run{fail, fail}, 0, false},
		{"empty since the bucket began", []run{ok(0), ok(0), ok(0), ok(0)}, 4, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kv := newMemKV()
			for i, r := range tt.history {
				write(t, kv, rec(1, i, r.future, r.err))
			}
			sum, err := NewReader(kv).Summary(context.Background(), 0)
			if err != nil {
				t.Fatal(err)
			}
			got := sum.Latest[0]
			if got.ConsecutiveEmptyRuns != tt.wantCount || got.Empty() != tt.wantEmpty {
				t.Errorf("ConsecutiveEmptyRuns = %d (empty %v), want %d (%v)",
					got.ConsecutiveEmptyRuns, got.Empty(), tt.wantCount, tt.wantEmpty)
			}
		})
	}
}

func TestHubStatusWalkIsBounded(t *testing.T) {
	kv := newMemKV()
	for i := 0; i < 3*maxWalk; i++ {
		write(t, kv, rec(1, i, 0, "boom"))
	}
	if _, err := NewReader(kv).Summary(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if kv.gets > maxWalk+1 {
		t.Errorf("Summary made %d gets for one failing hub, want at most %d", kv.gets, maxWalk+1)
	}
}

func TestReaderCachesRecords(t *testing.T) {
	kv := newMemKV()
	write(t, kv, rec(1, 0, 1, ""), rec(1, 1, 1, ""))
	r := NewReader(kv)
	if _, err := r.Summary(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	first := kv.gets
	if _, err := r.Summary(context.Background(), 10); err != nil {
		t.Fatal(err)
	}
	if kv.gets != first {
		t.Errorf("second Summary made %d more gets, want 0 (records never change)", kv.gets-first)
	}

	// An expired key leaves the cache.
	kv.delete(Key(rec(1, 0, 1, "")))
	sum, err := r.Summary(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.Runs) != 1 {
		t.Errorf("Runs = %d records after one expired, want 1", len(sum.Runs))
	}
	if len(r.cache) != 1 {
		t.Errorf("cache holds %d records, want 1", len(r.cache))
	}
}

func TestSummaryIgnoresBadKeysAndValues(t *testing.T) {
	kv := newMemKV()
	write(t, kv, rec(1, 0, 1, ""))
	kv.vals["not-a-run-key"] = []byte("{}")
	kv.vals[keyOf(1, t0.Add(-time.Hour).UnixMilli())] = []byte("not json")
	sum, err := NewReader(kv).Summary(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.Runs) != 1 || len(sum.Latest) != 1 {
		t.Errorf("Summary = %+v, want the one good record", sum)
	}
}

func TestSummaryReportsBucketErrors(t *testing.T) {
	kv := newMemKV()
	write(t, kv, rec(1, 0, 1, ""))
	kv.getErr = errors.New("nats: timeout")
	if _, err := NewReader(kv).Summary(context.Background(), 10); err == nil {
		t.Error("Summary = nil error with a failing Get, want one")
	}
	kv.listErr = errors.New("nats: timeout")
	if _, err := NewReader(kv).Summary(context.Background(), 10); err == nil {
		t.Error("Summary = nil error with a failing ListKeys, want one")
	}
}

func TestSummaryFailsOnACancelledListing(t *testing.T) {
	kv := newMemKV()
	write(t, kv, rec(1, 0, 1, ""))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewReader(kv).Summary(ctx, 10); err == nil {
		t.Error("Summary = nil error on a cancelled context, want one (the listing may be cut off)")
	}
}

func TestSince(t *testing.T) {
	kv := newMemKV()
	write(t, kv, rec(1, 0, 1, ""), rec(2, 0, 1, ""), rec(1, 5, 1, "boom"), rec(1, 9, 1, ""))
	got, err := NewReader(kv).Since(context.Background(), t0.Add(5*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !got[0].Failed() || !got[1].StartedAt.Equal(t0.Add(9*time.Hour)) {
		t.Errorf("Since = %+v, want the t0+5h and t0+9h records, oldest first", got)
	}
}

// TestReaderIsSafeForConcurrentUse is /api/runs and /metrics reading at
// once while keys expire (run with -race).
func TestReaderIsSafeForConcurrentUse(t *testing.T) {
	kv := newMemKV()
	for i := 0; i < 20; i++ {
		write(t, kv, rec(1, i, i%2, ""), rec(2, i, 0, ""))
	}
	r := NewReader(kv)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				if g == 0 {
					kv.delete(Key(rec(1, i, i%2, "")))
				}
				if _, err := r.Summary(context.Background(), 10); err != nil {
					t.Error(err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
}

// TestSummaryKeepsAHubWhoseNewestPutFailed: hub 2's record of the newest
// run was never written; it must stay visible on its previous record.
func TestSummaryKeepsAHubWhoseNewestPutFailed(t *testing.T) {
	kv := newMemKV()
	write(t, kv, rec(1, 0, 1, ""), rec(2, 0, 0, "boom"), rec(1, 1, 1, ""))
	sum, err := NewReader(kv).Summary(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(sum.Latest) != 2 || !sum.Latest[1].Failed() {
		t.Errorf("Latest = %+v, want both hubs, hub 2 still failing", sum.Latest)
	}
}

// TestRetriesInOneHourCountAsOneEmptyRun: a failed run retried twice in
// the same hour must not reach the empty threshold on its own.
func TestRetriesInOneHourCountAsOneEmptyRun(t *testing.T) {
	kv := newMemKV()
	base := rec(1, 0, 0, "")
	for i := 0; i < 3; i++ {
		r := base
		r.StartedAt = base.StartedAt.Add(time.Duration(i) * time.Minute)
		write(t, kv, r)
	}
	sum, err := NewReader(kv).Summary(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := sum.Latest[0].ConsecutiveEmptyRuns; got != 1 {
		t.Errorf("ConsecutiveEmptyRuns = %d for three attempts in one hour, want 1", got)
	}
}

// TestLastSuccessAfterTwoDaysOfFailures: the walk reaches a success older
// than two days of hourly failures.
func TestLastSuccessAfterTwoDaysOfFailures(t *testing.T) {
	kv := newMemKV()
	write(t, kv, rec(1, 0, 2, ""))
	for i := 1; i <= 60; i++ {
		write(t, kv, rec(1, i, 0, "boom"))
	}
	sum, err := NewReader(kv).Summary(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Latest[0].LastSuccessAt == nil {
		t.Error("LastSuccessAt = nil, want the success 60 runs back")
	}
}
