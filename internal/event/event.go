// Package event defines the shared message schemas published to and
// consumed from NATS JetStream, per docs/design.md §8. All three services
// (scraper, telegram-notifier, web-ui-api) import this package so the wire
// format stays in sync without needing to hand-copy struct definitions.
package event

import "time"

// Reason values for Discovered.Reason.
const (
	ReasonDiscovered  = "discovered"
	ReasonManualRetry = "manual-retry"
)

// Discovered is published to subject "events.discovered" by the scraper
// (Reason=ReasonDiscovered) and re-published by web-ui-api on manual
// retrigger (Reason=ReasonManualRetry).
type Discovered struct {
	EventID   int64  `json:"event_id"`
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Venue     string `json:"venue,omitempty"`
	Category  string `json:"category,omitempty"`
	EventDate string `json:"event_date,omitempty"` // YYYY-MM-DD or YYYY-MM-DDTHH:MM
	URL       string `json:"url"`
	ImageURL  string `json:"image_url,omitempty"`
	Reason    string `json:"reason"`
}

// NotificationSent is published to subject "notifications.sent" by
// telegram-notifier after Telegram confirms delivery (HTTP 2xx).
type NotificationSent struct {
	EventID           int64     `json:"event_id"`
	TelegramMessageID string    `json:"telegram_message_id"`
	SentAt            time.Time `json:"sent_at"`
}

// NotificationFailed is published to subject "notifications.failed" by
// telegram-notifier once redelivery attempts are exhausted.
type NotificationFailed struct {
	EventID  int64     `json:"event_id"`
	Error    string    `json:"error"`
	FailedAt time.Time `json:"failed_at"`
	Attempts int       `json:"attempts"`
}
