// Package config loads scraper configuration from environment variables.
package config

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
// TICKETLINE_HUB_PAGES is not set (design.md §6.1).
var defaultHubPages = []string{}

// Load builds a Config from environment variables, applying defaults where
// applicable and terminating the process (via log.Fatalf) if a required
// variable is missing.
func Load() Config {
	return Config{
		NatsURL:          getEnv("NATS_URL", "nats://localhost:4222"),
		TursoDatabaseURL: getEnvRequired("TURSO_DATABASE_URL"),
		TursoAuthToken:   getEnvRequired("TURSO_AUTH_TOKEN"),
		UserAgent:        getEnv("USER_AGENT", "ticket-live-event-scanner/0.1 (personal project; contact: jfms7s@gmail.com)"),
		RequestDelayMS:   getEnvInt("REQUEST_DELAY_MS", 1500),
		HubPages:         getEnvHubPages("TICKETLINE_HUB_PAGES", defaultHubPages),
	}
}
