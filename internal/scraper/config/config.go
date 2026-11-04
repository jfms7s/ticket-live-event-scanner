// Package config loads scraper configuration from environment variables.
package config

import (
	"github.com/jfms7s/ticket-live-event-scanner/internal/envconfig"
)

// Config holds the scraper's runtime configuration.
type Config struct {
	NatsURL          string
	TursoDatabaseURL string
	TursoAuthToken   string
	UserAgent        string
	RequestDelayMS   int
	// HubPages is TICKETLINE_HUB_PAGES, which only seeds the watches table
	// (see package watch); the run scrapes watch.Resolve's result.
	HubPages []string
}

// defaultHubPages are the hub pages seeded when TICKETLINE_HUB_PAGES is not
// set. It is empty on purpose: there is no sensible default. A run with no
// enabled watch fails (see cmd/scraper).
var defaultHubPages = []string{}

// Load builds a Config from environment variables, applying defaults where
// applicable and terminating the process (via log.Fatalf) if a required
// variable is missing.
func Load() Config {
	cfg := Config{
		NatsURL:          envconfig.NatsURL(),
		TursoDatabaseURL: envconfig.Required("TURSO_DATABASE_URL"),
		TursoAuthToken:   envconfig.Required("TURSO_AUTH_TOKEN"),
		UserAgent:        envconfig.String("USER_AGENT", "ticket-live-event-scanner/0.1 (personal project; contact: jfms7s@gmail.com)"),
		RequestDelayMS:   envconfig.Int("REQUEST_DELAY_MS", 1500),
		HubPages:         hubPagesFromEnv("TICKETLINE_HUB_PAGES", defaultHubPages),
	}
	return cfg
}
