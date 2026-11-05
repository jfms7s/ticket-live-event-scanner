package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/webui/repository"
)

// handleListEvents returns all events, optionally filtered by status (active/finished).
func (app *App) handleListEvents(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")

	// Validate status parameter
	if status != "" && status != "active" && status != "finished" {
		writeError(w, http.StatusBadRequest, "status must be 'active' or 'finished'")
		return
	}

	events, err := repository.ListEvents(r.Context(), app.db, status, time.Now())
	if err != nil {
		log.Printf("Error listing events: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, events)
}

// handleGetEvent returns a single event by ID.
func (app *App) handleGetEvent(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	eventResp, err := repository.GetEvent(r.Context(), app.db, id, time.Now())
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		log.Printf("Error getting event %d: %v", id, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, eventResp)
}

// handleRetrigger queues another Telegram notification for the event (the
// dashboard's Retry). The earlier notifications stay as the history.
func (app *App) handleRetrigger(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	// Detached from the client's connection: the notification is queued
	// even if the client goes away mid-request.
	queueCtx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancel()
	err := app.queue.EnqueueRetry(queueCtx, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		log.Printf("Error queueing the retry of event %d: %v", id, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{
		"event_id": id,
		"status":   "pending",
	})
}

// maxPurchasedBodyBytes caps the handleSetPurchased body; the real one is
// about 20 bytes.
const maxPurchasedBodyBytes = 1024

// setPurchasedRequest is the JSON body for handleSetPurchased.
type setPurchasedRequest struct {
	Purchased bool `json:"purchased"`
}

// handleSetPurchased marks an event as purchased or not purchased.
func (app *App) handleSetPurchased(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	var body setPurchasedRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxPurchasedBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	changed, err := repository.SetEventPurchased(r.Context(), app.db, id, body.Purchased)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		log.Printf("Error setting purchased for event %d: %v", id, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Only a real false-to-true change queues an invite (in the same
	// transaction as the flag): ticking an event that is already purchased
	// (a double submit or a client retry) is a no-op. Unticking and ticking
	// again is how a user re-sends one.
	if body.Purchased && changed {
		app.queue.Wake()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"event_id":  id,
		"purchased": body.Purchased,
	})
}

// handleDeleteEvent is the dashboard's Remove: it hides the event (see
// repository.HideEvent) rather than deleting the row.
func (app *App) handleDeleteEvent(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	err := repository.HideEvent(r.Context(), app.db, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		log.Printf("Error hiding event %d: %v", id, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
