package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/nats-io/nats.go/jetstream"
)

// Publisher is an interface for publishing messages to JetStream.
type Publisher interface {
	Publish(ctx context.Context, subject string, data []byte, opts ...jetstream.PublishOpt) (*jetstream.PubAck, error)
}

// App holds dependencies for HTTP handlers.
type App struct {
	db *sql.DB
	js Publisher
}

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

// handleHealthz returns a simple health check response.
func (app *App) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "\"ok\"\n")
}

// handleListEvents returns all events, optionally filtered by status (active/finished).
func (app *App) handleListEvents(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")

	// Validate status parameter
	if status != "" && status != "active" && status != "finished" {
		http.Error(w, `{"error":"status must be 'active' or 'finished'"}`, http.StatusBadRequest)
		return
	}

	events, err := listEvents(r.Context(), app.db, status)
	if err != nil {
		log.Printf("Error listing events: %v", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(events)
}

// handleGetEvent returns a single event by ID.
func (app *App) handleGetEvent(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid event ID"}`, http.StatusBadRequest)
		return
	}

	eventResp, err := getEvent(r.Context(), app.db, id)
	if err == sql.ErrNoRows {
		http.Error(w, `{"error":"event not found"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		log.Printf("Error getting event %d: %v", id, err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(eventResp)
}

// handleRetrigger re-publishes an event to trigger a retry notification.
func (app *App) handleRetrigger(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid event ID"}`, http.StatusBadRequest)
		return
	}

	// Get the event from database
	eventResp, err := getEvent(r.Context(), app.db, id)
	if err == sql.ErrNoRows {
		http.Error(w, `{"error":"event not found"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		log.Printf("Error getting event %d for retrigger: %v", id, err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	// Build Discovered message from stored event
	// Note: derefString converts nil EventDate to "" (empty string), which is handled
	// gracefully by telegram-notifier's message template (simply omits the date line).
	disc := &event.Discovered{
		EventID:   eventResp.ID,
		Slug:      eventResp.Slug,
		Title:     eventResp.Title,
		Venue:     derefString(eventResp.Venue),
		Category:  derefString(eventResp.Category),
		EventDate: derefString(eventResp.EventDate),
		URL:       eventResp.URL,
		ImageURL:  derefString(eventResp.ImageURL),
		Reason:    event.ReasonManualRetry,
	}

	discBytes, err := json.Marshal(disc)
	if err != nil {
		log.Printf("Error marshaling retrigger payload for event %d: %v", id, err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	// Publish to NATS with a unique message ID (nano precision) to avoid deduplication.
	// Use a detached, short-lived context to decouple the publish from the client's
	// connection lifecycle, avoiding double-publishing if the client disconnects mid-request.
	publishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	msgID := fmt.Sprintf("retry-%d-%d", id, time.Now().UnixNano())
	_, err = app.js.Publish(publishCtx, streams.EventsSubject,
		discBytes,
		jetstream.WithMsgID(msgID),
	)
	if err != nil {
		log.Printf("Error publishing retrigger for event %d: %v", id, err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"event_id": id,
		"status":   "pending",
	})
}

// handleListNotifications returns all notifications, optionally filtered by status.
func (app *App) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")

	// Validate status parameter
	if status != "" && status != "pending" && status != "sent" && status != "failed" {
		http.Error(w, `{"error":"status must be 'pending', 'sent', or 'failed'"}`, http.StatusBadRequest)
		return
	}

	notifs, err := listNotifications(r.Context(), app.db, status)
	if err != nil {
		log.Printf("Error listing notifications: %v", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(notifs)
}

// Helper functions

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func refString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
