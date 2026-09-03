package email

import (
	"fmt"
	"strings"
	"time"

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

// BuildICS renders a minimal RFC 5545 VCALENDAR/VEVENT for p. p.EventDate
// is interpreted as Europe/Lisbon local time when it carries a time
// component, or as an all-day event when it's date-only.
func BuildICS(p event.Purchased) (string, error) {
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
		"BEGIN:VEVENT",
		fmt.Sprintf("UID:event-%d@ticket-live-event-scanner", p.EventID),
		"DTSTAMP:" + time.Now().UTC().Format(icsUTCLayout),
		dtstart,
		dtend,
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
	lines = append(lines, "END:VEVENT", "END:VCALENDAR")

	return strings.Join(lines, "\r\n") + "\r\n", nil
}

// icsEscape escapes TEXT values per RFC 5545 §3.3.11.
func icsEscape(s string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		`;`, `\;`,
		`,`, `\,`,
		"\n", `\n`,
	)
	return replacer.Replace(s)
}
