// Package config loads the email notifier's configuration from environment
// variables.
package config

import "github.com/jfms7s/ticket-live-event-scanner/internal/envconfig"

// Config holds the email notifier's runtime configuration.
type Config struct {
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

// required are the variables without which the notifier cannot send.
var required = []string{"SMTP_HOST", "SMTP_USERNAME", "SMTP_PASSWORD", "EMAIL_FROM", "EMAIL_TO"}

// Load builds a Config from environment variables. When a required variable
// is unset it returns those variables' names and the notifier is disabled
// (the rest of the process runs without it). A malformed SMTP_PORT or an
// EMAIL_TO with no address stops the process (via log.Fatalf).
func Load() (cfg Config, missing []string) {
	if missing = envconfig.Unset(required...); len(missing) > 0 {
		return Config{}, missing
	}
	return Config{
		SMTPHost:           envconfig.Required("SMTP_HOST"),
		SMTPPort:           envconfig.Int("SMTP_PORT", 587),
		SMTPUsername:       envconfig.Required("SMTP_USERNAME"),
		SMTPPassword:       envconfig.Required("SMTP_PASSWORD"),
		SMTPAllowPlaintext: envconfig.Bool("SMTP_ALLOW_PLAINTEXT"),
		EmailFrom:          envconfig.Required("EMAIL_FROM"),
		EmailTo:            envconfig.List("EMAIL_TO"),
	}, nil
}
