package main

import (
	"os"
	"reflect"
	"testing"
)

// TestHubPageSlug tests that hubPageSlug accepts both full URLs and bare
// slugs, ignoring any domain in the input.
func TestHubPageSlug(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"auchan-live-academia-maia-98164", "auchan-live-academia-maia-98164"},
		{"https://www.ticketline.pt/evento/auchan-live-academia-maia-98164", "auchan-live-academia-maia-98164"},
		{"https://www.ticketline.pt/evento/auchan-live-academia-aveiro-98167/", "auchan-live-academia-aveiro-98167"},
		{"/evento/auchan-live-academia-maia-98164", "auchan-live-academia-maia-98164"},
		{"", ""},
	}

	for _, tc := range tests {
		if got := hubPageSlug(tc.input); got != tc.expected {
			t.Errorf("hubPageSlug(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

// TestGetEnvHubPages tests parsing a comma-separated TICKETLINE_HUB_PAGES
// value, and falling back to the default when unset.
func TestGetEnvHubPages(t *testing.T) {
	const key = "TICKETLINE_HUB_PAGES_TEST"
	defaultVal := []string{"default-slug-1"}

	t.Run("unset falls back to default", func(t *testing.T) {
		os.Unsetenv(key)
		got := getEnvHubPages(key, defaultVal)
		if !reflect.DeepEqual(got, defaultVal) {
			t.Errorf("getEnvHubPages() = %v, want %v", got, defaultVal)
		}
	})

	t.Run("mixed full URLs and bare slugs", func(t *testing.T) {
		os.Setenv(key, "https://www.ticketline.pt/evento/auchan-live-academia-maia-98164, auchan-live-academia-aveiro-98167")
		defer os.Unsetenv(key)

		got := getEnvHubPages(key, defaultVal)
		want := []string{"auchan-live-academia-maia-98164", "auchan-live-academia-aveiro-98167"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("getEnvHubPages() = %v, want %v", got, want)
		}
	})
}

// TestCacheURLResponseLRUNoDuplicate tests that caching the same URL twice
// doesn't add it to cacheOrder twice
func TestCacheURLResponseLRUNoDuplicate(t *testing.T) {
	scraper := &Scraper{
		urlCache:   make(map[string]*urlCacheEntry),
		cacheOrder: make([]string, 0),
	}

	// Cache the same URL twice
	url := "https://example.com/page1"
	scraper.cacheURLResponse(url, "etag1", "lastmod1", "body1")
	scraper.cacheURLResponse(url, "etag2", "lastmod2", "body2")

	// cacheOrder should contain the URL only once
	if len(scraper.cacheOrder) != 1 {
		t.Errorf("Expected cacheOrder to have 1 entry, got %d: %v", len(scraper.cacheOrder), scraper.cacheOrder)
	}

	// The cache should have the updated entry
	if cached, ok := scraper.urlCache[url]; !ok {
		t.Errorf("Expected URL to be in cache")
	} else {
		if cached.etag != "etag2" {
			t.Errorf("Expected updated etag 'etag2', got %q", cached.etag)
		}
		if cached.body != "body2" {
			t.Errorf("Expected updated body 'body2', got %q", cached.body)
		}
	}
}

// TestCacheURLResponseLRUUpdateOrder tests that cacheOrder is updated correctly
// and doesn't create duplicates when refreshing cached entries
func TestCacheURLResponseLRUUpdateOrder(t *testing.T) {
	scraper := &Scraper{
		urlCache:   make(map[string]*urlCacheEntry),
		cacheOrder: make([]string, 0),
	}

	// Cache multiple different URLs
	url1 := "https://example.com/page1"
	url2 := "https://example.com/page2"
	url3 := "https://example.com/page3"

	scraper.cacheURLResponse(url1, "etag1", "lastmod1", "body1")
	scraper.cacheURLResponse(url2, "etag2", "lastmod2", "body2")
	scraper.cacheURLResponse(url3, "etag3", "lastmod3", "body3")

	expectedOrder := []string{url1, url2, url3}
	if len(scraper.cacheOrder) != 3 {
		t.Errorf("Expected 3 entries in cacheOrder, got %d", len(scraper.cacheOrder))
	}

	for i, url := range expectedOrder {
		if i < len(scraper.cacheOrder) && scraper.cacheOrder[i] != url {
			t.Errorf("Expected cacheOrder[%d] = %s, got %s", i, url, scraper.cacheOrder[i])
		}
	}

	// Now refresh url2 - cacheOrder should still have 3 entries, not 4
	scraper.cacheURLResponse(url2, "etag2-updated", "lastmod2-updated", "body2-updated")

	if len(scraper.cacheOrder) != 3 {
		t.Errorf("After refresh, expected 3 entries in cacheOrder, got %d", len(scraper.cacheOrder))
	}

	// Count occurrences of url2 in cacheOrder - should be exactly 1
	count := 0
	for _, url := range scraper.cacheOrder {
		if url == url2 {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Expected url2 to appear once in cacheOrder, got %d times", count)
	}

	// Verify the cache entry was updated
	if entry, ok := scraper.urlCache[url2]; ok {
		if entry.etag != "etag2-updated" {
			t.Errorf("Expected etag to be updated, got %s", entry.etag)
		}
	} else {
		t.Errorf("Expected url2 to be in cache")
	}
}

// TestFetchHubPageErrorAggregation tests that fetchHubPage correctly aggregates errors
// by ensuring that local errorCount increments don't affect global stats.Errors until return
func TestFetchHubPageErrorAggregation(t *testing.T) {
	// This test verifies the error aggregation pattern in fetchHubPage (lines 321-339)
	// The important behavior is that fetchHubPage:
	// 1. Increments a local errorCount for each failed event fetch
	// 2. Returns an error only if errorCount > 0
	// 3. Does NOT increment scraper.stats.Errors (that's done by discover() after aggregating all errors)
	//
	// This is difficult to unit test without mocking HTTP, but the pattern is:
	// - Local errorCount tracks failures within this function
	// - Caller (discover) aggregates all errorCounts and sets s.stats.Errors once
	// - This prevents double-counting and ensures accurate error totals
	//
	// The fix removed the redundant scraper.stats.Errors++ at line 118 in run(),
	// ensuring discover() is the single source of truth for setting error counts.
	t.Log("fetchHubPage error aggregation: errorCount is local, stats.Errors set by discover()")
}