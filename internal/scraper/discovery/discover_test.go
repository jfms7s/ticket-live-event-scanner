package discovery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/scraper/config"
	"github.com/jfms7s/ticket-live-event-scanner/internal/store"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/jfms7s/ticket-live-event-scanner/internal/webui/repository"
	"github.com/nats-io/nats.go/jetstream"
	_ "modernc.org/sqlite"
)

const hubSlug = "fado-hub-500"

// hubSite is an httptest stand-in for ticketline.pt: one hub page listing
// the sessions in ids, and a detail page for each.
type hubSite struct {
	mu  sync.Mutex
	ids []int64
	srv *httptest.Server
	// detail overrides the detail page for an event ID.
	detail map[int64]http.HandlerFunc
	// detailFetches counts requests per detail page.
	detailFetches map[int64]int
}

func newHubSite(t *testing.T, ids ...int64) *hubSite {
	t.Helper()
	h := &hubSite{ids: ids, detail: map[int64]http.HandlerFunc{}, detailFetches: map[int64]int{}}
	h.srv = httptest.NewServer(http.HandlerFunc(h.serve))
	t.Cleanup(h.srv.Close)
	return h
}

func (h *hubSite) list(ids ...int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ids = ids
}

func (h *hubSite) serve(w http.ResponseWriter, r *http.Request) {
	slug, ok := strings.CutPrefix(r.URL.Path, "/evento/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if slug == hubSlug {
		h.mu.Lock()
		ids := append([]int64(nil), h.ids...)
		h.mu.Unlock()
		var b strings.Builder
		b.WriteString("<html><body><ul>")
		for _, id := range ids {
			fmt.Fprintf(&b, `<li itemscope itemtype="http://schema.org/Event">
	<a itemprop="url" href="/evento/session-%d">
		<h3 itemprop="name">Session %d</h3>
		<span itemprop="startDate" content="2030-01-01">1 Jan 2030</span>
	</a>
</li>`, id, id)
		}
		b.WriteString("</ul></body></html>")
		fmt.Fprint(w, b.String())
		return
	}
	idStr, ok := strings.CutPrefix(slug, "session-")
	if !ok {
		http.NotFound(w, r)
		return
	}
	id, _ := strconv.ParseInt(idStr, 10, 64)
	h.mu.Lock()
	h.detailFetches[id]++
	override := h.detail[id]
	h.mu.Unlock()
	if override != nil {
		override(w, r)
		return
	}
	fmt.Fprintf(w, `<html><body><div itemscope itemtype="http://schema.org/Event">
	<h1 itemprop="name">Session %s</h1>
	<p itemprop="location">Casa do Fado</p>
	<span itemprop="startDate" content="2030-01-01">1 Jan 2030</span>
</div></body></html>`, idStr)
}

// recordingPublisher records every publish. It applies no message-ID
// dedup, as if the stream's duplicate window had already passed.
type recordingPublisher struct {
	published []event.Discovered
}

func (p *recordingPublisher) Publish(_ context.Context, subject string, data []byte, _ ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	if subject != streams.EventsSubject {
		return nil, fmt.Errorf("unexpected subject %q", subject)
	}
	var d event.Discovered
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	p.published = append(p.published, d)
	return &jetstream.PubAck{Stream: streams.EventsStreamName}, nil
}

func (p *recordingPublisher) count(id int64) int {
	n := 0
	for _, d := range p.published {
		if d.EventID == id {
			n++
		}
	}
	return n
}

// memKV is an in-memory stand-in for the seen KV bucket.
type memKV struct {
	mu      sync.Mutex
	keys    map[string]bool
	listErr error
	putErr  error
}

func newMemKV() *memKV { return &memKV{keys: map[string]bool{}} }

func (m *memKV) Put(_ context.Context, key string, _ []byte) (uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.putErr != nil {
		return 0, m.putErr
	}
	m.keys[key] = true
	return uint64(len(m.keys)), nil
}

func (m *memKV) ListKeys(context.Context, ...jetstream.WatchOpt) (jetstream.KeyLister, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listErr != nil {
		return nil, m.listErr
	}
	ch := make(chan string, len(m.keys))
	for k := range m.keys {
		ch <- k
	}
	close(ch)
	return memLister{ch}, nil
}

