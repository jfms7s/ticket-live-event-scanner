// Package runs is the scraper's run log: one record per hub per run, kept
// in the SCRAPER_RUNS KV bucket (streams.RunsBucketName). The scraper
// writes the records at the end of every run, failed runs included;
// web-ui-api reads them for GET /api/runs and /metrics.
//
// A key is "<hub id>.<run start, unix milliseconds>". Every record of one
// run shares the start time, so the keys ending in the newest start time
// are the newest run, one per hub it was configured with.
package runs

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Record is one hub's outcome in one run.
type Record struct {
	// RunID is shared by every record of a run: its start time in RFC 3339
	// with milliseconds.
	RunID      string    `json:"run_id"`
	HubID      int64     `json:"hub_id"`
	HubSlug    string    `json:"hub_slug"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	// SessionsSeen counts the session cards on the hub page, past ones
	// included.
	SessionsSeen int `json:"sessions_seen"`
	// FutureSessions counts the cards dated today or later (Europe/Lisbon).
	FutureSessions int `json:"future_sessions"`
	// NewEvents counts the sessions of this hub published this run.
	NewEvents int `json:"new_events"`
	// Error is why this hub failed this run; empty when it did not.
	Error string `json:"error,omitempty"`
}

// Failed reports whether the hub failed this run.
func (r Record) Failed() bool { return r.Error != "" }

// RunID returns the run ID for a run that started at start.
func RunID(start time.Time) string {
	return start.UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

// Key returns the bucket key of r.
func Key(r Record) string {
	return keyOf(r.HubID, r.StartedAt.UnixMilli())
}

func keyOf(hubID, startMS int64) string {
	return strconv.FormatInt(hubID, 10) + "." + strconv.FormatInt(startMS, 10)
}

// key is a parsed bucket key.
type key struct {
	hubID   int64
	startMS int64
}

func (k key) String() string { return keyOf(k.hubID, k.startMS) }

// parseKey parses "<hub id>.<start ms>".
func parseKey(s string) (key, error) {
	hub, start, ok := strings.Cut(s, ".")
	if !ok {
		return key{}, fmt.Errorf("run key %q: want <hub id>.<start ms>", s)
	}
	hubID, err := strconv.ParseInt(hub, 10, 64)
	if err != nil {
		return key{}, fmt.Errorf("run key %q: hub id: %w", s, err)
	}
	startMS, err := strconv.ParseInt(start, 10, 64)
	if err != nil {
		return key{}, fmt.Errorf("run key %q: start: %w", s, err)
	}
	return key{hubID: hubID, startMS: startMS}, nil
}
