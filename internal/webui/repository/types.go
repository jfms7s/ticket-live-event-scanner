// Package repository provides database access for the web-ui-api service:
// materializing discovered events, tracking notification status, and
// serving the queries behind the HTTP API.
package repository

// EventResponse represents an event in the JSON API response.
type EventResponse struct {
	ID            int64                         `json:"id"`
	Slug          string                        `json:"slug"`
	Title         string                        `json:"title"`
	Venue         *string                       `json:"venue"`
	Category      *string                       `json:"category"`
	EventDate     *string                       `json:"event_date"`
	URL           string                        `json:"url"`
	ImageURL      *string                       `json:"image_url"`
	DiscoveredAt  string                        `json:"discovered_at"`
	Purchased     bool                          `json:"purchased"`
	Status        string                        `json:"status"`
	Notifications []NotificationInEventResponse `json:"notifications"`
}

// NotificationInEventResponse represents a notification nested within an Event (no event_id field).
type NotificationInEventResponse struct {
	ID                int64   `json:"id"`
	Status            string  `json:"status"`
	TelegramMessageID *string `json:"telegram_message_id"`
	AttemptedAt       string  `json:"attempted_at"`
	ConfirmedAt       *string `json:"confirmed_at"`
	Error             *string `json:"error"`
	TriggeredBy       string  `json:"triggered_by"`
}

// NotificationResponse represents a standalone notification in the JSON API response (includes event_id).
type NotificationResponse struct {
	ID                int64   `json:"id"`
	EventID           int64   `json:"event_id"`
	Status            string  `json:"status"`
	TelegramMessageID *string `json:"telegram_message_id"`
	AttemptedAt       string  `json:"attempted_at"`
	ConfirmedAt       *string `json:"confirmed_at"`
	Error             *string `json:"error"`
	TriggeredBy       string  `json:"triggered_by"`
}
