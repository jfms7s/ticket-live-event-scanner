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
	extractItempropsRecursive(n, props)
	return props
}

func extractItempropsRecursive(n *html.Node, props map[string]string) {
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
				if src := getAttr(n, "src"); src != "" {
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
			case "name", "location":
				if text := getTextContent(n); text != "" {
					props[itemprop] = text
				}
			}
		}
	}

	for c := n.FirstChild; c != nil; c = c.NextSibling {
		extractItempropsRecursive(c, props)
	}
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

// validateEventDate checks if the date string is in valid YYYY-MM-DD format.
// If the date is empty, it is considered valid (optional field).
// If a date is present, it must match the YYYY-MM-DD format.
func validateEventDate(dateStr string) error {
	if dateStr == "" {
		// Empty date is acceptable (optional field)
		return nil
	}
	_, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return fmt.Errorf("event date %q does not match YYYY-MM-DD format: %w", dateStr, err)
	}
	return nil
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
					extractItempropsRecursive(node, props)
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
	// Look for .metadata.categories or similar patterns
	category := findElementWithClass(n, "metadata categories")
	if category != "" {
		return strings.TrimSpace(category)
	}

	// Alternative: look for category in various places
	category = findElementWithClass(n, "category")
	if category != "" {
		return strings.TrimSpace(category)
	}

	return ""
}
