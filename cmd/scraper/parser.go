package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"golang.org/x/net/html"
)

// parseSearchPage extracts event cards from a search/agenda page.
func parseSearchPage(body string) ([]event.Discovered, error) {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse HTML: %w", err)
	}

	var events []event.Discovered
	findEventCards(doc, &events)
	return events, nil
}

// findEventCards recursively finds all Event microdata items in the document.
func findEventCards(n *html.Node, events *[]event.Discovered) {
	if n.Type == html.ElementNode && n.Data == "li" {
		// Check if this is an event card
		if isEventCard(n) {
			evt := extractEventFromCard(n)
			if evt != nil && evt.EventID != 0 {
				*events = append(*events, *evt)
			}
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		findEventCards(c, events)
	}
}

// isEventCard checks if a <li> element is a schema.org/Event microdata item.
func isEventCard(n *html.Node) bool {
	if n.Type != html.ElementNode || n.Data != "li" {
		return false
	}

	for _, attr := range n.Attr {
		if attr.Key == "itemtype" && strings.Contains(attr.Val, "schema.org/Event") {
			return true
		}
	}
	return false
}

// extractEventFromCard parses all microdata fields from an event card.
func extractEventFromCard(n *html.Node) *event.Discovered {
	evt := &event.Discovered{}

	// Extract all itemprop fields
	itemprops := extractItemprops(n)

	// Extract URL and event ID from the href
	if urlStr, ok := itemprops["url"]; ok {
		evt.URL = urlStr
		evt.Slug, evt.EventID = parseEventURL(urlStr)
	}

	evt.Title = itemprops["name"]
	evt.Venue = itemprops["location"]
	evt.ImageURL = itemprops["image"]
	evt.EventDate = itemprops["startDate"]

	// Extract category from .metadata.categories
	evt.Category = extractCategory(n)

	return evt
}

// extractItemprops finds all itemprop elements and their values.
func extractItemprops(n *html.Node) map[string]string {
	props := make(map[string]string)
	extractItempropsRecursive(n, props, true)
	return props
}

// extractItempropsRecursive walks n's subtree collecting itemprop values
// into props. isRoot marks n as the item we're extracting properties FOR
// (e.g. the Event card/detail node) rather than a nested item found while
// recursing (e.g. a Place nested under "location") — see the itemscope
// guard below for why that distinction matters.
func extractItempropsRecursive(n *html.Node, props map[string]string, isRoot bool) {
	if n.Type == html.ElementNode {
		// Get itemprop attribute
		itemprop := getAttr(n, "itemprop")
		if itemprop != "" {
			switch itemprop {
			case "url":
				if href := getAttr(n, "href"); href != "" {
					props["url"] = href
				}
			case "image":
				// Cards lazy-load their poster: src is a "/static/img/blank.png"
				// placeholder and the real URL sits in data-src-original, so
				// that must be checked first or every image ends up broken.
				if lazySrc := getAttr(n, "data-src-original"); lazySrc != "" {
					props["image"] = lazySrc
				} else if src := getAttr(n, "src"); src != "" {
					props["image"] = src
				} else if content := getAttr(n, "content"); content != "" {
					props["image"] = content
				}
			case "startDate":
				if dateStr := getAttr(n, "content"); dateStr != "" {
					props["startDate"] = dateStr
				} else if dateStr := getAttr(n, "data-date"); dateStr != "" {
					props["startDate"] = dateStr
				}
			case "name":
				if content := getAttr(n, "content"); content != "" {
					props["name"] = content
				} else if text := strings.TrimSpace(getTextContent(n)); text != "" {
					props["name"] = text
				}
			case "location":
				if content := getAttr(n, "content"); content != "" {
					props["location"] = content
				} else if hasAttr(n, "itemscope") {
					// Nested Place item (event detail pages, unlike search
					// cards, wrap the venue in its own schema.org/Place with
					// its own "name"/"address" itemprops). Use just the
					// Place's own name rather than the full node text, which
					// would otherwise glue the venue name and address
					// together with no separator.
					if venue := findNestedItemprop(n, "name"); venue != "" {
						props["location"] = venue
					}
				} else if text := strings.TrimSpace(getTextContent(n)); text != "" {
					props["location"] = text
				}
			}

			// A non-root node that is itself a nested item (declares
			// itemscope) marks the start of another schema.org item —
			// e.g. the Place nested under the Event's "location" above.
			// Its own itemprop value has just been captured; don't
			// descend into it, or itemprops belonging to that nested item
			// (like the Place's own "name") would land in this same flat
			// map and silently overwrite the outer Event's same-named
			// property. The root item node itself may also carry
			// itemscope (and, on some pages, a stray itemprop) but must
			// still be descended into — that's where all its properties
			// actually live.
			if !isRoot && hasAttr(n, "itemscope") {
				return
			}
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		extractItempropsRecursive(c, props, false)
	}
}

// hasAttr reports whether the node carries the given attribute, regardless
// of its value — needed for boolean attributes like "itemscope" where
// getAttr's empty-string return can't distinguish "absent" from "present
// with no value".
func hasAttr(n *html.Node, key string) bool {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return true
		}
	}
	return false
}

