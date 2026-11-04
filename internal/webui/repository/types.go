// Package repository provides database access for the web-ui-api service:
// materializing discovered events, tracking notification status, and
// serving the queries behind the HTTP API.
package repository

// EventResponse represents an event in the JSON API response.
type EventResponse struct {
	ID        int64   `json:"id"`
	Slug      string  `json:"slug"`
	Title     string  `json:"title"`
	Venue     *string `json:"venue"`
	Category  *string `json:"category"`
	EventDate *string `json:"event_date"`
	URL       string  `json:"url"`
	ImageURL  *string `json:"image_url"`
	// HubSlug is the hub page that found the event; nil for events stored
	// before hubs were recorded.
	HubSlug       *string                       `json:"hub_slug"`
	DiscoveredAt  string                        `json:"discovered_at"`
	Purchased     bool                          `json:"purchased"`
	Status        string                        `json:"status"`
	Notifications []NotificationInEventResponse `json:"notifications"`
	// InviteStatus is the calendar-invite email's state: "pending" once the
	// purchase is recorded, then "sent" or "failed" from email-notifier's
	// result. Nil for an event never marked purchased, or purchased before
	// invites were tracked.
	InviteStatus *string `json:"invite_status"`
	// InviteError is the failure reason when InviteStatus is "failed".
	InviteError *string `json:"invite_error"`
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

// NotificationResponse is a standalone notification in the JSON API
// response: the nested shape plus event_id. encoding/json flattens the
// embedded struct, so its fields appear at the top level.
type NotificationResponse struct {
	NotificationInEventResponse
	EventID int64 `json:"event_id"`
}
