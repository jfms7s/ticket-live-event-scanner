// Package parse extracts event data from ticketline.pt search/hub pages,
// event detail pages, and event URLs.
package parse

import (
	"strings"

	"golang.org/x/net/html"
)

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
