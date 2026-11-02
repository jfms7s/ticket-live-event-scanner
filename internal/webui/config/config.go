// Package config loads web-ui-api configuration from environment variables.
package config

import "github.com/jfms7s/ticket-live-event-scanner/internal/envconfig"

// Config holds web-ui-api's runtime configuration.
type Config struct {
	NatsURL          string
	TursoDatabaseURL string
	TursoAuthToken   string
	ListenAddr       string
	// CORSOrigin is the dashboard origin allowed to call the API; empty
	// (the default) sends no CORS headers.
	CORSOrigin string
}

// Load builds a Config from environment variables, terminating the process
// (via log.Fatalf) if a required variable is missing.
func Load() Config {
	return Config{
		NatsURL:          envconfig.NatsURL(),
		TursoDatabaseURL: envconfig.Required("TURSO_DATABASE_URL"),
		TursoAuthToken:   envconfig.Required("TURSO_AUTH_TOKEN"),
		ListenAddr:       envconfig.String("LISTEN_ADDR", ":8080"),
		CORSOrigin:       envconfig.String("CORS_ORIGIN", ""),
	}
}
