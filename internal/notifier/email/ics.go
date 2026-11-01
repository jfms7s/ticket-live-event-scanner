package email

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	// Embed the IANA timezone database so time.LoadLocation("Europe/Lisbon")
	// works even on minimal container images (e.g. alpine) that don't ship
	// a system tzdata package.
	_ "time/tzdata"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
)

const (
	icsDateTimeLayout = "2006-01-02T15:04"
	icsDateLayout     = "2006-01-02"
	icsUTCLayout      = "20060102T150405Z"
	icsDateOnlyLayout = "20060102"

	// defaultEventDuration is used for DTEND when only a start time is
	// known (event_date carries no end time).
	defaultEventDuration = 2 * time.Hour
)

// BuildICS renders an RFC 5545 VCALENDAR/VEVENT for p with METHOD:REQUEST
// and an ORGANIZER/ATTENDEE list, so mail clients (Gmail, Outlook, Apple
// Mail) render it as an actual calendar invitation with RSVP controls
// rather than a plain file attachment. p.EventDate is interpreted as
// Europe/Lisbon local time when it carries a time component, or as an
// all-day event when it's date-only.
func BuildICS(p event.Purchased, organizer string, attendees []string) (string, error) {
	if p.EventDate == "" {
		return "", fmt.Errorf("event %d: event_date is required to build an ics attachment", p.EventID)
	}

	loc, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		return "", fmt.Errorf("load Europe/Lisbon location: %w", err)
	}

	var dtstart, dtend string
	if start, err := time.ParseInLocation(icsDateTimeLayout, p.EventDate, loc); err == nil {
		end := start.Add(defaultEventDuration)
		dtstart = "DTSTART:" + start.UTC().Format(icsUTCLayout)
		dtend = "DTEND:" + end.UTC().Format(icsUTCLayout)
	} else if day, err := time.ParseInLocation(icsDateLayout, p.EventDate, loc); err == nil {
		dtstart = "DTSTART;VALUE=DATE:" + day.Format(icsDateOnlyLayout)
		dtend = "DTEND;VALUE=DATE:" + day.AddDate(0, 0, 1).Format(icsDateOnlyLayout)
	} else {
		return "", fmt.Errorf("event %d: invalid event_date %q: expected %q or %q", p.EventID, p.EventDate, icsDateTimeLayout, icsDateLayout)
	}

	description := p.Category
	if p.URL != "" {
		if description != "" {
			description += "\n"
		}
		description += p.URL
	}

	lines := []string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//ticket-live-event-scanner//email-notifier//EN",
		"CALSCALE:GREGORIAN",
		"METHOD:REQUEST",
		"BEGIN:VEVENT",
		fmt.Sprintf("UID:event-%d@ticket-live-event-scanner", p.EventID),
		"DTSTAMP:" + time.Now().UTC().Format(icsUTCLayout),
		dtstart,
		dtend,
		"SEQUENCE:0",
		"STATUS:CONFIRMED",
		"SUMMARY:" + icsEscape(p.Title),
	}
	if p.Venue != "" {
		lines = append(lines, "LOCATION:"+icsEscape(p.Venue))
	}
	if description != "" {
		lines = append(lines, "DESCRIPTION:"+icsEscape(description))
	}
	if p.URL != "" {
		lines = append(lines, "URL:"+icsEscape(p.URL))
	}
	if organizer != "" {
		lines = append(lines, "ORGANIZER:mailto:"+organizer)
	}
	for _, attendee := range attendees {
		lines = append(lines, "ATTENDEE;ROLE=REQ-PARTICIPANT;PARTSTAT=NEEDS-ACTION;RSVP=TRUE:mailto:"+attendee)
	}
	lines = append(lines, "END:VEVENT", "END:VCALENDAR")

	for i, line := range lines {
		lines[i] = icsFold(line)
	}
	return strings.Join(lines, "\r\n") + "\r\n", nil
}

// icsEscaper escapes TEXT values per RFC 5545 §3.3.11. Line breaks
// (CRLF or LF) become the escaped "\n"; a lone CR is dropped, so no value
// can end its content line early. "\r\n" comes before "\r" because the
// replacer tries patterns in argument order at each position.
var icsEscaper = strings.NewReplacer(
	`\`, `\\`,
	`;`, `\;`,
	`,`, `\,`,
	"\r\n", `\n`,
	"\n", `\n`,
	"\r", "",
)

// icsEscape escapes a TEXT value per RFC 5545 §3.3.11.
func icsEscape(s string) string {
	return icsEscaper.Replace(s)
}

// icsMaxLineOctets is the RFC 5545 §3.1 content-line limit, excluding the
// line break.
const icsMaxLineOctets = 75

// icsFold folds a content line longer than 75 octets per RFC 5545 §3.1:
// CRLF followed by one space, which counts towards the next line's 75
// octets. It never splits a UTF-8 sequence.
func icsFold(line string) string {
	if len(line) <= icsMaxLineOctets {
		return line
	}
	var b strings.Builder
	limit := icsMaxLineOctets
	for len(line) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(line[cut]) {
			cut--
		}
		b.WriteString(line[:cut])
		b.WriteString("\r\n ")
		line = line[cut:]
		limit = icsMaxLineOctets - 1 // the leading space uses one octet
	}
	b.WriteString(line)
	return b.String()
}
