package telegram

import (
	"fmt"
	"testing"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
)

// TestFormatTelegramMessage tests message formatting
func TestFormatTelegramMessage(t *testing.T) {
	disc := event.Discovered{
		Title:     "Test Event",
		Venue:     "Test Venue",
		Category:  "Test Category",
		EventDate: "2026-08-22",
		URL:       "https://example.com/event",
	}

	msg := formatTelegramMessage(disc)

	// Check that key fields are present
	if msg == "" {
		t.Error("formatted message is empty")
	}

	// The message should contain HTML tags (indicating it's formatted for HTML parse_mode)
	if !contains(msg, "<b>") {
		t.Error("message should contain bold formatting")
	}

	if !contains(msg, "Test Event") {
		t.Error("message should contain title")
	}

	if !contains(msg, "Test Venue") {
		t.Error("message should contain venue")
	}

	if !contains(msg, "Test Category") {
		t.Error("message should contain category")
	}

	if !contains(msg, "2026-08-22") {
		t.Error("message should contain event date")
	}
}

// TestFormatTelegramMessageEventDateWithTime tests that a date+time
// EventDate (e.g. hub sessions with a fixed start time) renders as
// "date time" instead of the raw "date<T>time".
func TestFormatTelegramMessageEventDateWithTime(t *testing.T) {
	disc := event.Discovered{
		Title:     "Test Session",
		EventDate: "2026-09-04T18:30",
		URL:       "https://example.com/event",
	}

	msg := formatTelegramMessage(disc)

	if !contains(msg, "2026-09-04 18:30") {
		t.Errorf("expected message to contain '2026-09-04 18:30', got: %s", msg)
	}
	if contains(msg, "2026-09-04T18:30") {
		t.Errorf("expected raw 'T' separator to be replaced, got: %s", msg)
	}
}

// TestHTMLEscape tests HTML escaping for special characters
func TestHTMLEscape(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "simple text",
			expected: "simple text",
		},
		{
			input:    "text & more",
			expected: "text &amp; more",
		},
		{
			input:    "<script>alert('xss')</script>",
			expected: "&lt;script&gt;alert(&#39;xss&#39;)&lt;/script&gt;",
		},
		{
			input:    `"quoted" & <test>`,
			expected: "&quot;quoted&quot; &amp; &lt;test&gt;",
		},
		{
			input:    "single' quote",
			expected: "single&#39; quote",
		},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("escape_%s", tt.input[:10]), func(t *testing.T) {
			got := htmlEscape(tt.input)
			if got != tt.expected {
				t.Errorf("htmlEscape(%q) = %q, expected %q", tt.input, got, tt.expected)
			}
		})
	}
}

// TestHTMLEscapeInMessage ensures message text is properly escaped
func TestHTMLEscapeInMessage(t *testing.T) {
	disc := event.Discovered{
		Title:     "Event <with> & special \"chars\"",
		Venue:     "Venue's Place & Gallery",
		Category:  "Category<A>",
		EventDate: "2026-08-22",
		URL:       "https://example.com/event?param=a&b=c",
	}

	msg := formatTelegramMessage(disc)

	// Check that special characters are escaped
	if contains(msg, "<with>") || contains(msg, " & ") {
		t.Error("message contains unescaped user input")
	}

	// Check that they are properly escaped
	if !contains(msg, "&lt;with&gt;") {
		t.Error("message should escape < and >")
	}

	if !contains(msg, "&amp;") {
		t.Error("message should escape &")
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) > len(substr) && contains2(s, substr))
}

func contains2(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
