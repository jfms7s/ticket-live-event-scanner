package config

import (
	"reflect"
	"testing"
)

func setAll(t *testing.T) {
	t.Helper()
	for k, v := range map[string]string{
		"SMTP_HOST": "smtp.example", "SMTP_USERNAME": "u", "SMTP_PASSWORD": "p",
		"EMAIL_FROM": "from@example", "EMAIL_TO": "a@example, b@example",
	} {
		t.Setenv(k, v)
	}
}

func TestLoadDisablesTheNotifierWhenVariablesAreUnset(t *testing.T) {
	setAll(t)
	t.Setenv("SMTP_PASSWORD", "")
	t.Setenv("EMAIL_TO", "")
	_, missing := Load()
	if want := []string{"SMTP_PASSWORD", "EMAIL_TO"}; !reflect.DeepEqual(missing, want) {
		t.Errorf("missing = %v, want %v", missing, want)
	}
}

func TestLoadReadsACompleteConfig(t *testing.T) {
	setAll(t)
	cfg, missing := Load()
	if missing != nil || cfg.SMTPHost != "smtp.example" || cfg.SMTPPort != 587 ||
		!reflect.DeepEqual(cfg.EmailTo, []string{"a@example", "b@example"}) {
		t.Errorf("Load = %+v, %v", cfg, missing)
	}
}
