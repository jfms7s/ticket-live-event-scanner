package parse

import (
	"strconv"
	"strings"
)

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

// ExtractIDFromSlug returns the trailing numeric event ID from a bare
// slug such as "auchan-live-academia-maia-98164" (no leading path).
func ExtractIDFromSlug(slug string) int64 {
	lastHyphen := strings.LastIndex(slug, "-")
	if lastHyphen <= 0 {
		return 0
	}
	id, err := strconv.ParseInt(slug[lastHyphen+1:], 10, 64)
	if err != nil {
		return 0
	}
	return id
}
