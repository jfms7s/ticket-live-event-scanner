package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/runs"
)

// fakeRuns is a RunsReader returning a fixed summary.
type fakeRuns struct {
	sum       runs.Summary
	err       error
	lastLimit int
}

func (f *fakeRuns) Summary(_ context.Context, limit int) (runs.Summary, error) {
	f.lastLimit = limit
	return f.sum, f.err
}

func getRuns(t *testing.T, app *App, query string) *http.Response {
	t.Helper()
	srv := httptest.NewServer(app.NewMux())
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/api/runs" + query)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func sampleSummary() runs.Summary {
	start := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	ok := start.Add(-time.Hour)
	r := runs.Record{
		RunID: runs.RunID(start), HubID: 98164, HubSlug: "maia-98164", StartedAt: start,
		FinishedAt: start.Add(time.Minute), SessionsSeen: 4, FutureSessions: 0, Error: "fetch hub page 98164: timeout",
	}
	return runs.Summary{
		Latest: []runs.HubStatus{{Record: r, LastSuccessAt: &ok, UpcomingSessions: 2, ConsecutiveEmptyRuns: 1}},
		Runs:   []runs.Record{r},
	}
}

func TestListRunsServesTheLatestRunPerHub(t *testing.T) {
	fake := &fakeRuns{sum: sampleSummary()}
	app := &App{}
	app.SetRuns(fake)

	resp := getRuns(t, app, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if fake.lastLimit != defaultRunsLimit {
		t.Errorf("limit = %d, want the default %d", fake.lastLimit, defaultRunsLimit)
	}
	var got runs.Summary
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Latest) != 1 || got.Latest[0].HubSlug != "maia-98164" || got.Latest[0].Error == "" {
		t.Errorf("latest = %+v", got.Latest)
	}
	if len(got.Runs) != 1 {
		t.Errorf("runs = %+v", got.Runs)
	}
}

func TestListRunsLimit(t *testing.T) {
	fake := &fakeRuns{sum: sampleSummary()}
	app := &App{}
	app.SetRuns(fake)

	if resp := getRuns(t, app, "?limit=200"); resp.StatusCode != http.StatusOK || fake.lastLimit != 200 {
		t.Errorf("limit=200: status %d, limit %d", resp.StatusCode, fake.lastLimit)
	}
	for _, bad := range []string{"?limit=201", "?limit=-1", "?limit=x"} {
		if resp := getRuns(t, app, bad); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", bad, resp.StatusCode)
		}
	}
}

func TestListRunsWithoutARunLog(t *testing.T) {
	resp := getRuns(t, &App{}, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["latest"]) != "[]" || string(raw["runs"]) != "[]" {
		t.Errorf("body = %s / %s, want empty arrays", raw["latest"], raw["runs"])
	}
}

func TestListRunsBucketError(t *testing.T) {
	app := &App{}
	app.SetRuns(&fakeRuns{err: errors.New("nats: timeout")})
	resp := getRuns(t, app, "")
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

// The JSON field names of a run are the contract with the dashboard's Run
// and HubStatus types in web/frontend/src/types.ts.
var wantRunKeys = []string{
	"error", "finished_at", "future_sessions", "hub_id", "hub_slug", "new_events",
	"run_id", "sessions_seen", "started_at",
}

var wantHubStatusKeys = append(append([]string{}, wantRunKeys...),
	"consecutive_empty_runs", "last_success_at", "upcoming_sessions")

func TestRunJSONContract(t *testing.T) {
	sum := sampleSummary()
	raw, err := json.Marshal(sum)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Latest []map[string]json.RawMessage `json:"latest"`
		Runs   []map[string]json.RawMessage `json:"runs"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if got := sortedKeys(body.Runs[0]); !reflect.DeepEqual(got, sortedCopy(wantRunKeys)) {
		t.Errorf("run JSON keys = %v, want %v (update web/frontend/src/types.ts too)", got, wantRunKeys)
	}
	if got := sortedKeys(body.Latest[0]); !reflect.DeepEqual(got, sortedCopy(wantHubStatusKeys)) {
		t.Errorf("hub status JSON keys = %v, want %v (update web/frontend/src/types.ts too)", got, wantHubStatusKeys)
	}
}

func sortedCopy(s []string) []string {
	m := make(map[string]json.RawMessage, len(s))
	for _, k := range s {
		m[k] = nil
	}
	return sortedKeys(m)
}
