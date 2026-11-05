package discovery

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/outbox"
	"github.com/jfms7s/ticket-live-event-scanner/internal/scraper/config"
	"github.com/jfms7s/ticket-live-event-scanner/internal/store"
	"github.com/jfms7s/ticket-live-event-scanner/internal/webui/repository"
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
	// cardDates overrides a session card's start date (default 2030-01-01).
	cardDates map[int64]string
	// hubStatus, when set, is the hub page's HTTP status.
	hubStatus int
}

func newHubSite(t *testing.T, ids ...int64) *hubSite {
	t.Helper()
	h := &hubSite{ids: ids, detail: map[int64]http.HandlerFunc{}, detailFetches: map[int64]int{}, cardDates: map[int64]string{}}
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
		status := h.hubStatus
		dates := make(map[int64]string, len(h.cardDates))
		for id, d := range h.cardDates {
			dates[id] = d
		}
		h.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		var b strings.Builder
		b.WriteString("<html><body><ul>")
		for _, id := range ids {
			date := dates[id]
			if date == "" {
				date = "2030-01-01"
			}
			fmt.Fprintf(&b, `<li itemscope itemtype="http://schema.org/Event">
	<a itemprop="url" href="/evento/session-%d">
		<h3 itemprop="name">Session %d</h3>
		<span itemprop="startDate" content="%s">%s</span>
	</a>
</li>`, id, id, date, date)
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

// recordingPublisher queues through the real outbox and records what it
// queued. err, when set, makes every enqueue fail.
type recordingPublisher struct {
	q         *outbox.Queue
	published []event.Discovered
	err       error
}

func newQueue(db *sql.DB) *recordingPublisher { return &recordingPublisher{q: &outbox.Queue{DB: db}} }

func (p *recordingPublisher) EnqueueDiscovered(ctx context.Context, d *event.Discovered) (bool, error) {
	if p.err != nil {
		return false, p.err
	}
	queued, err := p.q.EnqueueDiscovered(ctx, d)
	if queued {
		p.published = append(p.published, *d)
	}
	return queued, err
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

// migratedDB is web-ui-api's database after store.Open, with no rows.
func migratedDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(context.Background(), store.Options{Path: filepath.Join(t.TempDir(), "scanner.db"), AllowEmpty: true})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// run is one scraper process: a fresh Scraper (so nothing in memory carries
// over), loading the known set and discovering once.
func run(t *testing.T, site *hubSite, db *sql.DB, pub *recordingPublisher) error {
	t.Helper()
	cfg := config.Config{UserAgent: "test", HubPages: []string{hubSlug}}
	s := newScraper(cfg, site.srv.URL, db, pub)
	if err := s.LoadKnownIDs(context.Background()); err != nil {
		return fmt.Errorf("load known IDs: %w", err)
	}
	return s.Discover(context.Background())
}

func mustRun(t *testing.T, site *hubSite, db *sql.DB, pub *recordingPublisher) {
	t.Helper()
	if err := run(t, site, db, pub); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func assertPublishedOnce(t *testing.T, pub *recordingPublisher, ids ...int64) {
	t.Helper()
	for _, id := range ids {
		if n := pub.count(id); n != 1 {
			t.Errorf("event %d queued %d times across runs, want 1", id, n)
		}
	}
	if len(pub.published) != len(ids) {
		t.Errorf("queued %d notifications, want %d", len(pub.published), len(ids))
	}
}

// rowsFor counts the notification rows of an event.
func rowsFor(t *testing.T, db *sql.DB, id int64) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM notifications WHERE event_id = ?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestDiscoverTwiceQueuesEachSessionOnce is two hourly runs over the same
// hub page: the second finds everything known.
func TestDiscoverTwiceQueuesEachSessionOnce(t *testing.T) {
	site := newHubSite(t, 101, 102)
	db, pub := migratedDB(t), (*recordingPublisher)(nil)
	pub = newQueue(db)

	mustRun(t, site, db, pub)
	mustRun(t, site, db, pub)

	assertPublishedOnce(t, pub, 101, 102)
	if rowsFor(t, db, 101) != 1 || rowsFor(t, db, 102) != 1 {
		t.Error("a session has more than one notification row")
	}
}

// TestDiscoverAfterRemove is the dashboard's Remove between runs: the
// event is still listed on its hub page and must not be notified again.
func TestDiscoverAfterRemove(t *testing.T) {
	site := newHubSite(t, 101, 102)
	db := migratedDB(t)
	pub := newQueue(db)

	mustRun(t, site, db, pub)
	if err := repository.HideEvent(context.Background(), db, 101); err != nil {
		t.Fatalf("remove event 101: %v", err)
	}
	mustRun(t, site, db, pub)

	assertPublishedOnce(t, pub, 101, 102)
}

// TestDiscoverPublishesNewSessionsOnLaterRuns guards the other way a dedup
// bug fails: silently suppressing a genuinely new session.
func TestDiscoverPublishesNewSessionsOnLaterRuns(t *testing.T) {
	site := newHubSite(t, 101)
	db := migratedDB(t)
	pub := newQueue(db)

	mustRun(t, site, db, pub)
	site.list(101, 103)
	mustRun(t, site, db, pub)

	assertPublishedOnce(t, pub, 101, 103)
}

// TestDiscoverSkipsSessionsAlreadyInTheDatabase: a session stored by an
// earlier release (before the outbox existed) is known and not queued.
func TestDiscoverSkipsSessionsAlreadyInTheDatabase(t *testing.T) {
	site := newHubSite(t, 101, 102)
	db := migratedDB(t)
	if _, err := db.Exec(`INSERT INTO events (id, slug, title, url, hidden) VALUES (101, 's', 't', 'u', 1)`); err != nil {
		t.Fatal(err)
	}
	pub := newQueue(db)

	mustRun(t, site, db, pub)

	assertPublishedOnce(t, pub, 102)
	if n := site.detailFetches[101]; n != 0 {
		t.Errorf("known session 101 was fetched %d times, want 0", n)
	}
}

// TestDiscoverOverlappingRunsQueueOnce is two processes that both loaded the
// known set before either queued: the second insert is a no-op, so the
// session is announced once.
func TestDiscoverOverlappingRunsQueueOnce(t *testing.T) {
	site := newHubSite(t, 101)
	db := migratedDB(t)
	a, b := newQueue(db), newQueue(db)
	cfg := config.Config{UserAgent: "test", HubPages: []string{hubSlug}}
	sa, sb := newScraper(cfg, site.srv.URL, db, a), newScraper(cfg, site.srv.URL, db, b)
	for _, s := range []*Scraper{sa, sb} {
		if err := s.LoadKnownIDs(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if err := sa.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := sb.Discover(context.Background()); err != nil {
		t.Fatal(err)
	}

	if len(a.published)+len(b.published) != 1 || rowsFor(t, db, 101) != 1 {
		t.Errorf("queued %d + %d, rows %d; want the session queued exactly once", len(a.published), len(b.published), rowsFor(t, db, 101))
	}
	if sb.Stats.EventsPublished != 0 {
		t.Errorf("the second run counted %d queued, want 0", sb.Stats.EventsPublished)
	}
}

// TestDiscoverFailsWhenQueueingFails: a session whose notification cannot be
// queued is not stored either, so the next run finds it new and tries again.
func TestDiscoverFailsWhenQueueingFails(t *testing.T) {
	site := newHubSite(t, 101)
	db := migratedDB(t)
	pub := newQueue(db)
	if _, err := db.Exec(`CREATE TRIGGER refuse BEFORE INSERT ON notifications BEGIN SELECT RAISE(ABORT, 'disk full'); END`); err != nil {
		t.Fatal(err)
	}

	if err := run(t, site, db, pub); err == nil {
		t.Fatal("run succeeded although the notification could not be queued")
	}
	var n int
	db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&n)
	if n != 0 {
		t.Error("the event was stored without its notification (it would never be announced)")
	}

	db.Exec(`DROP TRIGGER refuse`)
	mustRun(t, site, db, pub)
	assertPublishedOnce(t, pub, 101)
}
