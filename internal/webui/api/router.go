package api

import "net/http"

// NewMux builds the HTTP mux with all routes registered.
func (app *App) NewMux() *http.ServeMux {
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("GET /healthz", app.handleHealthz)

	// Events API
	mux.HandleFunc("GET /api/events", app.handleListEvents)
	mux.HandleFunc("GET /api/events/{id}", app.handleGetEvent)
	mux.HandleFunc("POST /api/events/{id}/retrigger", app.handleRetrigger)
	mux.HandleFunc("PATCH /api/events/{id}/purchased", app.handleSetPurchased)
	mux.HandleFunc("DELETE /api/events/{id}", app.handleDeleteEvent)

	// Notifications API
	mux.HandleFunc("GET /api/notifications", app.handleListNotifications)

	return mux
}
