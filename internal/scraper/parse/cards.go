package parse

import (
	"fmt"
	"strings"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"golang.org/x/net/html"
)

// ParseSearchPage extracts event cards from a search/agenda/hub page.
func ParseSearchPage(body string) ([]event.Discovered, error) {
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
