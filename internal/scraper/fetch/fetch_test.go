package fetch

import "testing"

// TestCacheResponseLRUNoDuplicate tests that caching the same URL twice
// doesn't add it to cacheOrder twice.
func TestCacheResponseLRUNoDuplicate(t *testing.T) {
	f := &Fetcher{
		cache:      make(map[string]*cacheEntry),
		cacheOrder: make([]string, 0),
	}

	// Cache the same URL twice
	url := "https://example.com/page1"
	f.cacheResponse(url, "etag1", "lastmod1", "body1")
	f.cacheResponse(url, "etag2", "lastmod2", "body2")

	// cacheOrder should contain the URL only once
	if len(f.cacheOrder) != 1 {
		t.Errorf("Expected cacheOrder to have 1 entry, got %d: %v", len(f.cacheOrder), f.cacheOrder)
	}

	// The cache should have the updated entry
	if cached, ok := f.cache[url]; !ok {
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

// TestCacheResponseLRUUpdateOrder tests that cacheOrder is updated correctly
// and doesn't create duplicates when refreshing cached entries.
func TestCacheResponseLRUUpdateOrder(t *testing.T) {
	f := &Fetcher{
		cache:      make(map[string]*cacheEntry),
		cacheOrder: make([]string, 0),
	}

	// Cache multiple different URLs
	url1 := "https://example.com/page1"
	url2 := "https://example.com/page2"
	url3 := "https://example.com/page3"

	f.cacheResponse(url1, "etag1", "lastmod1", "body1")
	f.cacheResponse(url2, "etag2", "lastmod2", "body2")
	f.cacheResponse(url3, "etag3", "lastmod3", "body3")

	expectedOrder := []string{url1, url2, url3}
	if len(f.cacheOrder) != 3 {
		t.Errorf("Expected 3 entries in cacheOrder, got %d", len(f.cacheOrder))
	}

	for i, url := range expectedOrder {
		if i < len(f.cacheOrder) && f.cacheOrder[i] != url {
			t.Errorf("Expected cacheOrder[%d] = %s, got %s", i, url, f.cacheOrder[i])
		}
	}

	// Now refresh url2 - cacheOrder should still have 3 entries, not 4
	f.cacheResponse(url2, "etag2-updated", "lastmod2-updated", "body2-updated")

	if len(f.cacheOrder) != 3 {
		t.Errorf("After refresh, expected 3 entries in cacheOrder, got %d", len(f.cacheOrder))
	}

	// Count occurrences of url2 in cacheOrder - should be exactly 1
	count := 0
	for _, url := range f.cacheOrder {
		if url == url2 {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Expected url2 to appear once in cacheOrder, got %d times", count)
	}

	// Verify the cache entry was updated
	if entry, ok := f.cache[url2]; ok {
		if entry.etag != "etag2-updated" {
			t.Errorf("Expected etag to be updated, got %s", entry.etag)
		}
	} else {
		t.Errorf("Expected url2 to be in cache")
	}
}
