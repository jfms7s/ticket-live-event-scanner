package parse

import (
	"fmt"
	"strings"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"golang.org/x/net/html"
)

// eventDateLayouts are the schema.org startDate formats seen on ticketline.pt
// detail pages: a plain date for events without a fixed time, or a date+time
// (no seconds, no timezone offset) for events with one, e.g. sessions on the
// hub pages ("2026-09-04T18:30").
var eventDateLayouts = []string{"2006-01-02", "2006-01-02T15:04"}

// validateEventDate checks if the date string matches one of eventDateLayouts.
// If the date is empty, it is considered valid (optional field).
func validateEventDate(dateStr string) error {
	if dateStr == "" {
		// Empty date is acceptable (optional field)
		return nil
	}
	for _, layout := range eventDateLayouts {
		if _, err := time.Parse(layout, dateStr); err == nil {
			return nil
		}
	}
	return fmt.Errorf("event date %q does not match any known format (%s)", dateStr, strings.Join(eventDateLayouts, " or "))
}

// ParseEventDetail extracts detailed event information from an event detail page.
func ParseEventDetail(body string, eventID int64) (*event.Discovered, error) {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse HTML: %w", err)
	}

	evt := &event.Discovered{
		EventID: eventID,
	}

	// Extract all itemprops from the first Event microdata item
	itemprops := findFirstEventItemprops(doc)

	evt.Title = itemprops["name"]
	evt.Venue = itemprops["location"]
	evt.ImageURL = itemprops["image"]
	evt.EventDate = itemprops["startDate"]

	// The session card matched above (schema.org/Event, "Sessões" list)
	// doesn't carry an itemprop="image" at all — the only poster on a
	// detail page is the plain, non-lazy-loaded <a class="thumb"> in the
	// page's own header. Use it when microdata didn't give us one.
	if evt.ImageURL == "" {
		evt.ImageURL = findEventPosterURL(doc)
	}

	// Validate event date format
	if err := validateEventDate(evt.EventDate); err != nil {
		return nil, fmt.Errorf("event %d: invalid event date: %w", eventID, err)
	}

	// Extract URL and slug
	if urlStr, ok := itemprops["url"]; ok {
		evt.URL = urlStr
		evt.Slug, _ = parseEventURL(urlStr)
	}

	// Extract category
	evt.Category = findCategory(doc)

	return evt, nil
}

// findFirstEventItemprops finds itemprops from the first Event microdata item.
func findFirstEventItemprops(n *html.Node) map[string]string {
	props := make(map[string]string)

	var foundEvent bool
	var findItemprops func(*html.Node)
	findItemprops = func(node *html.Node) {
		if foundEvent {
			return
		}

		if node.Type == html.ElementNode {
			// Check if this is an Event microdata item
			for _, attr := range node.Attr {
				if attr.Key == "itemtype" && strings.Contains(attr.Val, "schema.org/Event") {
					foundEvent = true
					extractItempropsRecursive(node, props, true)
					return
				}
			}
		}

		for c := node.FirstChild; c != nil; c = c.NextSibling {
			findItemprops(c)
		}
	}

	findItemprops(n)
	return props
}

// findCategory finds the category text from the page.
func findCategory(n *html.Node) string {
	// Detail pages carry the event's own category tag(s) in its header as
	// <ul class="tags_list"><li><a href="/pesquisa?category=...">FORMAÇÃO</a></li></ul>.
	// This is the only category text actually scoped to this event — the
	// page's "similar events" widget further down reuses the
	// ".metadata.categories" class (checked below) for *other*, unrelated
	// events, so that fallback can silently return the wrong category if
	// tried first.
	if tags := findCategoryTags(n); tags != "" {
		return tags
	}

	// Fallback for markup without a tags_list (e.g. search/agenda cards,
	// or a page variant without one). Not element-scoped, so only reliable
	// when the caller has already narrowed n to a single card.
	category := findElementWithClass(n, "metadata categories")
	if category != "" {
		return strings.TrimSpace(category)
	}

	category = findElementWithClass(n, "category")
	if category != "" {
		return strings.TrimSpace(category)
	}

	return ""
}

// findCategoryTags returns the text of every <a> inside the first
// "tags_list" element found in n's subtree, joined with ", ".
func findCategoryTags(n *html.Node) string {
	list := findElementNodeWithClass(n, "tags_list")
	if list == nil {
		return ""
	}

	var tags []string
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "a" {
			if text := strings.TrimSpace(getTextContent(node)); text != "" {
				tags = append(tags, text)
			}
			return
		}
		for c := node.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(list)

	return strings.Join(tags, ", ")
}

// findEventPosterURL returns the event's poster image URL from a detail
// page's own <a class="thumb" href="..."><img .../></a> in its header —
// the only non-lazy-loaded, always-absolute image on the page. Matched
// specifically on the <a> tag: the "similar events" widget further down
// the same page reuses the "thumb" class on <div> wrappers around its own
// (unrelated) lazy-loaded card images, which must not be picked up here.
func findEventPosterURL(n *html.Node) string {
	if n.Type == html.ElementNode && n.Data == "a" && hasClass(n, "thumb") {
		if href := getAttr(n, "href"); href != "" {
			return href
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if url := findEventPosterURL(c); url != "" {
			return url
		}
	}
	return ""
}
