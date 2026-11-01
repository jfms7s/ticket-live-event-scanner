package email

import (
	"encoding/base64"
	"mime"
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

// headerBlock returns the top-level header lines of msg (up to the first
// blank line).
func headerBlock(t *testing.T, msg []byte) []string {
	t.Helper()
	text := string(msg)
	end := strings.Index(text, "\r\n\r\n")
	if end == -1 {
		t.Fatalf("no blank line ending the header block in:\n%s", text)
	}
	return strings.Split(text[:end], "\r\n")
}

func TestBuildMIMEMessageRejectsHeaderInjection(t *testing.T) {
	msg := buildMIMEMessage(
		"noreply@example.com\r\nBcc: from@example.com",
		[]string{"a@example.com\nBcc: to@example.com"},
		"Calendar invite: Evil\r\nBcc: attacker@example.com",
		"<id@x>\r\nBcc: id@example.com",
		"body",
		"BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n",
	)

	subjects := 0
	for _, line := range headerBlock(t, msg) {
		if strings.HasPrefix(line, "Subject:") {
			subjects++
		}
		if strings.HasPrefix(strings.ToLower(line), "bcc:") {
			t.Errorf("injected header line %q in:\n%s", line, msg)
		}
		if strings.ContainsAny(line, "\r\n") {
			t.Errorf("header line %q contains a bare CR or LF", line)
		}
	}
	if subjects != 1 {
		t.Errorf("got %d Subject: lines, want 1:\n%s", subjects, msg)
	}
}

func TestBuildMIMEMessageEncodesNonASCIISubject(t *testing.T) {
	msg := buildMIMEMessage(
		"noreply@example.com",
		[]string{"a@example.com"},
		"Calendar invite: Concerto de Verão",
		"<id@x>",
		"body",
		"BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n",
	)

	var subject string
	for _, line := range headerBlock(t, msg) {
		if strings.HasPrefix(line, "Subject:") {
			subject = line
		}
	}
	if !strings.Contains(subject, "=?utf-8?q?") {
		t.Errorf("Subject not RFC 2047 encoded: %q", subject)
	}
	for _, r := range subject {
		if r > 127 {
			t.Fatalf("Subject header carries raw non-ASCII: %q", subject)
		}
	}
	dec := new(mime.WordDecoder)
	got, err := dec.DecodeHeader(strings.TrimPrefix(subject, "Subject: "))
	if err != nil || got != "Calendar invite: Concerto de Verão" {
		t.Errorf("decoded subject = %q, %v", got, err)
	}
}
