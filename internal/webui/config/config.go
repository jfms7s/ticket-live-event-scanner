// Package config loads web-ui-api configuration from environment variables.
package config

import "github.com/jfms7s/ticket-live-event-scanner/internal/envconfig"

// Config holds web-ui-api's runtime configuration.
type Config struct {
	// DatabasePath is DATABASE_PATH, the SQLite file.
	DatabasePath string
	// DatabaseAllowEmpty is DATABASE_ALLOW_EMPTY=true: start on an empty
	// SQLite file (a fresh install). Otherwise that start is refused (see
	// store.Options.AllowEmpty).
	DatabaseAllowEmpty bool
	ListenAddr         string
	// CORSOrigin is the dashboard origin allowed to call the API; empty
	// (the default) sends no CORS headers.
	CORSOrigin string
}

// Load builds a Config from environment variables, terminating the process
// (via log.Fatalf) if a required variable is missing.
func Load() Config {
	return Config{
		DatabasePath:       envconfig.Required("DATABASE_PATH"),
		DatabaseAllowEmpty: envconfig.Bool("DATABASE_ALLOW_EMPTY"),
		ListenAddr:         envconfig.String("LISTEN_ADDR", ":8080"),
		CORSOrigin:         envconfig.String("CORS_ORIGIN", ""),
	}
}
