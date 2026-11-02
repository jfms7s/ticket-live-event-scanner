package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestFetchFollowsSameHostRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/evento/a":
			http.Redirect(w, r, "/evento/b", http.StatusFound)
		case "/evento/b":
			_, _ = w.Write([]byte("page b"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	f := New("test-agent", 5*time.Second)
	body, err := f.Fetch(context.Background(), srv.URL+"/evento/a", srv.URL)
	if err != nil {
		t.Fatalf("Fetch: unexpected error: %v", err)
	}
	if body != "page b" {
		t.Errorf("body = %q, want %q", body, "page b")
	}
}

func TestFetchRefusesOffHostRedirect(t *testing.T) {
	var offHostHit bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offHostHit = true
		_, _ = w.Write([]byte("elsewhere"))
	}))
	defer other.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/evento/a", http.StatusFound)
	}))
	defer srv.Close()

	f := New("test-agent", 5*time.Second)
	_, err := f.Fetch(context.Background(), srv.URL+"/evento/a", srv.URL)
	if err == nil {
		t.Fatal("Fetch: expected an error for an off-host redirect, got nil")
	}
	if !strings.Contains(err.Error(), "redirect refused") {
		t.Errorf("error = %v, want a refused redirect", err)
	}
	if offHostHit {
		t.Error("the off-host server was requested")
	}
}

func TestFetchRefusesRedirectToDisallowedPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/evento/a" {
			http.Redirect(w, r, "/carrinho", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("cart"))
	}))
	defer srv.Close()

	f := New("test-agent", 5*time.Second)
	if _, err := f.Fetch(context.Background(), srv.URL+"/evento/a", srv.URL); err == nil {
		t.Fatal("Fetch: expected an error for a redirect to a disallowed path, got nil")
	}
}

func TestFetchStopsRedirectLoop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path, http.StatusFound)
	}))
	defer srv.Close()

	f := New("test-agent", 5*time.Second)
	_, err := f.Fetch(context.Background(), srv.URL+"/evento/loop", srv.URL)
	if err == nil || !strings.Contains(err.Error(), "too many redirects") {
		t.Fatalf("Fetch: error = %v, want too many redirects", err)
	}
}

func TestFetchRejectsSchemeMismatch(t *testing.T) {
	f := New("test-agent", 5*time.Second)
	if _, err := f.Fetch(context.Background(), "http://www.ticketline.pt/evento/x", "https://www.ticketline.pt"); err == nil {
		t.Fatal("Fetch: expected a scheme mismatch error, got nil")
	}
}

func TestFetchRejectsNonOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	f := New("test-agent", 5*time.Second)
	if _, err := f.Fetch(context.Background(), srv.URL+"/evento/a", srv.URL); err == nil {
		t.Fatal("Fetch: expected an error for a non-200 response, got nil")
	}
}
