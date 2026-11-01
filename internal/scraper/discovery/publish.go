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
	detailURL := fmt.Sprintf("%s/evento/%s", s.baseURL, basicEvent.Slug)
	body, err := s.fetcher.Fetch(ctx, detailURL, s.baseURL)
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

	ack, err := s.pub.Publish(ctx, streams.EventsSubject, data,
		jetstream.WithMsgID(strconv.FormatInt(basicEvent.EventID, 10)))
	if err != nil {
		return fmt.Errorf("publish event: %w", err)
	}

	// Mark as known immediately so this run doesn't re-fetch/re-publish it
	// if the same event ID turns up again later in this same discovery pass
	// (the same session can be listed on two tracked hub pages in one run).
	s.knownIDs[basicEvent.EventID] = true

	if ack != nil && ack.Duplicate {
		// The stream still remembers this message ID (7-day window), so
		// nothing was delivered: an earlier run published it but didn't
		// get to mark it seen.
		log.Printf("Event %d already on the stream; marking it seen", basicEvent.EventID)
	} else {
		s.Stats.EventsPublished++
		log.Printf("Published event %d: %s", basicEvent.EventID, detailEvent.Title)
	}

	// Without the mark, the next runs rely on the stream's message-ID
	// window and on web-ui-api materialising the event, so fail the run
	// loudly rather than carry on as if all were well.
	if err := s.seen.Mark(ctx, basicEvent.EventID); err != nil {
		return fmt.Errorf("event %d published but not marked seen: %w", basicEvent.EventID, err)
	}
	return nil
}
