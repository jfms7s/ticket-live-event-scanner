package api

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/runs"
)

func getMetrics(t *testing.T, app *App) (int, string) {
	t.Helper()
	srv := httptest.NewServer(app.NewMux())
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain; version=0.0.4") {
		t.Errorf("Content-Type = %q, want the Prometheus text format", ct)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body)
}

func TestMetricsFromTheRunLog(t *testing.T) {
	start := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	ok := start.Add(-time.Hour)
	maia := runs.HubStatus{
		Record: runs.Record{HubID: 98164, HubSlug: "maia-98164", StartedAt: start, FinishedAt: start.Add(1500 * time.Millisecond),
			Error: "fetch hub page 98164: timeout"},
		LastSuccessAt: &ok, UpcomingSessions: 4,
	}
	aveiro := runs.HubStatus{
		Record:               runs.Record{HubID: 98167, HubSlug: `ave"iro-98167`, StartedAt: start, FinishedAt: start.Add(time.Second)},
		ConsecutiveEmptyRuns: 3,
	}
	app := &App{}
	app.SetRuns(&fakeRuns{sum: runs.Summary{Latest: []runs.HubStatus{maia, aveiro}}})

	status, body := getMetrics(t, app)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	for _, want := range []string{
		"ticket_scanner_run_log_up 1\n",
		fmt.Sprintf("ticket_scanner_last_run_timestamp_seconds %d.500\n", start.Unix()+1),
		`ticket_scanner_hub_last_run_failed{hub="maia-98164",hub_id="98164"} 1` + "\n",
		`ticket_scanner_hub_last_run_failed{hub="ave\"iro-98167",hub_id="98167"} 0` + "\n",
		fmt.Sprintf(`ticket_scanner_hub_last_success_timestamp_seconds{hub="maia-98164",hub_id="98164"} %d.000`+"\n", ok.Unix()),
		`ticket_scanner_hub_upcoming_sessions{hub="maia-98164",hub_id="98164"} 4` + "\n",
		`ticket_scanner_hub_consecutive_empty_runs{hub="ave\"iro-98167",hub_id="98167"} 3` + "\n",
		"# TYPE ticket_scanner_hub_last_run_timestamp_seconds gauge\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics lack %q\n%s", want, body)
		}
	}
	// No success in the window: no series rather than a zero timestamp.
	if strings.Contains(body, `ticket_scanner_hub_last_success_timestamp_seconds{hub="ave`) {
		t.Errorf("a hub without a success has a last-success series:\n%s", body)
	}
}

func TestMetricsWhenTheRunLogFails(t *testing.T) {
	app := &App{}
	app.SetRuns(&fakeRuns{err: errors.New("nats: timeout")})
	status, body := getMetrics(t, app)
	if status != http.StatusOK || !strings.Contains(body, "ticket_scanner_run_log_up 0\n") {
		t.Errorf("status %d body:\n%s\nwant 200 with run_log_up 0", status, body)
	}
	if strings.Contains(body, "ticket_scanner_hub_") || strings.Contains(body, "last_run_timestamp") {
		t.Errorf("run series served without a run log:\n%s", body)
	}
}

func TestMetricsWithNoRunsYet(t *testing.T) {
	app := &App{}
	app.SetRuns(&fakeRuns{sum: runs.Summary{}})
	_, body := getMetrics(t, app)
	if !strings.Contains(body, "ticket_scanner_run_log_up 1\n") || strings.Contains(body, "last_run_timestamp_seconds ") {
		t.Errorf("body:\n%s\nwant run_log_up 1 and no run series", body)
	}
}

func TestMetricsReportWhichNotifiersAreEnabled(t *testing.T) {
	app := &App{}
	app.SetNotifierStatus(map[string]bool{"telegram": true, "email": false})
	_, body := getMetrics(t, app)
	for _, want := range []string{
		`ticket_scanner_notifier_enabled{channel="email"} 0` + "\n",
		`ticket_scanner_notifier_enabled{channel="telegram"} 1` + "\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics lack %q:\n%s", want, body)
		}
	}
}

func TestMetricsOmitNotifiersWhenNoneAreReported(t *testing.T) {
	_, body := getMetrics(t, &App{})
	if strings.Contains(body, "notifier_enabled") {
		t.Errorf("metrics carry notifier series without a status:\n%s", body)
	}
}
