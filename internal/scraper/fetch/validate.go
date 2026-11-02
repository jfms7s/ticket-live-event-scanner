package fetch

import (
	"fmt"
	"net/url"
	"strings"
)

// allowedPathPrefixes are the only ticketline.pt paths the scraper is
// permitted to fetch.
var allowedPathPrefixes = []string{"/agenda", "/pesquisa", "/evento"}

// validateURL checks that urlStr has the same scheme and host as baseURL and
// that its path starts with one of allowedPathPrefixes.
func validateURL(urlStr, baseURL string) error {
	u, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	base, err := url.Parse(baseURL)
	if err != nil {
		return fmt.Errorf("invalid base URL: %w", err)
	}

	// Check scheme matches, so an https base never fetches over plain http
	if u.Scheme != base.Scheme {
		return fmt.Errorf("URL scheme mismatch: %s vs %s", u.Scheme, base.Scheme)
	}

	// Check domain matches
	if u.Host != base.Host {
		return fmt.Errorf("URL host mismatch: %s vs %s", u.Host, base.Host)
	}

	// Check path is in allowed list
	path := u.Path
	allowed := false
	for _, prefix := range allowedPathPrefixes {
		if strings.HasPrefix(path, prefix) {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("path not allowed: %s", path)
	}

	return nil
}
