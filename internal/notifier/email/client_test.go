package email

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestBuildMIMEMessage(t *testing.T) {
	msg := buildMIMEMessage(
		"noreply@example.com",
		[]string{"a@example.com", "b@example.com"},
		"Calendar invite: Test Concert",
		"<event-42-1@ticket-live-event-scanner>",
		"Your calendar invite for Test Concert is attached.",
		"BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n",
	)
	text := string(msg)

	for _, want := range []string{
		"From: noreply@example.com\r\n",
		"To: a@example.com, b@example.com\r\n",
		"Subject: Calendar invite: Test Concert\r\n",
		"Message-ID: <event-42-1@ticket-live-event-scanner>\r\n",
		"MIME-Version: 1.0\r\n",
		"Content-Type: multipart/mixed;",
		"Content-Type: text/calendar; method=REQUEST; charset=UTF-8",
		"Content-Transfer-Encoding: base64",
		`Content-Disposition: attachment; filename="event.ics"`,
		"Your calendar invite for Test Concert is attached.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("buildMIMEMessage() missing %q in:\n%s", want, text)
		}
	}

	// The ics content should round-trip through the base64 attachment.
	encoded := base64.StdEncoding.EncodeToString([]byte("BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n"))
	if !strings.Contains(text, encoded[:20]) {
		t.Errorf("buildMIMEMessage() missing base64-encoded ics content in:\n%s", text)
	}
}
