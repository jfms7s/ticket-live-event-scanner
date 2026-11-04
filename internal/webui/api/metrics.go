package api

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/runs"
)

// handleMetrics serves the scraper's run health in the Prometheus text
// format, derived from the run log on every scrape. The homelab
// PrometheusRule alerts on these series (run failing, hub empty, no
// recent run); there is no in-app alerting.
//
// A run log that can't be read is ticket_scanner_run_log_up 0 with no
// other series, so the rules see "no data" rather than old values.
func (app *App) handleMetrics(w http.ResponseWriter, r *http.Request) {
	var sum runs.Summary
	up := app.runs != nil
	if up {
		var err error
		sum, err = app.runs.Summary(r.Context(), 0)
		if err != nil {
			log.Printf("Error reading the run log for /metrics: %v", err)
			up = false
		}
	}

	sum = app.withoutDisabledHubs(r.Context(), sum)

	var b bytes.Buffer
	writeRunMetrics(&b, up, sum.Latest)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	if _, err := w.Write(b.Bytes()); err != nil {
		log.Printf("Error writing /metrics: %v", err)
	}
}

// writeRunMetrics renders the run-health series. Pure, for tests.
func writeRunMetrics(b *bytes.Buffer, up bool, latest []runs.HubStatus) {
	gauge(b, "ticket_scanner_run_log_up", "1 when web-ui-api could read the scraper's run log (SCRAPER_RUNS).")
	fmt.Fprintf(b, "ticket_scanner_run_log_up %d\n", boolInt(up))
	if !up || len(latest) == 0 {
		return
	}

	var newest time.Time
	for _, h := range latest {
		if h.FinishedAt.After(newest) {
			newest = h.FinishedAt
		}
	}
	gauge(b, "ticket_scanner_last_run_timestamp_seconds", "When the newest scraper run finished (Unix seconds).")
	fmt.Fprintf(b, "ticket_scanner_last_run_timestamp_seconds %s\n", unixSeconds(newest))

	series := []struct {
		name, help string
		value      func(runs.HubStatus) (string, bool)
	}{
		{"ticket_scanner_hub_last_run_timestamp_seconds", "When the hub's newest run finished (Unix seconds).",
			func(h runs.HubStatus) (string, bool) { return unixSeconds(h.FinishedAt), true }},
		{"ticket_scanner_hub_last_run_failed", "1 when the hub's newest run failed.",
			func(h runs.HubStatus) (string, bool) { return strconv.Itoa(boolInt(h.Failed())), true }},
		{"ticket_scanner_hub_last_success_timestamp_seconds", "When the hub last finished a run without an error (Unix seconds; absent when none in the last week).",
			func(h runs.HubStatus) (string, bool) {
				if h.LastSuccessAt == nil {
					return "", false
				}
				return unixSeconds(*h.LastSuccessAt), true
			}},
		{"ticket_scanner_hub_upcoming_sessions", "Sessions dated today or later on the hub page at its newest successful run.",
			func(h runs.HubStatus) (string, bool) { return strconv.Itoa(h.UpcomingSessions), true }},
		{"ticket_scanner_hub_consecutive_empty_runs", "Successful runs in a row (one per hour) in which the hub listed no future session.",
			func(h runs.HubStatus) (string, bool) { return strconv.Itoa(h.ConsecutiveEmptyRuns), true }},
	}
	for _, s := range series {
		gauge(b, s.name, s.help)
		for _, h := range latest {
			if v, ok := s.value(h); ok {
				fmt.Fprintf(b, "%s{hub=\"%s\",hub_id=\"%d\"} %s\n", s.name, escapeLabel(h.HubSlug), h.HubID, v)
			}
		}
	}
}

func gauge(b *bytes.Buffer, name, help string) {
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s gauge\n", name, help, name)
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func unixSeconds(t time.Time) string {
	return strconv.FormatFloat(float64(t.UnixMilli())/1000, 'f', 3, 64)
}

// escapeLabel escapes a label value for the text format.
var labelEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

func escapeLabel(s string) string { return labelEscaper.Replace(s) }
