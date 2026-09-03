// Package config loads email-notifier configuration from environment
// variables.
package config

// Config holds the email-notifier's runtime configuration.
type Config struct {
	NatsURL      string
	SMTPHost     string
	SMTPPort     int
	SMTPUsername string
	SMTPPassword string
	EmailFrom    string
	EmailTo      []string
}

// Load builds a Config from environment variables, applying defaults where
// applicable and terminating the process (via log.Fatalf) if a required
// variable is missing.
func Load() Config {
	return Config{
		NatsURL:      getEnv("NATS_URL", "nats://localhost:4222"),
		SMTPHost:     getEnvRequired("SMTP_HOST"),
		SMTPPort:     getEnvInt("SMTP_PORT", 587),
		SMTPUsername: getEnvRequired("SMTP_USERNAME"),
		SMTPPassword: getEnvRequired("SMTP_PASSWORD"),
		EmailFrom:    getEnvRequired("EMAIL_FROM"),
		EmailTo:      getEnvList("EMAIL_TO"),
	}
}
