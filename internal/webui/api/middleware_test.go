package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func innerRecorder() (http.Handler, *bool) {
	called := false
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}), &called
}

func TestRequireCustomHeaderRejectsMutationsWithoutIt(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPatch, http.MethodDelete} {
		inner, called := innerRecorder()
		rec := httptest.NewRecorder()
		RequireCustomHeader(inner).ServeHTTP(rec, httptest.NewRequest(method, "/api/events/1", nil))
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s without header: status %d, want 403", method, rec.Code)
		}
		if *called {
			t.Errorf("%s without header reached the handler", method)
		}
		if !strings.Contains(rec.Body.String(), "missing X-Requested-With header") {
			t.Errorf("%s body = %q", method, rec.Body.String())
		}
	}
}

func TestRequireCustomHeaderPassesMutationsWithIt(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPatch, http.MethodDelete} {
		inner, called := innerRecorder()
		req := httptest.NewRequest(method, "/api/events/1", nil)
		req.Header.Set("X-Requested-With", "ticket-scanner")
		rec := httptest.NewRecorder()
		RequireCustomHeader(inner).ServeHTTP(rec, req)
		if !*called || rec.Code != http.StatusNoContent {
			t.Errorf("%s with header: called=%v status=%d", method, *called, rec.Code)
		}
	}
}

func TestRequireCustomHeaderPassesReads(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		inner, called := innerRecorder()
		rec := httptest.NewRecorder()
		RequireCustomHeader(inner).ServeHTTP(rec, httptest.NewRequest(method, "/api/events", nil))
		if !*called {
			t.Errorf("%s without header was blocked", method)
		}
	}
}

func TestCORSPreflightAllowsTheCustomHeader(t *testing.T) {
	inner, _ := innerRecorder()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/events/1", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	CORSMiddleware(RequireCustomHeader(inner), "http://localhost:3000").ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "X-Requested-With") {
		t.Errorf("Access-Control-Allow-Headers = %q, want X-Requested-With", got)
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Errorf("Vary = %q, want Origin", got)
	}
}
