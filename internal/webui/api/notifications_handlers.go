package api

import (
	"log"
	"net/http"

	"github.com/jfms7s/ticket-live-event-scanner/internal/webui/repository"
)

// handleListNotifications returns all notifications, optionally filtered by status.
func (app *App) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")

	// Validate status parameter
	if status != "" && status != "pending" && status != "sent" && status != "failed" {
		writeError(w, http.StatusBadRequest, "status must be 'pending', 'sent', or 'failed'")
		return
	}

	notifs, err := repository.ListNotifications(r.Context(), app.db, status)
	if err != nil {
		log.Printf("Error listing notifications: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, notifs)
}
