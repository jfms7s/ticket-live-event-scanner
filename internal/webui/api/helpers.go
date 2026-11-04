package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/webui/repository"
)

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// writeJSON writes v as a JSON response with the given status. An encode
// error can only be logged: the status line has already been sent.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("Error encoding %d response: %v", status, err)
	}
}

// errorResponse is the body of every API error.
type errorResponse struct {
	Error string `json:"error"`
}

// writeError writes {"error": msg} as JSON with the given status.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}

// pathID parses the {id} path value. On failure it writes the 400 itself
// and returns false.
func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid event ID")
		return 0, false
	}
	return id, true
}

// storedEvent holds the fields event.Discovered and event.Purchased share,
// taken from a stored event. derefString turns a nil optional field into
// "", which the notifiers treat as absent (the date line is just omitted).
type storedEvent struct {
	EventID                              int64
	Slug, Title, URL                     string
	Venue, Category, EventDate, ImageURL string
	HubSlug                              string
}

func storedEventFrom(e *repository.EventResponse) storedEvent {
	return storedEvent{
		EventID:   e.ID,
		Slug:      e.Slug,
		Title:     e.Title,
		URL:       e.URL,
		Venue:     derefString(e.Venue),
		Category:  derefString(e.Category),
		EventDate: derefString(e.EventDate),
		ImageURL:  derefString(e.ImageURL),
		HubSlug:   derefString(e.HubSlug),
	}
}

// discovered builds a Discovered message with the given reason.
func (s storedEvent) discovered(reason string) *event.Discovered {
	return &event.Discovered{
		EventID:   s.EventID,
		Slug:      s.Slug,
		Title:     s.Title,
		Venue:     s.Venue,
		Category:  s.Category,
		EventDate: s.EventDate,
		URL:       s.URL,
		ImageURL:  s.ImageURL,
		Reason:    reason,
		HubSlug:   s.HubSlug,
	}
}

// purchased builds a Purchased message stamped with at.
func (s storedEvent) purchased(at time.Time) *event.Purchased {
	return &event.Purchased{
		EventID:     s.EventID,
		Slug:        s.Slug,
		Title:       s.Title,
		Venue:       s.Venue,
		Category:    s.Category,
		EventDate:   s.EventDate,
		URL:         s.URL,
		ImageURL:    s.ImageURL,
		PurchasedAt: at,
	}
}