type memLister struct{ ch chan string }

func (l memLister) Keys() <-chan string { return l.ch }
func (memLister) Stop() error           { return nil }

// migratedDB is web-ui-api's database after store.Migrate, with no rows.
func migratedDB(t *testing.T) *sql.DB {
	t.Helper()
	db := emptyDB(t)
	if err := store.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// emptyDB is a database web-ui-api has never started against: no tables.
func emptyDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/scanner.db")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return db
}

// run is one scraper process: a fresh Scraper (so nothing in memory carries
// over), loading the seen set and discovering once.
func run(t *testing.T, site *hubSite, kv seenKV, db *sql.DB, pub publisher) error {
	t.Helper()
	cfg := config.Config{UserAgent: "test", HubPages: []string{hubSlug}}
	s := newScraper(cfg, site.srv.URL, newSeenStore(kv, db), pub)
	if err := s.LoadKnownIDs(context.Background()); err != nil {
		return fmt.Errorf("load known IDs: %w", err)
	}
	return s.Discover(context.Background())
}

func mustRun(t *testing.T, site *hubSite, kv seenKV, db *sql.DB, pub publisher) {
	t.Helper()
	if err := run(t, site, kv, db, pub); err != nil {
		t.Fatalf("run: %v", err)
	}
}

// materialise does what web-ui-api's events consumer does with each
// published message.
func materialise(t *testing.T, db *sql.DB, pub *recordingPublisher) {
	t.Helper()
	for i := range pub.published {
		if err := repository.UpsertEventAndInsertNotification(context.Background(), db, &pub.published[i], uint64(i+1)); err != nil {
			t.Fatalf("materialise event %d: %v", pub.published[i].EventID, err)
		}
	}
}

func assertPublishedOnce(t *testing.T, pub *recordingPublisher, ids ...int64) {
	t.Helper()
	for _, id := range ids {
		if n := pub.count(id); n != 1 {
			t.Errorf("event %d published %d times across runs, want 1", id, n)
		}
	}
	if len(pub.published) != len(ids) {
		t.Errorf("published %d messages, want %d", len(pub.published), len(ids))
	}
}

// TestDiscoverTwiceWhileWebUIIsDown is the hourly scrape with web-ui-api
// down (or behind): nothing materialises the events table between runs, so
// the second run must still know what the first published.
func TestDiscoverTwiceWhileWebUIIsDown(t *testing.T) {
	site := newHubSite(t, 101, 102)
	kv, db, pub := newMemKV(), migratedDB(t), &recordingPublisher{}

	mustRun(t, site, kv, db, pub)
	mustRun(t, site, kv, db, pub)

	assertPublishedOnce(t, pub, 101, 102)
}

// TestDiscoverAfterRemove is the dashboard's Remove between runs: the
// event is still listed on its hub page and must not be notified again.
func TestDiscoverAfterRemove(t *testing.T) {
	site := newHubSite(t, 101, 102)
	kv, db, pub := newMemKV(), migratedDB(t), &recordingPublisher{}

	mustRun(t, site, kv, db, pub)
	materialise(t, db, pub)
	if err := repository.HideEvent(context.Background(), db, 101); err != nil {
		t.Fatalf("remove event 101: %v", err)
	}
	mustRun(t, site, kv, db, pub)

	assertPublishedOnce(t, pub, 101, 102)
}

// TestDiscoverSkipsEventsKnownOnlyToTheDatabase is the first run after the
// seen bucket was introduced (or after it was lost): the bucket is empty,
// and events already notified, including removed ones, are known only
// from web-ui-api's events table. They must not be notified again.
func TestDiscoverSkipsEventsKnownOnlyToTheDatabase(t *testing.T) {
	site := newHubSite(t, 101, 102)
	db, pub := migratedDB(t), &recordingPublisher{}

	mustRun(t, site, newMemKV(), db, pub)
	materialise(t, db, pub)
	if err := repository.HideEvent(context.Background(), db, 101); err != nil {
		t.Fatalf("remove event 101: %v", err)
	}
	mustRun(t, site, newMemKV(), db, pub)

	assertPublishedOnce(t, pub, 101, 102)
}

