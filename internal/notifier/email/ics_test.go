package email

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
)

func TestBuildICS(t *testing.T) {
	t.Run("date and time", func(t *testing.T) {
		p := event.Purchased{
			EventID:   42,
			Title:     "Test Concert",
			Venue:     "Coliseu",
			Category:  "Music",
			EventDate: "2026-03-15T21:00",
			URL:       "https://example.com/event/42",
		}

		ics, err := BuildICS(p, "noreply@example.com", []string{"a@example.com", "b@example.com"})
		if err != nil {
			t.Fatalf("BuildICS() error = %v", err)
		}

		// 21:00 Europe/Lisbon in March (WET/UTC+0 before DST switch on 2026-03-29) -> 21:00Z
		if !strings.Contains(ics, "DTSTART:20260315T210000Z") {
			t.Errorf("expected DTSTART:20260315T210000Z, got:\n%s", ics)
		}
		if !strings.Contains(ics, "DTEND:20260315T230000Z") {
			t.Errorf("expected DTEND:20260315T230000Z (2h default duration), got:\n%s", ics)
		}
		if !strings.Contains(ics, "UID:event-42@ticket-live-event-scanner") {
			t.Errorf("expected stable UID, got:\n%s", ics)
		}
		if !strings.Contains(ics, "SUMMARY:Test Concert") {
			t.Errorf("expected SUMMARY, got:\n%s", ics)
		}
		if !strings.Contains(ics, "LOCATION:Coliseu") {
			t.Errorf("expected LOCATION, got:\n%s", ics)
		}
		if !strings.Contains(ics, "METHOD:REQUEST") {
			t.Errorf("expected METHOD:REQUEST so clients render it as an invite, got:\n%s", ics)
		}
		if !strings.Contains(ics, "ORGANIZER:mailto:noreply@example.com") {
			t.Errorf("expected ORGANIZER, got:\n%s", ics)
		}
		if !strings.Contains(icsUnfold(ics), "ATTENDEE;ROLE=REQ-PARTICIPANT;PARTSTAT=NEEDS-ACTION;RSVP=TRUE:mailto:a@example.com") {
			t.Errorf("expected ATTENDEE for a@example.com, got:\n%s", ics)
		}
		if !strings.Contains(icsUnfold(ics), "ATTENDEE;ROLE=REQ-PARTICIPANT;PARTSTAT=NEEDS-ACTION;RSVP=TRUE:mailto:b@example.com") {
			t.Errorf("expected ATTENDEE for b@example.com, got:\n%s", ics)
		}
	})

	t.Run("date only becomes all-day event", func(t *testing.T) {
		p := event.Purchased{
			EventID:   7,
			Title:     "All Day Fest",
			EventDate: "2026-06-01",
			URL:       "https://example.com/event/7",
		}

		ics, err := BuildICS(p, "noreply@example.com", []string{"a@example.com"})
		if err != nil {
			t.Fatalf("BuildICS() error = %v", err)
		}

		if !strings.Contains(ics, "DTSTART;VALUE=DATE:20260601") {
			t.Errorf("expected all-day DTSTART, got:\n%s", ics)
		}
		if !strings.Contains(ics, "DTEND;VALUE=DATE:20260602") {
			t.Errorf("expected exclusive-end DTEND on the following day, got:\n%s", ics)
		}
	})

	t.Run("missing event date is an error", func(t *testing.T) {
		p := event.Purchased{EventID: 1, Title: "No Date"}
		if _, err := BuildICS(p, "noreply@example.com", []string{"a@example.com"}); err == nil {
			t.Error("expected error for missing event_date, got nil")
		}
	})

	t.Run("invalid event date is an error", func(t *testing.T) {
		p := event.Purchased{EventID: 1, Title: "Bad Date", EventDate: "not-a-date"}
		if _, err := BuildICS(p, "noreply@example.com", []string{"a@example.com"}); err == nil {
			t.Error("expected error for invalid event_date, got nil")
		}
	})

	t.Run("special characters are escaped", func(t *testing.T) {
		p := event.Purchased{
			EventID:   1,
			Title:     "Rock, Pop; Jazz\nSpecial",
			EventDate: "2026-01-01",
		}

		ics, err := BuildICS(p, "noreply@example.com", []string{"a@example.com"})
		if err != nil {
			t.Fatalf("BuildICS() error = %v", err)
		}

		if !strings.Contains(ics, `SUMMARY:Rock\, Pop\; Jazz\nSpecial`) {
			t.Errorf("expected escaped SUMMARY, got:\n%s", ics)
		}
	})

	t.Run("no attendees still produces a valid invite", func(t *testing.T) {
		p := event.Purchased{EventID: 1, Title: "Solo", EventDate: "2026-01-01"}
		ics, err := BuildICS(p, "noreply@example.com", nil)
		if err != nil {
			t.Fatalf("BuildICS() error = %v", err)
		}
		if strings.Contains(ics, "ATTENDEE") {
			t.Errorf("expected no ATTENDEE lines when attendees is empty, got:\n%s", ics)
		}
	})
}

func TestBuildICSNoBareCRAndFoldedLines(t *testing.T) {
	title := "Evil\rSUMMARY:x\r\nDTSTART:19700101T000000Z " + strings.Repeat("Ã", 100)
	p := event.Purchased{
		EventID:   7,
		Title:     title,
		Venue:     "Coliseu\r\nLOCATION:elsewhere",
		EventDate: "2026-06-15T21:00",
		URL:       "https://ticketline.pt/evento/x-7",
	}
	ics, err := BuildICS(p, "noreply@example.com", []string{"a@example.com"})
	if err != nil {
		t.Fatalf("BuildICS() error = %v", err)
	}

	if !strings.HasSuffix(ics, "\r\n") {
		t.Fatalf("ics does not end with CRLF")
	}
	for i, line := range strings.Split(strings.TrimSuffix(ics, "\r\n"), "\r\n") {
		if strings.ContainsAny(line, "\r\n") {
			t.Errorf("line %d has a bare CR or LF: %q", i, line)
		}
		if len(line) > 75 {
			t.Errorf("line %d is %d octets, want <= 75: %q", i, len(line), line)
		}
		if !utf8.ValidString(line) {
			t.Errorf("line %d splits a UTF-8 sequence: %q", i, line)
		}
	}

	// Unfolding (RFC 5545 §3.1) must give back exactly one SUMMARY,
	// LOCATION and DTSTART property.
	unfolded := icsUnfold(ics)
	for _, prop := range []string{"SUMMARY:", "LOCATION:", "DTSTART:"} {
		n := 0
		for _, line := range strings.Split(unfolded, "\r\n") {
			if strings.HasPrefix(line, prop) {
				n++
			}
		}
		if n != 1 {
			t.Errorf("got %d %s lines after unfolding, want 1:\n%s", n, prop, ics)
		}
	}
	if !strings.Contains(unfolded, `SUMMARY:EvilSUMMARY:x\nDTSTART:19700101T000000Z `+strings.Repeat("Ã", 100)) {
		t.Errorf("unfolded SUMMARY not as expected:\n%s", unfolded)
	}
}

// icsUnfold reverses RFC 5545 §3.1 line folding (the ATTENDEE lines are
// longer than 75 octets, so they arrive folded).
func icsUnfold(ics string) string {
	return strings.ReplaceAll(ics, "\r\n ", "")
}
