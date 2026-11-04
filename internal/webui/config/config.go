// Package config loads web-ui-api configuration from environment variables.
package config

import "github.com/jfms7s/ticket-live-event-scanner/internal/envconfig"

// Config holds web-ui-api's runtime configuration.
type Config struct {
	NatsURL string
	// DatabasePath is DATABASE_PATH, the SQLite file. When it is set,
	// TURSO_DATABASE_URL / TURSO_AUTH_TOKEN are optional and name an sqld
	// to import from once, into a new file (see store.Open). When it is
	// unset, the database is that sqld, as before.
	DatabasePath     string
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
	path := envconfig.String("DATABASE_PATH", "")
	tursoURL, tursoToken := envconfig.String("TURSO_DATABASE_URL", ""), envconfig.String("TURSO_AUTH_TOKEN", "")
	if path == "" {
		tursoURL, tursoToken = envconfig.Required("TURSO_DATABASE_URL"), envconfig.Required("TURSO_AUTH_TOKEN")
	}
	return Config{
		NatsURL:          envconfig.NatsURL(),
		DatabasePath:     path,
		TursoDatabaseURL: tursoURL,
		TursoAuthToken:   tursoToken,
		ListenAddr:       envconfig.String("LISTEN_ADDR", ":8080"),
		CORSOrigin:       envconfig.String("CORS_ORIGIN", ""),
	}
}
