package discovery

import (
	"errors"
	"time"
	"unicode/utf8"

	"github.com/jfms7s/ticket-live-event-scanner/internal/runs"
	"github.com/jfms7s/ticket-live-event-scanner/internal/scraper/parse"
)

// maxErrorLen bounds a recorded error message: the run log is for a status
// line, not for stack traces.
const maxErrorLen = 500

// RunRecords turns a run's per-hub stats into run records, one for every
// configured hub. A hub the run never reached (it failed or was cancelled
// before getting there) gets runErr as its error, so every record of the
// newest run says something about every hub.
func RunRecords(hubPages []string, hubs []HubStats, start, finish time.Time, runErr error) []runs.Record {
	reached := make(map[string]HubStats, len(hubs))
	for _, h := range hubs {
		reached[h.Slug] = h
	}
	runID := runs.RunID(start)

	records := make([]runs.Record, 0, len(hubPages))
	keyed := map[int64]bool{}
	for _, slug := range hubPages {
		h, ok := reached[slug]
		if !ok {
			h = HubStats{Slug: slug, ID: parse.ExtractIDFromSlug(slug), Err: runErr}
			if h.Err == nil {
				h.Err = errNotReached
			}
		}
		// The key is the hub ID: a hub listed twice, or a second slug
		// without an ID (both ID 0), would overwrite the first record.
		// Keep the first; the run's error and log name the others.
		if keyed[h.ID] {
			continue
		}
		keyed[h.ID] = true
		r := runs.Record{
			RunID:          runID,
			HubID:          h.ID,
			HubSlug:        h.Slug,
			StartedAt:      start,
			FinishedAt:     finish,
			SessionsSeen:   h.SessionsSeen,
			FutureSessions: h.FutureSessions,
			NewEvents:      h.NewEvents,
		}
		if h.Err != nil {
			r.Error = truncate(h.Err.Error(), maxErrorLen)
		}
		records = append(records, r)
	}
	return records
}

// errNotReached is the error of a hub a run that didn't fail never got to;
// it shouldn't happen, but a record without an error would read as a
// success.
var errNotReached = errors.New("hub page not scraped this run")

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Cut on a rune boundary.
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}
