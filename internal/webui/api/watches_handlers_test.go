package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jfms7s/ticket-live-event-scanner/internal/runs"
)

func watchesServer(t *testing.T) (*App, *httptest.Server) {
	t.Helper()
	app := NewApp(setupTestDB(t), nil)
	srv := httptest.NewServer(RequireCustomHeader(app.NewMux()))
	t.Cleanup(srv.Close)
	return app, srv
}

func send(t *testing.T, srv *httptest.Server, method, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(CustomHeader, "test")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestAddListAndUpdateWatches(t *testing.T) {
	_, srv := watchesServer(t)

	if code, body := send(t, srv, http.MethodGet, "/api/watches", ""); code != http.StatusOK || strings.TrimSpace(body) != "[]" {
		t.Fatalf("empty list = %d %q, want 200 []", code, body)
	}

	code, body := send(t, srv, http.MethodPost, "/api/watches", `{"url":"https://www.ticketline.pt/evento/fado-hub-500/","label":"  Fado  "}`)
	if code != http.StatusCreated {
		t.Fatalf("POST = %d %s", code, body)
	}
	var added watchResponse
	if err := json.Unmarshal([]byte(body), &added); err != nil {
		t.Fatal(err)
	}
	if added.Slug != "fado-hub-500" || added.Label != "Fado" || !added.Enabled || added.HubID != 500 {
		t.Errorf("added = %+v", added)
	}

	if code, _ := send(t, srv, http.MethodPost, "/api/watches", `{"url":"fado-hub-500"}`); code != http.StatusConflict {
		t.Errorf("duplicate POST = %d, want 409", code)
	}

	code, body = send(t, srv, http.MethodPatch, "/api/watches/fado-hub-500", `{"enabled":false}`)
	var updated watchResponse
	if code != http.StatusOK || json.Unmarshal([]byte(body), &updated) != nil || updated.Enabled || updated.Label != "Fado" {
		t.Errorf("PATCH enabled=false = %d %s", code, body)
	}
	code, body = send(t, srv, http.MethodPatch, "/api/watches/fado-hub-500", `{"label":"Fado Lisboa"}`)
	if code != http.StatusOK || json.Unmarshal([]byte(body), &updated) != nil || updated.Enabled || updated.Label != "Fado Lisboa" {
		t.Errorf("PATCH label = %d %s", code, body)
	}

	_, body = send(t, srv, http.MethodGet, "/api/watches", "")
	var list []watchResponse
	if err := json.Unmarshal([]byte(body), &list); err != nil || len(list) != 1 || list[0].Enabled {
		t.Errorf("list = %s, %v", body, err)
	}
}

func TestAddWatchRejectsBadInput(t *testing.T) {
	_, srv := watchesServer(t)
	for name, body := range map[string]string{
		"not json":   `x`,
		"empty":      `{}`,
		"other host": `{"url":"https://evil.example/evento/fado-hub-500"}`,
		"no id":      `{"url":"https://www.ticketline.pt/evento/fado-hub"}`,
		"other path": `{"url":"https://www.ticketline.pt/agenda/fado-hub-500"}`,
		"long label": `{"url":"fado-hub-500","label":"` + strings.Repeat("x", 81) + `"}`,
		"too big":    `{"url":"fado-hub-500","label":"` + strings.Repeat("x", 5000) + `"}`,
	} {
		if code, _ := send(t, srv, http.MethodPost, "/api/watches", body); code != http.StatusBadRequest {
			t.Errorf("%s: POST = %d, want 400", name, code)
		}
	}
	if _, body := send(t, srv, http.MethodGet, "/api/watches", ""); strings.TrimSpace(body) != "[]" {
		t.Errorf("a rejected POST left watches behind: %s", body)
	}
}

func TestUpdateWatchErrors(t *testing.T) {
	_, srv := watchesServer(t)
	if code, _ := send(t, srv, http.MethodPatch, "/api/watches/missing-9", `{"enabled":false}`); code != http.StatusNotFound {
		t.Errorf("PATCH unknown = %d, want 404", code)
	}
	send(t, srv, http.MethodPost, "/api/watches", `{"url":"fado-hub-500"}`)
	for _, body := range []string{`{}`, `x`, `{"label":"` + strings.Repeat("x", 81) + `"}`} {
		if code, _ := send(t, srv, http.MethodPatch, "/api/watches/fado-hub-500", body); code != http.StatusBadRequest {
			t.Errorf("PATCH %.20q = %d, want 400", body, code)
		}
	}
}

func TestWatchMutationsNeedTheCustomHeader(t *testing.T) {
	_, srv := watchesServer(t)
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/watches"}, {http.MethodPatch, "/api/watches/x-1"},
	} {
		req, _ := http.NewRequest(c.method, srv.URL+c.path, strings.NewReader(`{"url":"fado-hub-500"}`))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s without the header = %d, want 403", c.method, c.path, resp.StatusCode)
		}
	}
}

// A disabled watch keeps its events and drops out of the run log, so a
// retired hub neither colours the header nor trips the empty-hub alerts.
func TestDisabledWatchKeepsEventsAndLeavesTheRunLog(t *testing.T) {
	app, srv := watchesServer(t)
	app.SetRuns(&fakeRuns{sum: runs.Summary{
		Latest: []runs.HubStatus{{Record: runs.Record{HubID: 500, HubSlug: "fado-hub-500"}}, {Record: runs.Record{HubID: 600, HubSlug: "other-600"}}},
		Runs:   []runs.Record{{HubID: 500, HubSlug: "fado-hub-500"}, {HubID: 600, HubSlug: "other-600"}},
	}})
	if _, err := app.db.Exec(`INSERT INTO events (id, slug, title, url, hub_slug) VALUES (1, 's', 'Show', 'https://x', 'fado-hub-500')`); err != nil {
		t.Fatal(err)
	}
	send(t, srv, http.MethodPost, "/api/watches", `{"url":"fado-hub-500"}`)
	send(t, srv, http.MethodPatch, "/api/watches/fado-hub-500", `{"enabled":false}`)

	_, body := send(t, srv, http.MethodGet, "/api/runs", "")
	var sum runs.Summary
	if err := json.Unmarshal([]byte(body), &sum); err != nil {
		t.Fatal(err)
	}
	if len(sum.Latest) != 1 || sum.Latest[0].HubSlug != "other-600" || len(sum.Runs) != 1 {
		t.Errorf("runs after disabling fado-hub-500 = %s", body)
	}
	_, metrics := send(t, srv, http.MethodGet, "/metrics", "")
	if strings.Contains(metrics, "fado-hub-500") || !strings.Contains(metrics, "other-600") {
		t.Errorf("metrics still carry the disabled hub:\n%s", metrics)
	}
	_, events := send(t, srv, http.MethodGet, "/api/events", "")
	if !strings.Contains(events, `"hub_slug":"fado-hub-500"`) {
		t.Errorf("the disabled hub's event is gone from /api/events: %s", events)
	}
}
