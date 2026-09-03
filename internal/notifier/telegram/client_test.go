package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
)

// TestSendTelegramMessageSuccess tests successful Telegram API response
func TestSendTelegramMessageSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method and path
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}

		// Verify form data
		if err := r.ParseForm(); err != nil {
			t.Fatalf("failed to parse form: %v", err)
		}

		if r.FormValue("chat_id") != "test-chat-id" {
			t.Errorf("unexpected chat_id: %s", r.FormValue("chat_id"))
		}

		if r.FormValue("parse_mode") != "HTML" {
			t.Errorf("unexpected parse_mode: %s", r.FormValue("parse_mode"))
		}

		// Respond with success
		resp := map[string]interface{}{
			"ok": true,
			"result": map[string]interface{}{
				"message_id": int64(12345),
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Replace default client with test server
	originalClient := http.DefaultClient
	http.DefaultClient.Transport = &http.Transport{
		DisableKeepAlives: true,
	}
	defer func() { http.DefaultClient = originalClient }()

	// Create a test request to use server URL
	ctx := context.Background()
	disc := event.Discovered{
		EventID:   123,
		Title:     "Test Event",
		Venue:     "Test Venue",
		Category:  "Test Category",
		EventDate: "2026-08-22",
		URL:       "https://example.com/event",
	}

	// Manually call the API using a custom client
	messageID, err := sendTelegramMessage(ctx, http.DefaultClient, server.URL, "test-chat-id", disc)
	if err != nil {
		t.Fatalf("SendTelegramMessage failed: %v", err)
	}

	if messageID != "12345" {
		t.Errorf("expected messageID '12345', got '%s'", messageID)
	}
}

// TestSendTelegramMessageWithImage tests that an event with an image is
// sent via sendPhoto (poster inline) with the formatted message as its
// caption, rather than a plain sendMessage.
func TestSendTelegramMessageWithImage(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path

		if err := r.ParseForm(); err != nil {
			t.Fatalf("failed to parse form: %v", err)
		}

		if r.FormValue("photo") != "https://example.com/poster.jpg" {
			t.Errorf("expected photo URL in form, got %q", r.FormValue("photo"))
		}
		if r.FormValue("caption") == "" {
			t.Error("expected non-empty caption")
		}
		if r.FormValue("text") != "" {
			t.Errorf("expected no 'text' field on a photo message, got %q", r.FormValue("text"))
		}

		resp := map[string]interface{}{
			"ok":     true,
			"result": map[string]interface{}{"message_id": int64(999)},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	ctx := context.Background()
	disc := event.Discovered{
		EventID:  123,
		Title:    "Test Event",
		Venue:    "Test Venue",
		URL:      "https://example.com/event",
		ImageURL: "https://example.com/poster.jpg",
	}

	messageID, err := sendTelegramMessage(ctx, http.DefaultClient, server.URL, "test-chat-id", disc)
	if err != nil {
		t.Fatalf("sendTelegramMessage failed: %v", err)
	}
	if messageID != "999" {
		t.Errorf("expected messageID '999', got '%s'", messageID)
	}
	if gotPath != "/sendPhoto" {
		t.Errorf("expected request to /sendPhoto, got %q", gotPath)
	}
}

// TestSendTelegramMessageImageCaptionTooLong tests that an event with an
// image but a message too long for Telegram's photo caption limit falls
// back to a plain sendMessage instead of failing outright.
func TestSendTelegramMessageImageCaptionTooLong(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		resp := map[string]interface{}{
			"ok":     true,
			"result": map[string]interface{}{"message_id": int64(1)},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	ctx := context.Background()
	disc := event.Discovered{
		EventID:  123,
		Title:    strings.Repeat("A very long title ", 100), // well over 1024 chars
		URL:      "https://example.com/event",
		ImageURL: "https://example.com/poster.jpg",
	}

	if _, err := sendTelegramMessage(ctx, http.DefaultClient, server.URL, "test-chat-id", disc); err != nil {
		t.Fatalf("sendTelegramMessage failed: %v", err)
	}
	if gotPath != "/sendMessage" {
		t.Errorf("expected fallback to /sendMessage for an oversized caption, got %q", gotPath)
	}
}

// TestSendTelegramMessageHTTPError tests non-2xx HTTP response
func TestSendTelegramMessageHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("unauthorized"))
	}))
	defer server.Close()

	ctx := context.Background()
	disc := event.Discovered{
		EventID: 123,
		Title:   "Test Event",
		URL:     "https://example.com/event",
	}

	messageID, err := sendTelegramMessage(ctx, http.DefaultClient, server.URL, "test-chat-id", disc)
	if err == nil {
		t.Errorf("expected error, got success with messageID %s", messageID)
	}

	if messageID != "" {
		t.Errorf("expected empty messageID on error, got %s", messageID)
	}
}

// TestSendTelegramMessageOKFalse tests Telegram's ok=false response
func TestSendTelegramMessageOKFalse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"ok":         false,
			"error_code": 400,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	ctx := context.Background()
	disc := event.Discovered{
		EventID: 123,
		Title:   "Test Event",
		URL:     "https://example.com/event",
	}

	messageID, err := sendTelegramMessage(ctx, http.DefaultClient, server.URL, "test-chat-id", disc)
	if err == nil {
		t.Errorf("expected error when ok=false, got success with messageID %s", messageID)
	}

	if messageID != "" {
		t.Errorf("expected empty messageID on error, got %s", messageID)
	}
}

// TestSendTelegramMessageMalformedJSON tests invalid JSON response
func TestSendTelegramMessageMalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not valid json"))
	}))
	defer server.Close()

	ctx := context.Background()
	disc := event.Discovered{
		EventID: 123,
		Title:   "Test Event",
		URL:     "https://example.com/event",
	}

	messageID, err := sendTelegramMessage(ctx, http.DefaultClient, server.URL, "test-chat-id", disc)
	if err == nil {
		t.Errorf("expected error for malformed JSON, got success with messageID %s", messageID)
	}

	if messageID != "" {
		t.Errorf("expected empty messageID on error, got %s", messageID)
	}
}
