package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
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

// handleRetrigger re-publishes an event to trigger a retry notification.
func (app *App) handleRetrigger(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}

	// Get the event from database
	eventResp, err := repository.GetEvent(r.Context(), app.db, id, time.Now())
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "event not found")
		return
	}
	if err != nil {
		log.Printf("Error getting event %d for retrigger: %v", id, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	disc := storedEventFrom(eventResp).discovered(event.ReasonManualRetry)

	discBytes, err := json.Marshal(disc)
	if err != nil {
		log.Printf("Error marshaling retrigger payload for event %d: %v", id, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
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

	// Only a real false-to-true change sends an invite: ticking an event
	// that is already purchased (a double submit or a client retry) is a
	// no-op. Unticking and ticking again is how a user re-sends one.
	if body.Purchased && changed {
		app.publishPurchased(r.Context(), id)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"event_id":  id,
		"purchased": body.Purchased,
	})
}

// publishPurchased publishes an event.Purchased message for id so
// email-notifier can send a calendar-invite email. Errors are logged but
// do not fail the purchased-flag request, since the flag itself was
// already persisted successfully. When the message can't be published, the
// invite SetEventPurchased left pending is marked failed instead, because
// no result for it will ever arrive.
func (app *App) publishPurchased(reqCtx context.Context, id int64) {
	eventResp, err := repository.GetEvent(reqCtx, app.db, id, time.Now())
	if err != nil {
		log.Printf("Error getting event %d for purchased publish: %v", id, err)
		app.markInviteNotQueued(id, err)
		return
	}

	purchased := storedEventFrom(eventResp).purchased(time.Now().UTC())

	purchasedBytes, err := json.Marshal(purchased)
	if err != nil {
		log.Printf("Error marshaling purchased payload for event %d: %v", id, err)
		app.markInviteNotQueued(id, err)
		return
	}

	// Detached, short-lived context so the publish isn't tied to the
	// client's connection lifecycle, matching handleRetrigger's approach.
	publishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	msgID := fmt.Sprintf("purchased-%d-%d", id, time.Now().UnixNano())
	if _, err := app.js.Publish(publishCtx, streams.PurchasedSubject,
		purchasedBytes,
		jetstream.WithMsgID(msgID),
	); err != nil {
		log.Printf("Error publishing purchased event for event %d: %v", id, err)
		app.markInviteNotQueued(id, err)
	}
}

// markInviteNotQueued records that the invite request for id never reached
// NATS. It uses its own short context, like the publish, so a client that
// disconnects doesn't leave the invite pending.
func (app *App) markInviteNotQueued(id int64, cause error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := repository.UpdateInviteFailed(ctx, app.db, id, "invite not queued: "+cause.Error()); err != nil {
		log.Printf("Error marking invite failed for event %d: %v", id, err)
	}
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
