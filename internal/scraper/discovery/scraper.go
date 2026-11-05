// Package discovery orchestrates the scraper's end-to-end run: loading
// the set of already-known event IDs, fetching tracked hub pages, parsing
// new sessions off them, and queueing a notification for each new one.
package discovery

import (
	"context"
	"database/sql"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/scraper/config"
	"github.com/jfms7s/ticket-live-event-scanner/internal/scraper/fetch"
)

// baseURL is the only host the scraper is allowed to fetch from.
const baseURL = "https://www.ticketline.pt"

// httpTimeout bounds each individual page fetch.
const httpTimeout = 15 * time.Second

// Stats tracks counters for a single discovery run.
type Stats struct {
	EventsFound     int
	EventsPublished int
	// Skipped counts sessions not fetched or not published because of a
	// per-session data problem (see errSkip): rejected this run, or
	// rejected by an earlier run less than a day ago.
	Skipped int
	Errors  int
	// Hubs has one entry per configured hub page this run reached, in
	// config order.
	Hubs []HubStats
}

// HubStats is one hub page's outcome in a run.
type HubStats struct {
	Slug string
	// ID is the hub's event ID from its slug; 0 for a slug without one.
	ID int64
	// SessionsSeen counts the session cards on the hub page.
	SessionsSeen int
	// FutureSessions counts the cards dated today or later in
	// Europe/Lisbon. A card whose date doesn't parse is not counted.
	FutureSessions int
	// NewEvents counts the sessions of this hub published this run.
	NewEvents int
	// Err is why the hub failed this run; nil when it did not.
	Err error
}

// enqueuer stores a discovered session and queues its notification in one
// transaction (see package outbox). It reports whether a notification was
// queued: false means the session already had one.
type enqueuer interface {
	EnqueueDiscovered(ctx context.Context, d *event.Discovered) (bool, error)
}

// Scraper discovers new events from the configured hub pages and queues
// their notifications.
type Scraper struct {
	cfg      config.Config
	baseURL  string
	fetcher  *fetch.Fetcher
	db       *sql.DB
	queue    enqueuer
	knownIDs map[int64]bool
	skipped  skipStore
	skipIDs  map[int64]bool
	// now is the clock "future session" is counted against.
	now   func() time.Time
	Stats Stats
}

// New constructs a Scraper that queues what it finds through q and reads
// the sessions it already knows (the events table, hidden ones included) and
// the ones the parser rejected recently (skipped_sessions) from db.
func New(cfg config.Config, db *sql.DB, q enqueuer) *Scraper {
	return newScraper(cfg, baseURL, db, q)
}

// newScraper builds a Scraper that fetches from site (baseURL in
// production, an httptest server in tests).
func newScraper(cfg config.Config, site string, db *sql.DB, q enqueuer) *Scraper {
	return &Scraper{
		cfg:      cfg,
		baseURL:  site,
		fetcher:  fetch.New(cfg.UserAgent, httpTimeout),
		db:       db,
		queue:    q,
		knownIDs: make(map[int64]bool),
		skipped:  newSkipStore(db),
		skipIDs:  make(map[int64]bool),
		now:      time.Now,
	}
}
