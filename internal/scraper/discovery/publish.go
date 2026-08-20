package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/scraper/parse"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	"github.com/nats-io/nats.go/jetstream"
)

// fetchAndPublishEvent fetches an event's detail page, parses it, and
// publishes the resulting event.Discovered message to NATS JetStream.
func (s *Scraper) fetchAndPublishEvent(ctx context.Context, basicEvent event.Discovered) error {
	// Construct full absolute URL for detail page
	detailURL := fmt.Sprintf("%s/evento/%s", baseURL, basicEvent.Slug)
	body, err := s.fetcher.Fetch(ctx, detailURL, baseURL)
	if err != nil {
		return err
	}

	// Parse detail page
	detailEvent, err := parse.ParseEventDetail(body, basicEvent.EventID)
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
		detailEvent.URL = baseURL + detailEvent.URL
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

	s.Stats.EventsPublished++
	log.Printf("Published event %d: %s", basicEvent.EventID, detailEvent.Title)
	return nil
}
