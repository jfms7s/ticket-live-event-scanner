package telegram

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
)

const fakeToken = "SECRET123:XYZ"

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestSendTelegramMessageTransportErrorOmitsToken checks that a failed
// request doesn't put the bot token (which is part of the API URL) into the
// returned error, since that text is logged and stored.
func TestSendTelegramMessageTransportErrorOmitsToken(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial tcp: connection refused")
	})}

	_, err := sendTelegramMessage(context.Background(), client, "https://api.telegram.org/bot"+fakeToken, "1", event.Discovered{Title: "t"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "SECRET123") {
		t.Fatalf("error leaks the token: %v", err)
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error lost its cause: %v", err)
	}
}

// TestSendTelegramMessageTimeoutOmitsToken covers the client-timeout path,
// where net/http builds the *url.Error itself.
func TestSendTelegramMessageTimeoutOmitsToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
	}))
	defer server.Close()

	client := &http.Client{Timeout: time.Nanosecond}
	_, err := sendTelegramMessage(context.Background(), client, server.URL+"/bot"+fakeToken, "1", event.Discovered{Title: "t"})
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "SECRET123") {
		t.Fatalf("error leaks the token: %v", err)
	}
}

func TestRedactTokenRemovesTokenSegment(t *testing.T) {
	err := redactToken(errors.New("boom at https://api.telegram.org/bot"+fakeToken+"/sendMessage"), "https://api.telegram.org/bot"+fakeToken)
	if strings.Contains(err.Error(), "SECRET123") {
		t.Fatalf("token not redacted: %v", err)
	}
	if !strings.Contains(err.Error(), "<redacted>") {
		t.Errorf("expected <redacted> marker: %v", err)
	}
}
