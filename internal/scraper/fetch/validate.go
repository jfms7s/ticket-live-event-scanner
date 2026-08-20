package fetch

import (
	"fmt"
	"net/url"
	"strings"
)

// allowedPathPrefixes are the only ticketline.pt paths the scraper is
// permitted to fetch.
var allowedPathPrefixes = []string{"/agenda", "/pesquisa", "/evento"}

// validateURL checks that urlStr resolves to the same host as baseURL and
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
