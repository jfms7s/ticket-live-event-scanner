// Package config loads telegram-notifier configuration from environment
// variables.
package config

import (
	"log"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/envconfig"
	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/handler"
)

// Config holds telegram-notifier's runtime configuration.
type Config struct {
	NatsURL  string
	BotToken string
	ChatID   string
	// MinSendInterval is TELEGRAM_MIN_SEND_INTERVAL (Go duration, default
	// 3s, 0 = off); an invalid value stops the process.
	MinSendInterval time.Duration
}

// Load builds a Config from environment variables, terminating the process
// (via log.Fatal) if a required variable is missing or invalid.
func Load() Config {
	minSendInterval, err := handler.ParseMinSendInterval(envconfig.String("TELEGRAM_MIN_SEND_INTERVAL", ""))
	if err != nil {
		log.Fatal(err)
	}
	return Config{
		NatsURL:         envconfig.NatsURL(),
		BotToken:        envconfig.Required("TELEGRAM_BOT_TOKEN"),
		ChatID:          envconfig.Required("TELEGRAM_CHAT_ID"),
		MinSendInterval: minSendInterval,
	}
}
