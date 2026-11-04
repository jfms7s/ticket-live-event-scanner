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
// is unset it returns those variables' names, and when a value is malformed
// (SMTP_PORT not a number, EMAIL_TO with no address) it returns the error;
// either way the notifier is disabled and the rest of the process runs
// without it.
func Load() (cfg Config, missing []string, err error) {
	if missing = envconfig.Unset(required...); len(missing) > 0 {
		return Config{}, missing, nil
	}
	port, err := envconfig.IntErr("SMTP_PORT", 587)
	if err != nil {
		return Config{}, nil, err
	}
	to, err := envconfig.ListErr("EMAIL_TO")
	if err != nil {
		return Config{}, nil, err
	}
	return Config{
		SMTPHost:           envconfig.String("SMTP_HOST", ""),
		SMTPPort:           port,
		SMTPUsername:       envconfig.String("SMTP_USERNAME", ""),
		SMTPPassword:       envconfig.String("SMTP_PASSWORD", ""),
		SMTPAllowPlaintext: envconfig.Bool("SMTP_ALLOW_PLAINTEXT"),
		EmailFrom:          envconfig.String("EMAIL_FROM", ""),
		EmailTo:            to,
	}, nil, nil
}
