// Package fetch performs rate-limited, cached HTTP GETs against
// ticketline.pt, restricted to a small set of allowed paths.
package fetch

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// maxCacheEntries limits the ETag/Last-Modified cache to 200 entries
// (~5-10 runs worth).
const maxCacheEntries = 200

// maxBodySize limits response bodies to 5MB to prevent unbounded memory
// consumption.
const maxBodySize = 5 * 1024 * 1024

type cacheEntry struct {
	etag         string
	lastModified string
	body         string
}

// Fetcher performs conditional GETs (ETag/Last-Modified) against a
// restricted set of allowed paths, caching successful responses so a 304
// can be served from memory instead of re-downloading the page.
type Fetcher struct {
	client     *http.Client
	userAgent  string
	cache      map[string]*cacheEntry
	cacheOrder []string // insertion order, for LRU eviction
}

// New constructs a Fetcher that sends the given User-Agent and times out
// requests after timeout.
func New(userAgent string, timeout time.Duration) *Fetcher {
	return &Fetcher{
		client:     &http.Client{Timeout: timeout},
		userAgent:  userAgent,
		cache:      make(map[string]*cacheEntry),
		cacheOrder: make([]string, 0),
	}
}

// Fetch retrieves urlStr, which must resolve under baseURL to one of the
// allowed paths (see validateURL). A cached ETag/Last-Modified is sent if
// present, and a 304 response returns the previously cached body.
func (f *Fetcher) Fetch(ctx context.Context, urlStr, baseURL string) (string, error) {
	if err := validateURL(urlStr, baseURL); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", f.userAgent)

	// Apply cached ETag/Last-Modified headers for conditional GET
	if cached, ok := f.cache[urlStr]; ok {
		if cached.etag != "" {
			req.Header.Set("If-None-Match", cached.etag)
		}
		if cached.lastModified != "" {
			req.Header.Set("If-Modified-Since", cached.lastModified)
		}
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Handle 304 Not Modified by returning cached content
	if resp.StatusCode == http.StatusNotModified {
		if cached, ok := f.cache[urlStr]; ok {
			log.Printf("Cache hit (304) for %s", urlStr)
			return cached.body, nil
		}
		// Shouldn't happen, but fall through to error
		return "", fmt.Errorf("got 304 but no cached content for %s", urlStr)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	limitedBody := io.LimitReader(resp.Body, int64(maxBodySize)+1)
	body, err := io.ReadAll(limitedBody)
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}

	if len(body) > maxBodySize {
		return "", fmt.Errorf("response body exceeds maximum size of %d bytes", maxBodySize)
	}

	f.cacheResponse(urlStr, resp.Header.Get("ETag"), resp.Header.Get("Last-Modified"), string(body))

	return string(body), nil
}

func (f *Fetcher) cacheResponse(url, etag, lastModified, body string) {
	// Check if this is a new URL (not already in cache)
	isNewURL := true
	if _, exists := f.cache[url]; exists {
		isNewURL = false
	}

	// If at capacity and this is a new URL, remove oldest entry
	if isNewURL && len(f.cache) >= maxCacheEntries {
		if len(f.cacheOrder) > 0 {
			oldest := f.cacheOrder[0]
			delete(f.cache, oldest)
			f.cacheOrder = f.cacheOrder[1:]
		}
	}

	// Add or update entry
	f.cache[url] = &cacheEntry{
		etag:         etag,
		lastModified: lastModified,
		body:         body,
	}

	// Only append to cacheOrder if this is a new URL
	if isNewURL {
		f.cacheOrder = append(f.cacheOrder, url)
	}
}
