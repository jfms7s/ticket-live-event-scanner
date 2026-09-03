package api

import (
	"fmt"
	"net/http"
)

// handleHealthz returns a simple health check response.
func (app *App) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "\"ok\"\n")
}
