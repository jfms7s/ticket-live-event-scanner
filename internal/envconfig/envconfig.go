// Package envconfig reads service configuration from environment
// variables. Every service's config package uses it, so they share one set
// of rules: an unset or empty variable takes its default, a required one
// that is missing stops the process, and an integer that doesn't parse or
// is negative stops the process instead of silently becoming the default.
package envconfig

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
)

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

// Unset returns the keys among keys that are unset or empty, in order.
func Unset(keys ...string) []string {
	var missing []string
	for _, k := range keys {
		if os.Getenv(k) == "" {
			missing = append(missing, k)
		}
	}
	return missing
}

// IntErr is Int that returns the problem instead of stopping the process.
func IntErr(key string, def int) (int, error) {
	val := os.Getenv(key)
	if val == "" {
		return def, nil
	}
	i, err := strconv.Atoi(strings.TrimSpace(val))
	if err != nil {
		return def, fmt.Errorf("environment variable %s=%q is not an integer", key, val)
	}
	if i < 0 {
		return def, fmt.Errorf("environment variable %s=%q must not be negative", key, val)
	}
	return i, nil
}

// Int returns key parsed as a non-negative integer, or def when it is
// unset or empty. A value that doesn't parse, or is negative, stops the
// process.
func Int(key string, def int) int {
	i, err := IntErr(key, def)
	if err != nil {
		fatalf("%v", err)
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

// ListErr splits a comma-separated variable into trimmed, non-empty values
// (e.g. EMAIL_TO=a@x.com,b@x.com). It returns an error when key is unset or
// holds no value.
func ListErr(key string) ([]string, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return nil, fmt.Errorf("required environment variable not set: %s", key)
	}
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("environment variable %s must contain at least one value", key)
	}
	return result, nil
}

// List is ListErr for a required variable: it stops the process when key
// is unset or holds no value.
func List(key string) []string {
	result, err := ListErr(key)
	if err != nil {
		fatalf("%v", err)
		return nil
	}
	return result
}
