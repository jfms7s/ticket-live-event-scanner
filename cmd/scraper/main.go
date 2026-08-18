package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/store"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type Config struct {
	NatsURL          string
	TursoDatabaseURL string
	TursoAuthToken   string
	UserAgent        string
	RequestDelayMS   int
	HubPages         []string
}

// defaultHubPages are the venue/series hub pages tracked when
// TICKETLINE_HUB_PAGES is not set (design.md §6.1).
var defaultHubPages = []string{}

type Scraper struct {
	config     Config
	client     *http.Client
	db         *sql.DB
	js         jetstream.JetStream
	knownIDs   map[int64]bool
	stats      Stats
	urlCache   map[string]*urlCacheEntry // Cache ETags and Last-Modified headers
	cacheOrder []string                  // Track insertion order for LRU eviction
}

type urlCacheEntry struct {
	etag         string
	lastModified string
	body         string
}

const maxCacheEntries = 200 // Limit cache to 200 entries (~5-10 runs worth)

type Stats struct {
	EventsFound     int
	EventsPublished int
	Errors          int
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	cfg := loadConfig()

	if err := run(ctx, cfg); err != nil {
		log.Printf("ERROR: %v", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg Config) error {
	// Initialize Turso connection
	db, err := store.Connect(cfg.TursoDatabaseURL, cfg.TursoAuthToken)
	if err != nil {
		// Wrap error but avoid exposing auth token in error messages
		return fmt.Errorf("connect to turso (check credentials): %w", err)
	}
	defer db.Close()

	// Initialize NATS connection
	nc, err := nats.Connect(cfg.NatsURL)
	if err != nil {
		return fmt.Errorf("connect to nats: %w", err)
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("create jetstream context: %w", err)
	}

	// Ensure streams exist
	if err := streams.EnsureStreams(ctx, js); err != nil {
		return fmt.Errorf("ensure streams: %w", err)
	}

	// Create scraper instance
	scraper := &Scraper{
		config:     cfg,
		db:         db,
		js:         js,
		knownIDs:   make(map[int64]bool),
		urlCache:   make(map[string]*urlCacheEntry),
		cacheOrder: make([]string, 0),
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}

	// Load known event IDs from database
	if err := scraper.loadKnownIDs(ctx); err != nil {
		return fmt.Errorf("load known event IDs: %w", err)
	}

	// Discover events
	if err := scraper.discover(ctx); err != nil {
		log.Printf("Discovery encountered errors: %v", err)
		return fmt.Errorf("discovery failed: %w", err)
	}

	// Log summary
	log.Printf("Scraper finished: found=%d published=%d errors=%d",
		scraper.stats.EventsFound,
		scraper.stats.EventsPublished,
		scraper.stats.Errors)

	return nil
}

func loadConfig() Config {
	cfg := Config{
		NatsURL:          getEnv("NATS_URL", "nats://localhost:4222"),
		TursoDatabaseURL: getEnvRequired("TURSO_DATABASE_URL"),
		TursoAuthToken:   getEnvRequired("TURSO_AUTH_TOKEN"),
		UserAgent:        getEnv("USER_AGENT", "ticket-live-event-scanner/0.1 (personal project; contact: jfms7s@gmail.com)"),
		RequestDelayMS:   getEnvInt("REQUEST_DELAY_MS", 1500),
		HubPages:         getEnvHubPages("TICKETLINE_HUB_PAGES", defaultHubPages),
	}
	return cfg
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvRequired(key string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	log.Fatalf("required environment variable not set: %s", key)
	return ""
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

// getEnvHubPages reads a comma-separated list of hub pages from the given
// env var, accepting either full URLs or bare slugs (see hubPageSlug).
// Falls back to defaultVal if the env var is unset or contains no usable
// entries.
func getEnvHubPages(key string, defaultVal []string) []string {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}

	var slugs []string
	for _, raw := range strings.Split(val, ",") {
		if slug := hubPageSlug(strings.TrimSpace(raw)); slug != "" {
			slugs = append(slugs, slug)
		}
	}
	if len(slugs) == 0 {
		return defaultVal
	}
	return slugs
}

// hubPageSlug extracts the bare "/evento/{slug}" slug from either a full
// hub page URL (e.g. "https://www.ticketline.pt/evento/auchan-live-academia-maia-98164")
// or an already-bare slug. Any domain in the input is ignored — hub pages
// are always fetched against "https://www.ticketline.pt", so a caller can't point
// the scraper at an arbitrary host via this setting.
func hubPageSlug(raw string) string {
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		raw = u.Path
	}
	raw = strings.Trim(raw, "/")
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "/")
	return parts[len(parts)-1]
}

func (s *Scraper) loadKnownIDs(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM events")
	if err != nil {
		return fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scan event id: %w", err)
		}
		s.knownIDs[id] = true
	}

	// Check for errors BEFORE logging success
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate events: %w", err)
	}

	log.Printf("Loaded %d known event IDs from database", len(s.knownIDs))
	return nil
}

