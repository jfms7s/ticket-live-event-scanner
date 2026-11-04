// Package event defines the shared message schemas published to and
// consumed from NATS JetStream, per the project design doc (kept outside this repo), §8.
// Both Go services import this package so the wire format stays in sync
// without hand-copied struct definitions:
//   - scraper publishes events.discovered.
//   - the Telegram channel (web-ui-api) consumes events.discovered and publishes
//     notifications.sent / notifications.failed.
//   - the email channel (web-ui-api) consumes events.purchased and publishes
//     notifications.email.sent / notifications.email.failed.
//   - web-ui-api's weekly digest job publishes scraper.digest; the Telegram channel sends it.
//   - web-ui-api consumes events.discovered and every notifications.*
//     subject, and publishes events.discovered (manual retrigger) and
//     events.purchased.
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
	// HubSlug is the tracked hub page that listed this session (empty on a
	// manual retry of an event stored before hubs were recorded).
	HubSlug string `json:"hub_slug,omitempty"`
}

// NotificationSent is published to subject "notifications.sent" by
// the Telegram channel after Telegram confirms delivery (HTTP 2xx).
type NotificationSent struct {
	EventID           int64     `json:"event_id"`
	TelegramMessageID string    `json:"telegram_message_id"`
	SentAt            time.Time `json:"sent_at"`
	// DeliverySeq is the EVENTS stream sequence of the events.discovered
	// message this result is for; web-ui-api matches it to the row it made
	// for that message. 0 (omitted) means unknown, and web-ui-api falls back
	// to the event's latest pending row.
	DeliverySeq uint64 `json:"delivery_seq,omitempty"`
}

// NotificationFailed is published to subject "notifications.failed" by
// the Telegram channel once redelivery attempts are exhausted.
type NotificationFailed struct {
	EventID  int64     `json:"event_id"`
	Error    string    `json:"error"`
	FailedAt time.Time `json:"failed_at"`
	Attempts int       `json:"attempts"`
	// DeliverySeq: as on NotificationSent.
	DeliverySeq uint64 `json:"delivery_seq,omitempty"`
}

// Purchased is published to subject "events.purchased" by web-ui-api when
// an event is marked purchased via PATCH /api/events/{id}/purchased.
type Purchased struct {
	EventID     int64     `json:"event_id"`
	Slug        string    `json:"slug"`
	Title       string    `json:"title"`
	Venue       string    `json:"venue,omitempty"`
	Category    string    `json:"category,omitempty"`
	EventDate   string    `json:"event_date,omitempty"` // YYYY-MM-DD or YYYY-MM-DDTHH:MM
	URL         string    `json:"url"`
	ImageURL    string    `json:"image_url,omitempty"`
	PurchasedAt time.Time `json:"purchased_at"`
}

// EmailSent is published to subject "notifications.email.sent" by
// the email channel after the calendar-invite email is accepted by the SMTP
// server.
type EmailSent struct {
	EventID   int64     `json:"event_id"`
	MessageID string    `json:"message_id"`
	SentAt    time.Time `json:"sent_at"`
}

// EmailFailed is published to subject "notifications.email.failed" by
// the email channel once redelivery attempts are exhausted.
type EmailFailed struct {
	EventID  int64     `json:"event_id"`
	Error    string    `json:"error"`
	FailedAt time.Time `json:"failed_at"`
	Attempts int       `json:"attempts"`
}

// Digest is published to subject "scraper.digest" by the weekly digest job
// once a week and sent by the Telegram channel as is.
type Digest struct {
	// Week is the ISO week it covers, e.g. "2026-W45"; it is also the
	// message ID, so a second publish for the same week is dropped.
	Week string `json:"week"`
	// Text is the message in Telegram HTML, escaped and under 4096
	// characters.
	Text string `json:"text"`
	// GeneratedAt is when it was built; the Telegram channel drops a digest
	// too old to be news (held while it was down).
	GeneratedAt time.Time `json:"generated_at"`
}
