// Package envconfig reads service configuration from environment
// variables. Every service's config package uses it, so they share one set
// of rules: an unset or empty variable takes its default, a required one
// that is missing stops the process, and an integer that doesn't parse or
// is negative stops the process instead of silently becoming the default.
package envconfig

import (
	"log"
	"os"
	"strconv"
	"strings"
)

// DefaultNatsURL is the NATS server every service uses when NATS_URL is unset.
const DefaultNatsURL = "nats://localhost:4222"

// fatalf stops the process; tests replace it.
var fatalf = log.Fatalf

// String returns the value of key, or def when it is unset or empty.
func String(key, def string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return def
}

// Required returns the value of key and stops the process when it is
// unset or empty.
func Required(key string) string {
	val := os.Getenv(key)
	if val == "" {
		fatalf("required environment variable not set: %s", key)
	}
	return val
}

// Int returns key parsed as a non-negative integer, or def when it is
// unset or empty. A value that doesn't parse, or is negative, stops the
// process.
func Int(key string, def int) int {
	val := os.Getenv(key)
	if val == "" {
		return def
	}
	i, err := strconv.Atoi(strings.TrimSpace(val))
	if err != nil {
		fatalf("environment variable %s=%q is not an integer", key, val)
		return def
	}
	if i < 0 {
		fatalf("environment variable %s=%q must not be negative", key, val)
		return def
	}
	return i
}

// Bool reports whether key is set to "true" or "1" (case-insensitive).
// Anything else, including unset, is false.
func Bool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "true", "1":
		return true
	}
	return false
}

// List splits a required, comma-separated variable into trimmed, non-empty
// values (e.g. EMAIL_TO=a@x.com,b@x.com). It stops the process when key is
// unset or holds no value.
func List(key string) []string {
	raw := Required(key)
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		fatalf("environment variable %s must contain at least one value", key)
	}
	return result
}

// NatsURL returns NATS_URL, or DefaultNatsURL when it is unset.
func NatsURL() string {
	return String("NATS_URL", DefaultNatsURL)
}
