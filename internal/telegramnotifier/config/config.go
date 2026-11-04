// Package config loads the Telegram notifier's configuration from
// environment variables.
package config

import (
	"log"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/envconfig"
	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/handler"
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
// is unset it returns those variables' names and the notifier is disabled
// (the rest of the process runs without it). An invalid
// TELEGRAM_MIN_SEND_INTERVAL stops the process (via log.Fatal).
func Load() (cfg Config, missing []string) {
	if missing = envconfig.Unset(required...); len(missing) > 0 {
		return Config{}, missing
	}
	minSendInterval, err := handler.ParseMinSendInterval(envconfig.String("TELEGRAM_MIN_SEND_INTERVAL", ""))
	if err != nil {
		log.Fatal(err)
	}
	return Config{
		BotToken:        envconfig.Required("TELEGRAM_BOT_TOKEN"),
		ChatID:          envconfig.Required("TELEGRAM_CHAT_ID"),
		MinSendInterval: minSendInterval,
	}, nil
}
