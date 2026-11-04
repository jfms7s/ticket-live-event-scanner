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

	// Scraper run log, and its health for Prometheus
	mux.HandleFunc("GET /api/runs", app.handleListRuns)
	mux.HandleFunc("GET /metrics", app.handleMetrics)

	// Dashboard: the catch-all; the patterns above are more specific and win.
	if app.static != nil {
		mux.Handle("GET /", app.static)
	}

	return mux
}
