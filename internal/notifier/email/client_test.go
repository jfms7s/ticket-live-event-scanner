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
		"BEGIN:VCALENDAR\r\nMETHOD:REQUEST\r\nEND:VCALENDAR\r\n",
	)
	text := string(msg)

	for _, want := range []string{
		"From: noreply@example.com\r\n",
		"To: a@example.com, b@example.com\r\n",
		"Subject: Calendar invite: Test Concert\r\n",
		"Message-ID: <event-42-1@ticket-live-event-scanner>\r\n",
		"MIME-Version: 1.0\r\n",
		"Content-Type: multipart/mixed;",
		"Content-Type: multipart/alternative;",
		"Content-Type: text/calendar; method=REQUEST; charset=UTF-8",
		`Content-Type: text/calendar; method=REQUEST; charset=UTF-8; name="invite.ics"`,
		`Content-Disposition: attachment; filename="invite.ics"`,
		"Content-Transfer-Encoding: base64",
		"Your calendar invite for Test Concert is attached.",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("buildMIMEMessage() missing %q in:\n%s", want, text)
		}
	}

	// The inline text/calendar part must NOT be marked as an attachment,
	// or Gmail/Outlook won't render Accept/Decline/Maybe controls for it.
	altStart := strings.Index(text, "multipart/alternative;")
	if altStart == -1 {
		t.Fatalf("no multipart/alternative part found in:\n%s", text)
	}
	boundaryStart := strings.Index(text[altStart:], `boundary="`) + altStart + len(`boundary="`)
	boundaryEnd := strings.Index(text[boundaryStart:], `"`) + boundaryStart
	innerBoundary := text[boundaryStart:boundaryEnd]

	closing := "--" + innerBoundary + "--"
	closeIdx := strings.Index(text, closing)
	if closeIdx == -1 {
		t.Fatalf("could not find closing boundary %q in:\n%s", closing, text)
	}
	altSection := text[altStart:closeIdx]
	if strings.Contains(altSection, "Content-Disposition: attachment") {
		t.Errorf("inline calendar part inside multipart/alternative must not carry Content-Disposition: attachment:\n%s", altSection)
	}

	// The ics content should round-trip through the base64-encoded parts.
	encoded := base64.StdEncoding.EncodeToString([]byte("BEGIN:VCALENDAR\r\nMETHOD:REQUEST\r\nEND:VCALENDAR\r\n"))
	if !strings.Contains(text, encoded[:20]) {
		t.Errorf("buildMIMEMessage() missing base64-encoded ics content in:\n%s", text)
	}
}
