package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/runs"
	"github.com/jfms7s/ticket-live-event-scanner/internal/scraper/parse"
	"github.com/jfms7s/ticket-live-event-scanner/internal/watch"
)

const (
	maxWatchBodyBytes = 4 << 10
	maxLabelLen       = 80
)

// watchResponse is a watch with its ticketline hub ID, which the dashboard
// matches against the run log.
type watchResponse struct {
	watch.Watch
	HubID int64 `json:"hub_id"`
}

func toWatchResponse(w watch.Watch) watchResponse {
	return watchResponse{Watch: w, HubID: parse.ExtractIDFromSlug(w.Slug)}
}

// handleListWatches returns every watch, oldest first.
func (app *App) handleListWatches(w http.ResponseWriter, r *http.Request) {
	ws, err := watch.List(r.Context(), app.db)
	if err != nil {
		log.Printf("Error listing watches: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	out := make([]watchResponse, len(ws))
	for i, x := range ws {
		out[i] = toWatchResponse(x)
	}
	writeJSON(w, http.StatusOK, out)
}

type addWatchRequest struct {
	// URL is a ticketline.pt hub page URL, "/evento/{slug}" path or bare slug.
	URL   string `json:"url"`
	Label string `json:"label"`
}

// handleAddWatch starts watching a hub page. The scraper picks it up on its
// next run.
func (app *App) handleAddWatch(w http.ResponseWriter, r *http.Request) {
	var body addWatchRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxWatchBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	slug, err := watch.Parse(body.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	label, ok := cleanLabel(body.Label)
	if !ok {
		writeError(w, http.StatusBadRequest, "label is too long")
		return
	}

	added, err := watch.Add(r.Context(), app.db, slug, label, time.Now())
	if errors.Is(err, watch.ErrDuplicate) {
		writeError(w, http.StatusConflict, "this hub is already watched")
		return
	}
	if err != nil {
		log.Printf("Error adding watch %s: %v", slug, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusCreated, toWatchResponse(added))
}

type updateWatchRequest struct {
	Enabled *bool   `json:"enabled"`
	Label   *string `json:"label"`
}

// handleUpdateWatch enables or disables a watch, or renames it. Disabling
// stops scraping the hub; its events stay.
func (app *App) handleUpdateWatch(w http.ResponseWriter, r *http.Request) {
	var body updateWatchRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxWatchBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || (body.Enabled == nil && body.Label == nil) {
		writeError(w, http.StatusBadRequest, "invalid request body: expected enabled and/or label")
		return
	}
	if body.Label != nil {
		label, ok := cleanLabel(*body.Label)
		if !ok {
			writeError(w, http.StatusBadRequest, "label is too long")
			return
		}
		body.Label = &label
	}

	updated, err := watch.Update(r.Context(), app.db, r.PathValue("slug"), body.Enabled, body.Label)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "watch not found")
		return
	}
	if err != nil {
		log.Printf("Error updating watch %s: %v", r.PathValue("slug"), err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, toWatchResponse(updated))
}

func cleanLabel(s string) (string, bool) {
	s = strings.TrimSpace(s)
	return s, len([]rune(s)) <= maxLabelLen
}

// withoutDisabledHubs drops the run log's rows for hubs that are disabled
// watches, so a retired hub neither colours the dashboard header nor trips
// the empty-hub alerts until its records expire. A watch list that cannot
// be read leaves the summary as it is.
func (app *App) withoutDisabledHubs(ctx context.Context, sum runs.Summary) runs.Summary {
	if app.db == nil {
		return sum
	}
	off, err := watch.Disabled(ctx, app.db)
	if err != nil {
		log.Printf("Error reading watches for the run log: %v", err)
		return sum
	}
	if len(off) == 0 {
		return sum
	}
	out := runs.Summary{Latest: []runs.HubStatus{}, Runs: []runs.Record{}}
	for _, h := range sum.Latest {
		if !off[h.HubSlug] {
			out.Latest = append(out.Latest, h)
		}
	}
	for _, rec := range sum.Runs {
		if !off[rec.HubSlug] {
			out.Runs = append(out.Runs, rec)
		}
	}
	return out
}