// TestDiscoverPublishesNewSessionsOnLaterRuns guards the other way a dedup
// bug fails: silently suppressing a genuinely new session.
func TestDiscoverPublishesNewSessionsOnLaterRuns(t *testing.T) {
	site := newHubSite(t, 101)
	kv, db, pub := newMemKV(), migratedDB(t), &recordingPublisher{}

	mustRun(t, site, kv, db, pub)
	site.list(101, 103)
	mustRun(t, site, kv, db, pub)

	assertPublishedOnce(t, pub, 101, 103)
}

// TestDiscoverBeforeWebUIEverStarted runs against a database that has no
// events table yet (web-ui-api runs Migrate, the scraper doesn't), twice.
func TestDiscoverBeforeWebUIEverStarted(t *testing.T) {
	site := newHubSite(t, 101)
	kv, db, pub := newMemKV(), emptyDB(t), &recordingPublisher{}

	mustRun(t, site, kv, db, pub)
	mustRun(t, site, kv, db, pub)

	assertPublishedOnce(t, pub, 101)
}

// TestDiscoverFailsWhenTheSeenBucketCannotBeRead checks that an unreadable
// bucket stops the run instead of treating every event as new.
func TestDiscoverFailsWhenTheSeenBucketCannotBeRead(t *testing.T) {
	site := newHubSite(t, 101)
	kv, db, pub := newMemKV(), migratedDB(t), &recordingPublisher{}
	kv.listErr = errors.New("nats: timeout")

	if err := run(t, site, kv, db, pub); err == nil {
		t.Fatal("run succeeded with an unreadable seen bucket, want an error")
	}
	if len(pub.published) != 0 {
		t.Errorf("published %d messages with an unreadable seen bucket, want 0", len(pub.published))
	}
}

// TestDiscoverReportsAFailedMark checks that a publish whose seen mark
// fails is still counted as published but fails the run, so the error is
// visible (the stream's 7-day message-ID window still covers the next runs).
func TestDiscoverReportsAFailedMark(t *testing.T) {
	site := newHubSite(t, 101)
	kv, db, pub := newMemKV(), migratedDB(t), &recordingPublisher{}
	kv.putErr = errors.New("nats: timeout")

	if err := run(t, site, kv, db, pub); err == nil {
		t.Fatal("run succeeded although marking the event seen failed, want an error")
	}
	assertPublishedOnce(t, pub, 101)
}

func TestSeenStoreKeysAreEventIDs(t *testing.T) {
	kv := newMemKV()
	seen := newSeenStore(kv, migratedDB(t))
	if err := seen.Mark(context.Background(), 98164); err != nil {
		t.Fatal(err)
	}
	if !kv.keys[strconv.FormatInt(98164, 10)] {
		t.Fatalf("bucket keys = %v, want \"98164\"", kv.keys)
	}

	kv.keys["not-a-number"] = true
	if _, err := seen.Load(context.Background()); err == nil {
		t.Error("Load accepted a key that is not an event ID, want an error")
	}
}

// duplicateAckPublisher acks every publish as a stream duplicate, as
// JetStream does for a message ID it saw within the Duplicates window.
type duplicateAckPublisher struct{ calls int }

func (p *duplicateAckPublisher) Publish(context.Context, string, []byte, ...jetstream.PublishOpt) (*jetstream.PubAck, error) {
	p.calls++
	return &jetstream.PubAck{Stream: streams.EventsStreamName, Duplicate: true}, nil
}

// TestDiscoverMarksStreamDuplicatesSeen covers a run after one that
// published but failed to mark: the stream rejects the repeat as a
// duplicate, which is not a new publish, but the event is now marked seen.
func TestDiscoverMarksStreamDuplicatesSeen(t *testing.T) {
	site := newHubSite(t, 101)
	kv, db, pub := newMemKV(), migratedDB(t), &duplicateAckPublisher{}

	cfg := config.Config{UserAgent: "test", HubPages: []string{hubSlug}}
	s := newScraper(cfg, site.srv.URL, newSeenStore(kv, db), pub)
	if err := s.LoadKnownIDs(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.Stats.EventsPublished != 0 {
		t.Errorf("EventsPublished = %d, want 0 for a stream duplicate", s.Stats.EventsPublished)
	}
	if !kv.keys["101"] {
		t.Error("event 101 not marked seen after a duplicate ack")
	}
}
