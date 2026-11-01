package discovery

import (
	"context"
	"errors"
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
			// A misconfigured entry: it can never be scraped, so say so
			// on every run instead of skipping it silently.
			log.Printf("ERROR hub page %q has no event ID in its slug", hubSlug)
			errorCount++
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

// fetchHubPage fetches a venue/series hub page (the project design doc,
// kept outside this repo, §4/§6.1: the two example URLs are hub pages, not
// single events) and discovers new session links on it with
// parse.ParseHubSessions, which ignores the similar-events widget.
func (s *Scraper) fetchHubPage(ctx context.Context, hubSlug string, hubID int64) error {
	hubURL := fmt.Sprintf("%s/evento/%s", s.baseURL, hubSlug)
	body, err := s.fetcher.Fetch(ctx, hubURL, s.baseURL)
	if err != nil {
		return fmt.Errorf("fetch hub page %d: %w", hubID, err)
	}

	sessions, err := parse.ParseHubSessions(body)
	if err != nil {
		return fmt.Errorf("parse hub page %d: %w", hubID, err)
	}
	// A tracked hub page always lists its sessions; zero cards most likely
	// means ticketline.pt changed its markup, which must not look like "no
	// new sessions".
	if len(sessions) == 0 {
		return fmt.Errorf("hub page %d: no session cards found (markup change?)", hubID)
	}

	var errorCount int
	for _, sess := range sessions {
		s.Stats.EventsFound++

		if s.knownIDs[sess.EventID] {
			continue
		}
		if s.skipIDs[sess.EventID] {
			// Rejected by this run or a recent one; retried once its
			// skipped-bucket key expires.
			s.Stats.Skipped++
			continue
		}

		time.Sleep(time.Duration(s.cfg.RequestDelayMS) * time.Millisecond)

		if err := s.fetchAndPublishEvent(ctx, sess); errors.Is(err, errSkip) {
			s.skip(ctx, sess.EventID, err)
		} else if err != nil {
			log.Printf("Error fetching detail for hub session event %d: %v", sess.EventID, err)
			errorCount++
		}
	}

	if errorCount > 0 {
		return fmt.Errorf("hub page %d: %d error(s) fetching events", hubID, errorCount)
	}

	return nil
}
