// Package discovery orchestrates the scraper's end-to-end run: loading
// known event IDs, fetching tracked hub pages, parsing new sessions off
// them, and publishing newly discovered events to NATS JetStream.
package discovery

import (
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

// Scraper discovers new events from the configured hub pages and publishes
// them to NATS JetStream.
type Scraper struct {
	cfg      config.Config
	fetcher  *fetch.Fetcher
	db       *sql.DB
	js       jetstream.JetStream
	knownIDs map[int64]bool
	Stats    Stats
}

// New constructs a Scraper against the given config, database, and
// JetStream context.
func New(cfg config.Config, db *sql.DB, js jetstream.JetStream) *Scraper {
	return &Scraper{
		cfg:      cfg,
		fetcher:  fetch.New(cfg.UserAgent, httpTimeout),
		db:       db,
		js:       js,
		knownIDs: make(map[int64]bool),
	}
}
