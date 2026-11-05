package discovery

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/scraper/parse"
)

// fetchAndPublishEvent fetches an event's detail page, parses it, and
// queues the resulting notification (storing the event with it).
func (s *Scraper) fetchAndPublishEvent(ctx context.Context, basicEvent event.Discovered, hubSlug string) error {
	// Construct full absolute URL for detail page
	detailURL := fmt.Sprintf("%s/evento/%s", s.baseURL, basicEvent.Slug)
	body, err := s.fetcher.Fetch(ctx, detailURL, s.baseURL)
	if err != nil {
		return err
	}

	// Parse detail page
	detailEvent, err := parse.ParseEventDetail(body, basicEvent.EventID)
	if err != nil {
		if errors.Is(err, parse.ErrInvalidEventDate) {
			return fmt.Errorf("%w: %w", errSkip, err)
		}
		return err
	}

	// Ensure URL is absolute (override any relative URL from parsing)
	if detailEvent.URL == "" {
		detailEvent.URL = detailURL
	} else if !strings.HasPrefix(detailEvent.URL, "http") {
		// If URL is relative, make it absolute
		detailEvent.URL = s.baseURL + detailEvent.URL
	}

	// Detail pages don't self-link via itemprop="url", so parse.ParseEventDetail
	// can't derive a slug from the page body — fall back to the slug we
	// already know from the hub/search card that pointed us here.
	if detailEvent.Slug == "" {
		detailEvent.Slug = basicEvent.Slug
	}

	// Belt-and-braces: if some detail page variant lacks both the
	// microdata image and the header thumb anchor parse.ParseEventDetail looks
	// for, fall back to the poster already seen on the hub/search card.
	if detailEvent.ImageURL == "" {
		detailEvent.ImageURL = basicEvent.ImageURL
	}

	// Set reason
	detailEvent.Reason = event.ReasonDiscovered
	detailEvent.HubSlug = hubSlug

	// Validate required fields
	if detailEvent.Title == "" {
		return fmt.Errorf("%w: event %d missing required field: title", errSkip, basicEvent.EventID)
	}
	if detailEvent.URL == "" {
		return fmt.Errorf("%w: event %d missing required field: url", errSkip, basicEvent.EventID)
	}
	if detailEvent.Slug == "" {
		return fmt.Errorf("%w: event %d missing required field: slug", errSkip, basicEvent.EventID)
	}

	// Store the session and queue its notification in one transaction.
	// Both exist or neither does: a failure here fails the run and the next
	// run finds the session unknown and tries again.
	queued, err := s.queue.EnqueueDiscovered(ctx, detailEvent)
	if err != nil {
		return fmt.Errorf("queue event %d: %w", basicEvent.EventID, err)
	}

	// Known now, so this run doesn't fetch it again if the same session
	// turns up on another tracked hub page.
	s.knownIDs[basicEvent.EventID] = true

	if !queued {
		// It already had a notification (another run or process queued it
		// between this one's start and now); nothing was added.
		log.Printf("Event %d already queued; not queueing it again", basicEvent.EventID)
		return nil
	}
	s.Stats.EventsPublished++
	log.Printf("Queued event %d: %s", basicEvent.EventID, detailEvent.Title)
	return nil
}
