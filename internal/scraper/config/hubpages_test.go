package config

import (
	"os"
	"reflect"
	"testing"
)

// TestHubPageSlug tests that hubPageSlug accepts both full URLs and bare
// slugs, ignoring any domain in the input.
func TestHubPageSlug(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"auchan-live-academia-maia-98164", "auchan-live-academia-maia-98164"},
		{"https://www.ticketline.pt/evento/auchan-live-academia-maia-98164", "auchan-live-academia-maia-98164"},
		{"https://www.ticketline.pt/evento/auchan-live-academia-aveiro-98167/", "auchan-live-academia-aveiro-98167"},
		{"/evento/auchan-live-academia-maia-98164", "auchan-live-academia-maia-98164"},
		{"", ""},
	}

	for _, tc := range tests {
		if got := hubPageSlug(tc.input); got != tc.expected {
			t.Errorf("hubPageSlug(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

// TestGetEnvHubPages tests parsing a comma-separated TICKETLINE_HUB_PAGES
// value, and falling back to the default when unset.
func TestGetEnvHubPages(t *testing.T) {
	const key = "TICKETLINE_HUB_PAGES_TEST"
	defaultVal := []string{"default-slug-1"}

	t.Run("unset falls back to default", func(t *testing.T) {
		os.Unsetenv(key)
		got := getEnvHubPages(key, defaultVal)
		if !reflect.DeepEqual(got, defaultVal) {
			t.Errorf("getEnvHubPages() = %v, want %v", got, defaultVal)
		}
	})

	t.Run("mixed full URLs and bare slugs", func(t *testing.T) {
		os.Setenv(key, "https://www.ticketline.pt/evento/auchan-live-academia-maia-98164, auchan-live-academia-aveiro-98167")
		defer os.Unsetenv(key)

		got := getEnvHubPages(key, defaultVal)
		want := []string{"auchan-live-academia-maia-98164", "auchan-live-academia-aveiro-98167"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("getEnvHubPages() = %v, want %v", got, want)
		}
	})
}
