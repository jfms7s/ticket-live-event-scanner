// Package config loads email-notifier configuration from environment
// variables.
package config

import "github.com/jfms7s/ticket-live-event-scanner/internal/envconfig"

// Config holds the email-notifier's runtime configuration.
type Config struct {
	NatsURL      string
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	// SMTPAllowPlaintext lets the client send without TLS when the server
	// doesn't offer STARTTLS (SMTP_ALLOW_PLAINTEXT=true or 1). Local
	// testing only; off by default.
	SMTPAllowPlaintext bool
	EmailFrom          string
	EmailTo            []string
}

// Load builds a Config from environment variables, applying defaults where
// applicable and terminating the process (via log.Fatalf) if a required
// variable is missing.
func Load() Config {
	return Config{
		NatsURL:            envconfig.NatsURL(),
		SMTPHost:           envconfig.Required("SMTP_HOST"),
		SMTPPort:           envconfig.Int("SMTP_PORT", 587),
		SMTPUsername:       envconfig.Required("SMTP_USERNAME"),
		SMTPPassword:       envconfig.Required("SMTP_PASSWORD"),
		SMTPAllowPlaintext: envconfig.Bool("SMTP_ALLOW_PLAINTEXT"),
		EmailFrom:          envconfig.Required("EMAIL_FROM"),
		EmailTo:            envconfig.List("EMAIL_TO"),
	}
}
