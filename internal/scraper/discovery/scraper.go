// Package discovery orchestrates the scraper's end-to-end run: loading
// the set of already-seen event IDs, fetching tracked hub pages, parsing
// new sessions off them, and publishing newly discovered events to NATS
// JetStream.
package discovery

import (
	"context"
	"database/sql"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/scraper/config"
	"github.com/jfms7s/ticket-live-event-scanner/internal/scraper/fetch"
	"github.com/nats-io/nats.go/jetstream"
)

// baseURL is the only host the scraper is allowed to fetch from.
const baseURL = "https://www.ticketline.pt"

// httpTimeout bounds each individual page fetch.
const httpTimeout = 15 * time.Second

// Stats tracks counters for a single discovery run.
type Stats struct {
	EventsFound     int
	EventsPublished int
	Errors          int
}

// publisher is the part of jetstream.JetStream the scraper uses.
type publisher interface {
	Publish(ctx context.Context, subject string, payload []byte, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error)
}

// seenStore holds the IDs of events the scraper has already published, so
// a later run doesn't publish them again.
type seenStore interface {
	// Load returns every seen event ID.
	Load(ctx context.Context) (map[int64]bool, error)
	// Mark records id as seen. It is called right after a successful
	// publish.
	Mark(ctx context.Context, id int64) error
}

// Scraper discovers new events from the configured hub pages and publishes
// them to NATS JetStream.
type Scraper struct {
	cfg      config.Config
	baseURL  string
	fetcher  *fetch.Fetcher
	seen     seenStore
	pub      publisher
	knownIDs map[int64]bool
	Stats    Stats
}

// New constructs a Scraper against the given config, database, and
// JetStream context.
func New(cfg config.Config, db *sql.DB, js jetstream.JetStream) *Scraper {
	return newScraper(cfg, baseURL, dbSeenStore{db: db}, js)
}

// newScraper builds a Scraper that fetches from site (baseURL in
// production, an httptest server in tests).
func newScraper(cfg config.Config, site string, seen seenStore, pub publisher) *Scraper {
	return &Scraper{
		cfg:      cfg,
		baseURL:  site,
		fetcher:  fetch.New(cfg.UserAgent, httpTimeout),
		seen:     seen,
		pub:      pub,
		knownIDs: make(map[int64]bool),
	}
}
