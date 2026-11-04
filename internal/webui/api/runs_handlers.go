package api

import (
	"log"
	"net/http"
	"strconv"

	"github.com/jfms7s/ticket-live-event-scanner/internal/runs"
)

const (
	defaultRunsLimit = 24
	maxRunsLimit     = 200
)

// handleListRuns returns the newest run's outcome per hub and the newest
// run records: {"latest": [...], "runs": [...]}. ?limit= sets how many
// records (default 24, at most 200).
func (app *App) handleListRuns(w http.ResponseWriter, r *http.Request) {
	limit := defaultRunsLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > maxRunsLimit {
			writeError(w, http.StatusBadRequest, "limit must be a number from 0 to 200")
			return
		}
		limit = n
	}

	if app.runs == nil {
		writeJSON(w, http.StatusOK, runs.Summary{Latest: []runs.HubStatus{}, Runs: []runs.Record{}})
		return
	}
	sum, err := app.runs.Summary(r.Context(), limit)
	if err != nil {
		log.Printf("Error reading the run log: %v", err)
		writeError(w, http.StatusServiceUnavailable, "run log unavailable")
		return
	}
	writeJSON(w, http.StatusOK, app.withoutDisabledHubs(r.Context(), sum))
}
