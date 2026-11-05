// Package config loads scraper configuration from environment variables.
package config

import (
	"github.com/jfms7s/ticket-live-event-scanner/internal/envconfig"
)

// Config holds the scraper's runtime configuration.
type Config struct {
	UserAgent      string
	RequestDelayMS int
	// HubPages is TICKETLINE_HUB_PAGES, which only seeds the watches table
	// (see package watch); the run scrapes watch.Resolve's result.
	HubPages []string
}

// defaultHubPages are the hub pages seeded when TICKETLINE_HUB_PAGES is not
// set. It is empty on purpose: there is no sensible default. A run with no
// enabled watch fails (see package job).
var defaultHubPages = []string{}

// Load builds a Config from environment variables, applying defaults where
// applicable. web-ui-api requires the database variables itself; a malformed
// REQUEST_DELAY_MS is returned as an error so it disables the scraper
// instead of stopping the process.
func Load() (Config, error) {
	delay, err := envconfig.IntErr("REQUEST_DELAY_MS", 1500)
	if err != nil {
		return Config{}, err
	}
	return Config{
		UserAgent:      envconfig.String("USER_AGENT", "ticket-live-event-scanner/0.1 (personal project; contact: jfms7s@gmail.com)"),
		RequestDelayMS: delay,
		HubPages:       hubPagesFromEnv("TICKETLINE_HUB_PAGES", defaultHubPages),
	}, nil
}
