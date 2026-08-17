package event

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDiscoveredJSONRoundTrip(t *testing.T) {
	orig := Discovered{
		EventID:   123,
		Slug:      "test-event",
		Title:     "Test Event",
		Venue:     "Test Venue",
		Category:  "Music",
		EventDate: "2024-12-25",
		URL:       "https://example.com/event",
		ImageURL:  "https://example.com/image.jpg",
		Reason:    ReasonDiscovered,
	}

	// Marshal to JSON
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Unmarshal back
	var restored Discovered
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if restored != orig {
		t.Errorf("Round-trip failed: got %+v, want %+v", restored, orig)
	}
}

func TestNotificationSentJSONRoundTrip(t *testing.T) {
	now := time.Now().UTC()
	orig := NotificationSent{
		EventID:           456,
		TelegramMessageID: "msg-789",
		SentAt:            now,
	}

	// Marshal to JSON
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Unmarshal back
	var restored NotificationSent
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if restored != orig {
		t.Errorf("Round-trip failed: got %+v, want %+v", restored, orig)
	}
}

func TestNotificationFailedJSONRoundTrip(t *testing.T) {
	now := time.Now().UTC()
	orig := NotificationFailed{
		EventID:  789,
		Error:    "connection timeout",
		FailedAt: now,
		Attempts: 5,
	}

	// Marshal to JSON
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Unmarshal back
	var restored NotificationFailed
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if restored != orig {
		t.Errorf("Round-trip failed: got %+v, want %+v", restored, orig)
	}
}

func TestReasonConstants(t *testing.T) {
	if ReasonDiscovered != "discovered" {
		t.Errorf("ReasonDiscovered = %q, want %q", ReasonDiscovered, "discovered")
	}
	if ReasonManualRetry != "manual-retry" {
		t.Errorf("ReasonManualRetry = %q, want %q", ReasonManualRetry, "manual-retry")
	}
}
