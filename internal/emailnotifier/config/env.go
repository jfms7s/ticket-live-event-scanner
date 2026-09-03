package config

import (
	"log"
	"os"
	"strconv"
	"strings"
)

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvRequired(key string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	log.Fatalf("required environment variable not set: %s", key)
	return ""
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

// getEnvList splits a required, comma-separated environment variable into
// a list of trimmed, non-empty values (e.g. EMAIL_TO=a@x.com,b@x.com).
func getEnvList(key string) []string {
	raw := getEnvRequired(key)
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		log.Fatalf("environment variable %s must contain at least one address", key)
	}
	return result
}
