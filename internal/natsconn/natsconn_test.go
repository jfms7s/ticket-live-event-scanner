package natsconn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nats-io/nats.go"
)

// env returns a getenv that reads from m.
func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// apply applies opts to a fresh copy of the client defaults.
func apply(t *testing.T, opts []nats.Option) nats.Options {
	t.Helper()
	o := nats.GetDefaultOptions()
	for _, opt := range opts {
		if err := opt(&o); err != nil {
			t.Fatalf("apply option: %v", err)
		}
	}
	return o
}

func TestOptionsFromEnvNone(t *testing.T) {
	opts, err := optionsFromEnv("scraper", env(nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	o := apply(t, opts)
	if o.Name != "scraper" {
		t.Errorf("Name = %q, want scraper", o.Name)
	}
	if o.User != "" || o.Password != "" || o.Token != "" || o.UserJWT != nil {
		t.Errorf("anonymous: got User=%q Password set=%v Token set=%v UserJWT set=%v",
			o.User, o.Password != "", o.Token != "", o.UserJWT != nil)
	}
}

func TestOptionsFromEnvUserPassword(t *testing.T) {
	opts, err := optionsFromEnv("web-ui-api", env(map[string]string{
		"NATS_USER": "api", "NATS_PASSWORD": "s3cret",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	o := apply(t, opts)
	if o.User != "api" || o.Password != "s3cret" {
		t.Errorf("User/Password = %q/%q, want api/s3cret", o.User, o.Password)
	}
	if o.Token != "" {
		t.Errorf("Token set, want empty")
	}
	if o.Name != "web-ui-api" {
		t.Errorf("Name = %q, want web-ui-api", o.Name)
	}
}

func TestOptionsFromEnvToken(t *testing.T) {
	opts, err := optionsFromEnv("telegram-notifier", env(map[string]string{"NATS_TOKEN": "tok"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	o := apply(t, opts)
	if o.Token != "tok" {
		t.Errorf("Token = %q, want tok", o.Token)
	}
	if o.User != "" || o.Password != "" {
		t.Errorf("User/Password set, want empty")
	}
}

func TestOptionsFromEnvCreds(t *testing.T) {
	// nats.UserCredentials checks the file exists when applied.
	path := filepath.Join(t.TempDir(), "user.creds")
	if err := os.WriteFile(path, []byte("not read until connect"), 0o600); err != nil {
		t.Fatal(err)
	}
	opts, err := optionsFromEnv("email-notifier", env(map[string]string{"NATS_CREDS": path}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	o := apply(t, opts)
	if o.UserJWT == nil || o.SignatureCB == nil {
		t.Errorf("creds: UserJWT/SignatureCB not set")
	}
}

func TestOptionsFromEnvRejectsBadCombinations(t *testing.T) {
	cases := map[string]map[string]string{
		"user only":         {"NATS_USER": "api"},
		"password only":     {"NATS_PASSWORD": "s3cret"},
		"token and user":    {"NATS_TOKEN": "tok", "NATS_USER": "api", "NATS_PASSWORD": "s3cret"},
		"token and creds":   {"NATS_TOKEN": "tok", "NATS_CREDS": "/c"},
		"creds and user":    {"NATS_CREDS": "/c", "NATS_USER": "api", "NATS_PASSWORD": "s3cret"},
		"token and pw only": {"NATS_TOKEN": "tok", "NATS_PASSWORD": "s3cret"},
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := optionsFromEnv("scraper", env(m))
			if err == nil {
				t.Fatal("want error, got nil")
			}
			for _, secret := range []string{"s3cret", "tok"} {
				if strings.Contains(err.Error(), secret) {
					t.Errorf("error %q leaks a credential", err)
				}
			}
		})
	}
}
