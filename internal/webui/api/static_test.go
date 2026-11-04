package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/jfms7s/ticket-live-event-scanner/internal/webui/static"
)

func TestDashboardAndAPIShareTheMux(t *testing.T) {
	app := NewApp(setupTestDB(t), &MockJetStream{})
	app.SetStatic(static.Handler(fstest.MapFS{
		"index.html":    {Data: []byte("<html>dash</html>")},
		"assets/a-1.js": {Data: []byte("x")},
	}))
	h := RequireCustomHeader(app.NewMux())

	do := func(method, target string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
		return rec
	}

	if rec := do(http.MethodGet, "/"); rec.Code != http.StatusOK || rec.Body.String() != "<html>dash</html>" {
		t.Errorf("GET / = %d %q", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodGet, "/assets/a-1.js"); rec.Code != http.StatusOK || rec.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Errorf("GET /assets/a-1.js = %d, Cache-Control %q", rec.Code, rec.Header().Get("Cache-Control"))
	}
	if rec := do(http.MethodGet, "/api/events"); rec.Code != http.StatusOK || rec.Body.String() == "<html>dash</html>" {
		t.Errorf("GET /api/events = %d %q, want the API", rec.Code, rec.Body.String())
	}
	if rec := do(http.MethodGet, "/healthz"); rec.Code != http.StatusOK {
		t.Errorf("GET /healthz = %d", rec.Code)
	}
	if rec := do(http.MethodGet, "/api/unknown"); rec.Code != http.StatusNotFound {
		t.Errorf("GET /api/unknown = %d, want 404 not index.html", rec.Code)
	}
	if rec := do(http.MethodDelete, "/api/events/1"); rec.Code != http.StatusForbidden {
		t.Errorf("DELETE without header = %d, want 403", rec.Code)
	}
}

func TestNoDashboardWithoutStatic(t *testing.T) {
	app := NewApp(setupTestDB(t), &MockJetStream{})
	rec := httptest.NewRecorder()
	app.NewMux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET / = %d, want 404", rec.Code)
	}
}
