package discovery

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/scraper/parse"
)

// Discover fetches every tracked hub page (design §6.1 — no site-wide
// /agenda or /pesquisa crawling) and publishes any new sessions found on
// them.
func (s *Scraper) Discover(ctx context.Context) error {
	var errorCount int

	for _, hubSlug := range s.cfg.HubPages {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Extract event ID from hub page slug
		hubID := parse.ExtractIDFromSlug(hubSlug)
		if hubID == 0 {
			continue
		}

		// Rate limit
		time.Sleep(time.Duration(s.cfg.RequestDelayMS) * time.Millisecond)

		if err := s.fetchHubPage(ctx, hubSlug, hubID); err != nil {
			log.Printf("Error fetching hub page %s: %v", hubSlug, err)
			errorCount++
		}
	}

	if errorCount > 0 {
		s.Stats.Errors = errorCount
		return fmt.Errorf("discovery completed with %d error(s)", errorCount)
	}

	return nil
}

// fetchHubPage fetches a venue/series hub page (design.md §4/§6.1 — the two
// example URLs are hub pages, not single events) and discovers new session
// links on it using the same schema.org/Event card parsing as search pages.
func (s *Scraper) fetchHubPage(ctx context.Context, hubSlug string, hubID int64) error {
	hubURL := fmt.Sprintf("%s/evento/%s", baseURL, hubSlug)
	body, err := s.fetcher.Fetch(ctx, hubURL, baseURL)
	if err != nil {
		return fmt.Errorf("fetch hub page %d: %w", hubID, err)
	}

	sessions, err := parse.ParseSearchPage(body)
	if err != nil {
		return fmt.Errorf("parse hub page %d: %w", hubID, err)
	}

	var errorCount int
	for _, sess := range sessions {
		s.Stats.EventsFound++

		if s.knownIDs[sess.EventID] {
			continue
		}

		time.Sleep(time.Duration(s.cfg.RequestDelayMS) * time.Millisecond)

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
