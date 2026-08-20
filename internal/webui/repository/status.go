package repository

import "time"

// dateLayouts are tried in order when parsing a stored event_date. The
// column is declared DATE and written as plain "2006-01-02" or
// "2006-01-02T15:04" text (the scraper includes a time when ticketline.pt
// gives one), but some SQLite driver implementations (observed with
// modernc.org/sqlite, used in tests) auto-detect DATE-typed columns and
// hand back RFC3339 on Scan instead of the raw text that was inserted.
// Trying all three keeps this correct regardless of which behavior the
// driver in use (test or production) exhibits.
var dateLayouts = []string{"2006-01-02", "2006-01-02T15:04", time.RFC3339}

// computeStatus computes whether an event is "active" or "finished" based on its event_date.
func computeStatus(eventDate *string, now time.Time) string {
	if eventDate == nil || *eventDate == "" {
		// If no event_date, assume it's active
		return "active"
	}

	var eventTime time.Time
	var parsed bool
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, *eventDate); err == nil {
			eventTime = t
			parsed = true
			break
		}
	}
	if !parsed {
		// If we can't parse, assume it's active
		return "active"
	}

	// Check if event_date >= now's date
	if eventTime.After(now) || eventTime.Format("2006-01-02") == now.Format("2006-01-02") {
		return "active"
	}

	return "finished"
}