// findNestedItemprop searches n's subtree for the first element carrying
// itemprop=key and returns its value (content attribute, else text). Unlike
// extractItempropsRecursive, it writes nothing to a shared props map, so it
// can safely be used to pull a single property out of a nested item without
// risking a key collision with the outer item being extracted.
func findNestedItemprop(n *html.Node, key string) string {
	if n.Type == html.ElementNode && getAttr(n, "itemprop") == key {
		if content := getAttr(n, "content"); content != "" {
			return content
		}
		return strings.TrimSpace(getTextContent(n))
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if val := findNestedItemprop(c, key); val != "" {
			return val
		}
	}
	return ""
}

// extractCategory finds the .metadata.categories text.
func extractCategory(n *html.Node) string {
	return findElementWithClass(n, "metadata categories")
}

// findElementWithClass recursively finds an element with the given class.
func findElementWithClass(n *html.Node, className string) string {
	if n.Type == html.ElementNode {
		if hasClass(n, className) {
			return strings.TrimSpace(getTextContent(n))
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if result := findElementWithClass(c, className); result != "" {
			return result
		}
	}

	return ""
}

// hasClass checks if an element has all the specified classes (space-separated).
func hasClass(n *html.Node, className string) bool {
	classAttr := getAttr(n, "class")
	if classAttr == "" {
		return false
	}

	// Split both the element's classes and the search string
	elementClasses := make(map[string]bool)
	for _, c := range strings.Fields(classAttr) {
		elementClasses[c] = true
	}

	// Check that all required classes are present
	for _, c := range strings.Fields(className) {
		if !elementClasses[c] {
			return false
		}
	}
	return true
}

// getAttr returns the value of an attribute.
func getAttr(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

// getTextContent extracts all text content from a node.
func getTextContent(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}

	var result strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		result.WriteString(getTextContent(c))
	}
	return result.String()
}

// parseEventURL extracts slug and event ID from a URL like /evento/auchan-live-academia-maia-98164
// Returns the full slug (including ID) and the numeric event ID
func parseEventURL(urlStr string) (slug string, id int64) {
	// URL format: /evento/{slug}-{id}
	// Extract the slug-id part
	parts := strings.Split(urlStr, "/")
	if len(parts) < 2 {
		return "", 0
	}

	slugID := parts[len(parts)-1]

	// Split on last hyphen to extract ID
	lastHyphen := strings.LastIndex(slugID, "-")
	if lastHyphen <= 0 {
		return "", 0
	}

	idStr := slugID[lastHyphen+1:]

	if num, err := strconv.ParseInt(idStr, 10, 64); err == nil {
		// Return full slug (including ID) for the published message
		return slugID, num
	}

	return "", 0
}

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

// parseEventDetail extracts detailed event information from an event detail page.
func parseEventDetail(body string, eventID int64) (*event.Discovered, error) {
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

// findElementNodeWithClass recursively finds the first element with the
// given class (see hasClass) and returns the node itself, unlike
// findElementWithClass which returns its text content.
func findElementNodeWithClass(n *html.Node, className string) *html.Node {
	if n.Type == html.ElementNode && hasClass(n, className) {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if result := findElementNodeWithClass(c, className); result != nil {
			return result
		}
	}
	return nil
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
