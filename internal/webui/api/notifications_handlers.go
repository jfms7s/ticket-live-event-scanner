package api

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/jfms7s/ticket-live-event-scanner/internal/webui/repository"
)

// handleListNotifications returns all notifications, optionally filtered by status.
func (app *App) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")

	// Validate status parameter
	if status != "" && status != "pending" && status != "sent" && status != "failed" {
		http.Error(w, `{"error":"status must be 'pending', 'sent', or 'failed'"}`, http.StatusBadRequest)
		return
	}

	notifs, err := repository.ListNotifications(r.Context(), app.db, status)
	if err != nil {
		log.Printf("Error listing notifications: %v", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(notifs); err != nil {
		log.Printf("Error encoding notifications response: %v", err)
	}
}
