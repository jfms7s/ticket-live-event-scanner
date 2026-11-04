package config

import (
	"strings"

	"github.com/jfms7s/ticket-live-event-scanner/internal/envconfig"
	"github.com/jfms7s/ticket-live-event-scanner/internal/watch"
)

// hubPagesFromEnv reads a comma-separated list of hub pages from the given
// env var, accepting either full URLs or bare slugs (see watch.Slug).
// Falls back to defaultVal if the env var is unset or contains no usable
// entries.
func hubPagesFromEnv(key string, defaultVal []string) []string {
	val := envconfig.String(key, "")
	if val == "" {
		return defaultVal
	}

	var slugs []string
	for _, raw := range strings.Split(val, ",") {
		if slug := watch.Slug(raw); slug != "" {
			slugs = append(slugs, slug)
		}
	}
	if len(slugs) == 0 {
		return defaultVal
	}
	return slugs
}
