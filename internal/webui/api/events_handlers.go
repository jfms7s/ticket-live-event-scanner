package api

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
	"github.com/jfms7s/ticket-live-event-scanner/internal/webui/repository"
	"github.com/nats-io/nats.go/jetstream"
)

// handleListEvents returns all events, optionally filtered by status (active/finished).
func (app *App) handleListEvents(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")

	// Validate status parameter
	if status != "" && status != "active" && status != "finished" {
		http.Error(w, `{"error":"status must be 'active' or 'finished'"}`, http.StatusBadRequest)
		return
	}

	events, err := repository.ListEvents(r.Context(), app.db, status)
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

	eventResp, err := repository.GetEvent(r.Context(), app.db, id)
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
	eventResp, err := repository.GetEvent(r.Context(), app.db, id)
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

// setPurchasedRequest is the JSON body for handleSetPurchased.
type setPurchasedRequest struct {
	Purchased bool `json:"purchased"`
}

// handleSetPurchased marks an event as purchased or not purchased.
func (app *App) handleSetPurchased(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid event ID"}`, http.StatusBadRequest)
		return
	}

	var body setPurchasedRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}

	err = repository.SetEventPurchased(r.Context(), app.db, id, body.Purchased)
	if err == sql.ErrNoRows {
		http.Error(w, `{"error":"event not found"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		log.Printf("Error setting purchased for event %d: %v", id, err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"event_id":  id,
		"purchased": body.Purchased,
	})
}

// handleDeleteEvent removes an event and its notifications from the database.
func (app *App) handleDeleteEvent(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid event ID"}`, http.StatusBadRequest)
		return
	}

	err = repository.DeleteEvent(r.Context(), app.db, id)
	if err == sql.ErrNoRows {
		http.Error(w, `{"error":"event not found"}`, http.StatusNotFound)
		return
	}
	if err != nil {
		log.Printf("Error deleting event %d: %v", id, err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
