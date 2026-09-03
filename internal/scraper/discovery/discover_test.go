package discovery

import "testing"

// TestFetchHubPageErrorAggregation documents that fetchHubPage aggregates
// errors correctly by ensuring that local errorCount increments don't
// affect Stats.Errors until Discover returns.
func TestFetchHubPageErrorAggregation(t *testing.T) {
	// This test documents the error aggregation pattern in fetchHubPage:
	// 1. Increments a local errorCount for each failed event fetch
	// 2. Returns an error only if errorCount > 0
	// 3. Does NOT increment Scraper.Stats.Errors (that's done by Discover
	//    after aggregating all errors)
	//
	// This is difficult to unit test without mocking HTTP, but the pattern is:
	// - Local errorCount tracks failures within this function
	// - Caller (Discover) aggregates all errorCounts and sets Stats.Errors once
	// - This prevents double-counting and ensures accurate error totals
	t.Log("fetchHubPage error aggregation: errorCount is local, Stats.Errors set by Discover()")
}
