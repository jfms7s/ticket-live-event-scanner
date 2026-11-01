package telegram

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
)

func rateLimitedServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSendReturnsRateLimitedErrorWithRetryAfter(t *testing.T) {
	srv := rateLimitedServer(t, `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 7","parameters":{"retry_after":7}}`)
	apiBase := srv.URL + "/botSECRET-TOKEN"

	_, err := sendTelegramMessage(context.Background(), srv.Client(), apiBase, "chat", event.Discovered{EventID: 1, Title: "x"})

	var rl *RateLimitedError
	if !errors.As(err, &rl) {
		t.Fatalf("err = %v (%T), want *RateLimitedError", err, err)
	}
	if rl.RetryAfter != 7*time.Second {
		t.Errorf("RetryAfter = %v, want 7s", rl.RetryAfter)
	}
	msg := err.Error()
	if strings.Contains(msg, "SECRET-TOKEN") || strings.Contains(msg, srv.URL) || strings.Contains(msg, "http") {
		t.Errorf("error text %q contains the URL or token", msg)
	}
}

func TestSendRateLimitedWithoutRetryAfterDefaults(t *testing.T) {
	srv := rateLimitedServer(t, `{"ok":false,"error_code":429,"description":"Too Many Requests"}`)

	_, err := sendTelegramMessage(context.Background(), srv.Client(), srv.URL+"/botT", "chat", event.Discovered{EventID: 1, Title: "x"})

	var rl *RateLimitedError
	if !errors.As(err, &rl) {
		t.Fatalf("err = %v, want *RateLimitedError", err)
	}
	if rl.RetryAfter != defaultRetryAfter {
		t.Errorf("RetryAfter = %v, want the %v default", rl.RetryAfter, defaultRetryAfter)
	}
}
