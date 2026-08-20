package config

import (
	"net/url"
	"os"
	"strings"
)

// getEnvHubPages reads a comma-separated list of hub pages from the given
// env var, accepting either full URLs or bare slugs (see hubPageSlug).
// Falls back to defaultVal if the env var is unset or contains no usable
// entries.
func getEnvHubPages(key string, defaultVal []string) []string {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}

	var slugs []string
	for _, raw := range strings.Split(val, ",") {
		if slug := hubPageSlug(strings.TrimSpace(raw)); slug != "" {
			slugs = append(slugs, slug)
		}
	}
	if len(slugs) == 0 {
		return defaultVal
	}
	return slugs
}

// hubPageSlug extracts the bare "/evento/{slug}" slug from either a full
// hub page URL (e.g. "https://www.ticketline.pt/evento/auchan-live-academia-maia-98164")
// or an already-bare slug. Any domain in the input is ignored — hub pages
// are always fetched against "https://www.ticketline.pt", so a caller can't point
// the scraper at an arbitrary host via this setting.
func hubPageSlug(raw string) string {
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return ""
		}
		raw = u.Path
	}
	raw = strings.Trim(raw, "/")
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "/")
	return parts[len(parts)-1]
}
