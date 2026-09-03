package config

import (
	"os"
	"reflect"
	"testing"
)

// TestGetEnvList tests parsing a comma-separated environment variable into
// a trimmed, non-empty list of addresses.
func TestGetEnvList(t *testing.T) {
	const key = "EMAIL_TO_TEST"

	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{"single address", "a@example.com", []string{"a@example.com"}},
		{"multiple addresses", "a@example.com,b@example.com", []string{"a@example.com", "b@example.com"}},
		{"whitespace trimmed", " a@example.com , b@example.com ", []string{"a@example.com", "b@example.com"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			os.Setenv(key, tc.value)
			defer os.Unsetenv(key)

			got := getEnvList(key)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("getEnvList(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

func TestGetEnvInt(t *testing.T) {
	const key = "SMTP_PORT_TEST"

	t.Run("unset falls back to default", func(t *testing.T) {
		os.Unsetenv(key)
		if got := getEnvInt(key, 587); got != 587 {
			t.Errorf("getEnvInt() = %d, want 587", got)
		}
	})

	t.Run("parses valid int", func(t *testing.T) {
		os.Setenv(key, "465")
		defer os.Unsetenv(key)
		if got := getEnvInt(key, 587); got != 465 {
			t.Errorf("getEnvInt() = %d, want 465", got)
		}
	})

	t.Run("invalid value falls back to default", func(t *testing.T) {
		os.Setenv(key, "not-a-number")
		defer os.Unsetenv(key)
		if got := getEnvInt(key, 587); got != 587 {
			t.Errorf("getEnvInt() = %d, want 587", got)
		}
	})
}
