package config

import (
	"os"
	"reflect"
	"testing"
)

// TestHubPagesFromEnv tests parsing a comma-separated TICKETLINE_HUB_PAGES
// value, and falling back to the default when unset.
func TestHubPagesFromEnv(t *testing.T) {
	const key = "TICKETLINE_HUB_PAGES_TEST"
	defaultVal := []string{"default-slug-1"}

	t.Run("unset falls back to default", func(t *testing.T) {
		os.Unsetenv(key)
		got := hubPagesFromEnv(key, defaultVal)
		if !reflect.DeepEqual(got, defaultVal) {
			t.Errorf("hubPagesFromEnv() = %v, want %v", got, defaultVal)
		}
	})

	t.Run("mixed full URLs and bare slugs", func(t *testing.T) {
		os.Setenv(key, "https://www.ticketline.pt/evento/auchan-live-academia-maia-98164, auchan-live-academia-aveiro-98167")
		defer os.Unsetenv(key)

		got := hubPagesFromEnv(key, defaultVal)
		want := []string{"auchan-live-academia-maia-98164", "auchan-live-academia-aveiro-98167"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("hubPagesFromEnv() = %v, want %v", got, want)
		}
	})
}
