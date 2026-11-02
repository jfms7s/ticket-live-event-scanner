// Package config loads scraper configuration from environment variables.
package config

import (
	"errors"
	"log"

	"github.com/jfms7s/ticket-live-event-scanner/internal/envconfig"
)

// Config holds the scraper's runtime configuration.
type Config struct {
	NatsURL          string
	TursoDatabaseURL string
	TursoAuthToken   string
	UserAgent        string
	RequestDelayMS   int
	HubPages         []string
}

// defaultHubPages are the venue/series hub pages tracked when
// TICKETLINE_HUB_PAGES is not set (the project design doc (kept outside this repo), §6.1).
// It is empty on purpose: there is no sensible default, and an empty list
// is a startup error (see requireHubPages).
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
	if err := requireHubPages(cfg.HubPages); err != nil {
		log.Fatal(err)
	}
	return cfg
}

// requireHubPages rejects an empty hub page list: a run with nothing to
// scrape would succeed while discovering nothing.
func requireHubPages(pages []string) error {
	if len(pages) == 0 {
		return errors.New("TICKETLINE_HUB_PAGES is empty or has no usable entry: set it to a comma-separated list of ticketline.pt hub page URLs or slugs")
	}
	return nil
}
