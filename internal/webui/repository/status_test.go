package repository

import (
	"testing"
	"time"
)

// TestComputeStatusEventDateWithTime verifies computeStatus can parse the
// "YYYY-MM-DDTHH:MM" event_date format the scraper stores for hub sessions
// that have a fixed start time (no seconds/timezone offset), not just plain
// "YYYY-MM-DD".
func TestComputeStatusEventDateWithTime(t *testing.T) {
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)

	futureToday := "2026-09-04T18:30"
	if status := computeStatus(&futureToday, now); status != "active" {
		t.Errorf("computeStatus(%q) = %q, want %q", futureToday, status, "active")
	}

	past := "2026-09-03T18:30"
	if status := computeStatus(&past, now); status != "finished" {
		t.Errorf("computeStatus(%q) = %q, want %q", past, status, "finished")
	}
}
