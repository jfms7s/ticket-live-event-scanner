// Package config loads the Telegram notifier's configuration from
// environment variables.
package config

import (
	"fmt"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/envconfig"
)

// Config holds the Telegram notifier's runtime configuration.
type Config struct {
	BotToken string
	ChatID   string
	// MinSendInterval is TELEGRAM_MIN_SEND_INTERVAL (Go duration, default
	// 3s, 0 = off); an invalid value stops the process.
	MinSendInterval time.Duration
}

// required are the variables without which the notifier cannot send.
var required = []string{"TELEGRAM_BOT_TOKEN", "TELEGRAM_CHAT_ID"}

// Load builds a Config from environment variables. When a required variable
// is unset it returns those variables' names, and when a value is invalid
// (TELEGRAM_MIN_SEND_INTERVAL) it returns the error; either way the
// notifier is disabled and the rest of the process runs without it.
func Load() (cfg Config, missing []string, err error) {
	if missing = envconfig.Unset(required...); len(missing) > 0 {
		return Config{}, missing, nil
	}
	minSendInterval, err := ParseMinSendInterval(envconfig.String("TELEGRAM_MIN_SEND_INTERVAL", ""))
	if err != nil {
		return Config{}, nil, err
	}
	return Config{
		BotToken:        envconfig.String("TELEGRAM_BOT_TOKEN", ""),
		ChatID:          envconfig.String("TELEGRAM_CHAT_ID", ""),
		MinSendInterval: minSendInterval,
	}, nil, nil
}

// DefaultMinSendInterval keeps sends under Telegram's limit of about 20
// messages a minute to one group.
const DefaultMinSendInterval = 3 * time.Second

// ParseMinSendInterval reads TELEGRAM_MIN_SEND_INTERVAL: a Go duration,
// DefaultMinSendInterval when empty, 0 to turn pacing off.
func ParseMinSendInterval(v string) (time.Duration, error) {
	if v == "" {
		return DefaultMinSendInterval, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("TELEGRAM_MIN_SEND_INTERVAL: %w", err)
	}
	if d < 0 {
		return 0, fmt.Errorf("TELEGRAM_MIN_SEND_INTERVAL: %v is negative", d)
	}
	return d, nil
}