func (s *Scraper) discover(ctx context.Context) error {
	var errorCount int

	// Discover events from the tracked hub pages only (design §6.1) — no
	// site-wide /agenda or /pesquisa crawling.
	for _, hubSlug := range s.config.HubPages {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Extract event ID from hub page slug
		hubID := extractIDFromSlug(hubSlug)
		if hubID == 0 {
			continue
		}

		// Rate limit
		time.Sleep(time.Duration(s.config.RequestDelayMS) * time.Millisecond)

		if err := s.fetchHubPage(ctx, hubSlug, hubID); err != nil {
			log.Printf("Error fetching hub page %s: %v", hubSlug, err)
			errorCount++
		}
	}

	if errorCount > 0 {
		s.stats.Errors = errorCount
		return fmt.Errorf("discovery completed with %d error(s)", errorCount)
	}

	return nil
}

// extractIDFromSlug returns the trailing numeric event ID from a bare
// slug such as "auchan-live-academia-maia-98164" (no leading path).
func extractIDFromSlug(slug string) int64 {
	lastHyphen := strings.LastIndex(slug, "-")
	if lastHyphen <= 0 {
		return 0
	}
	id, err := strconv.ParseInt(slug[lastHyphen+1:], 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// fetchHubPage fetches a venue/series hub page (design.md §4/§6.1 — the two
// example URLs are hub pages, not single events) and discovers new session
// links on it using the same schema.org/Event card parsing as search pages.
func (s *Scraper) fetchHubPage(ctx context.Context, hubSlug string, hubID int64) error {
	hubURL := fmt.Sprintf("%s/evento/%s", "https://www.ticketline.pt", hubSlug)
	body, err := s.fetchURL(ctx, hubURL)
	if err != nil {
		return fmt.Errorf("fetch hub page %d: %w", hubID, err)
	}

	sessions, err := parseSearchPage(body)
	if err != nil {
		return fmt.Errorf("parse hub page %d: %w", hubID, err)
	}

	var errorCount int
	for _, sess := range sessions {
		s.stats.EventsFound++

		if s.knownIDs[sess.EventID] {
			continue
		}

		time.Sleep(time.Duration(s.config.RequestDelayMS) * time.Millisecond)

		if err := s.fetchAndPublishEvent(ctx, sess); err != nil {
			log.Printf("Error fetching detail for hub session event %d: %v", sess.EventID, err)
			errorCount++
		}
	}

	if errorCount > 0 {
		return fmt.Errorf("hub page %d: %d error(s) fetching events", hubID, errorCount)
	}

	return nil
}

func (s *Scraper) fetchAndPublishEvent(ctx context.Context, basicEvent event.Discovered) error {
	// Construct full absolute URL for detail page
	detailURL := fmt.Sprintf("%s/evento/%s", "https://www.ticketline.pt", basicEvent.Slug)
	body, err := s.fetchURL(ctx, detailURL)
	if err != nil {
		return err
	}

	// Parse detail page
	detailEvent, err := parseEventDetail(body, basicEvent.EventID)
	if err != nil {
		// Log and skip events with invalid dates; return other errors
		if strings.Contains(err.Error(), "invalid event date") {
			log.Printf("Skipping event %d: %v", basicEvent.EventID, err)
			return nil
		}
		return err
	}

	// Ensure URL is absolute (override any relative URL from parsing)
	if detailEvent.URL == "" {
		detailEvent.URL = detailURL
	} else if !strings.HasPrefix(detailEvent.URL, "http") {
		// If URL is relative, make it absolute
		detailEvent.URL = "https://www.ticketline.pt" + detailEvent.URL
	}

	// Detail pages don't self-link via itemprop="url", so parseEventDetail
	// can't derive a slug from the page body — fall back to the slug we
	// already know from the hub/search card that pointed us here.
	if detailEvent.Slug == "" {
		detailEvent.Slug = basicEvent.Slug
	}

	// Belt-and-braces: if some detail page variant lacks both the
	// microdata image and the header thumb anchor parseEventDetail looks
	// for, fall back to the poster already seen on the hub/search card.
	if detailEvent.ImageURL == "" {
		detailEvent.ImageURL = basicEvent.ImageURL
	}

	// Set reason
	detailEvent.Reason = event.ReasonDiscovered

	// Validate required fields
	if detailEvent.Title == "" {
		return fmt.Errorf("event %d missing required field: title", basicEvent.EventID)
	}
	if detailEvent.URL == "" {
		return fmt.Errorf("event %d missing required field: url", basicEvent.EventID)
	}
	if detailEvent.Slug == "" {
		return fmt.Errorf("event %d missing required field: slug", basicEvent.EventID)
	}

	// Publish to NATS
	data, err := json.Marshal(detailEvent)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	_, err = s.js.Publish(ctx, streams.EventsSubject, data,
		jetstream.WithMsgID(strconv.FormatInt(basicEvent.EventID, 10)))
	if err != nil {
		return fmt.Errorf("publish event: %w", err)
	}

	// Mark as known immediately so this run doesn't re-fetch/re-publish it
	// if the same event ID turns up again later in this same discovery pass
	// (e.g. the same hub session appearing across multiple month searches).
	s.knownIDs[basicEvent.EventID] = true

	s.stats.EventsPublished++
	log.Printf("Published event %d: %s", basicEvent.EventID, detailEvent.Title)
	return nil
}

func (s *Scraper) fetchURL(ctx context.Context, urlStr string) (string, error) {
	// Validate URL is within allowed paths
	if err := validateURL(urlStr, "https://www.ticketline.pt"); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, "GET", urlStr, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", s.config.UserAgent)

	// Apply cached ETag/Last-Modified headers for conditional GET
	if cached, ok := s.urlCache[urlStr]; ok {
		if cached.etag != "" {
			req.Header.Set("If-None-Match", cached.etag)
		}
		if cached.lastModified != "" {
			req.Header.Set("If-Modified-Since", cached.lastModified)
		}
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Handle 304 Not Modified by returning cached content
	if resp.StatusCode == http.StatusNotModified {
		if cached, ok := s.urlCache[urlStr]; ok {
			log.Printf("Cache hit (304) for %s", urlStr)
			return cached.body, nil
		}
		// Shouldn't happen, but fall through to error
		return "", fmt.Errorf("got 304 but no cached content for %s", urlStr)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Limit response body to 5MB to prevent unbounded memory consumption
	const maxBodySize = 5 * 1024 * 1024
	limitedBody := io.LimitReader(resp.Body, int64(maxBodySize)+1)
	body, err := io.ReadAll(limitedBody)
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}

	if len(body) > maxBodySize {
		return "", fmt.Errorf("response body exceeds maximum size of %d bytes", maxBodySize)
	}

	// Cache the response headers for future requests with LRU eviction
	s.cacheURLResponse(urlStr, resp.Header.Get("ETag"), resp.Header.Get("Last-Modified"), string(body))

	return string(body), nil
}

func (s *Scraper) cacheURLResponse(url, etag, lastModified, body string) {
	// Check if this is a new URL (not already in cache)
	isNewURL := true
	if _, exists := s.urlCache[url]; exists {
		isNewURL = false
	}

	// If at capacity and this is a new URL, remove oldest entry
	if isNewURL && len(s.urlCache) >= maxCacheEntries {
		if len(s.cacheOrder) > 0 {
			oldest := s.cacheOrder[0]
			delete(s.urlCache, oldest)
			s.cacheOrder = s.cacheOrder[1:]
		}
	}

	// Add or update entry
	s.urlCache[url] = &urlCacheEntry{
		etag:         etag,
		lastModified: lastModified,
		body:         body,
	}

	// Only append to cacheOrder if this is a new URL
	if isNewURL {
		s.cacheOrder = append(s.cacheOrder, url)
	}
}

func validateURL(urlStr, baseURL string) error {
	u, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	base, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("invalid base URL: %w", err)
	}

	// Check domain matches
	if u.Host != base.Host {
		return fmt.Errorf("URL host mismatch: %s vs %s", u.Host, base.Host)
	}

	// Check path is in allowed list
	path := u.Path
	allowedPrefixes := []string{"/agenda", "/pesquisa", "/evento"}
	allowed := false
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(path, prefix) {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("path not allowed: %s", path)
	}

	return nil
}
