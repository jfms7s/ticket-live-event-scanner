package config

import (
	"reflect"
	"testing"
	"time"
)

func TestLoadDisablesTheNotifierWhenVariablesAreUnset(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	t.Setenv("TELEGRAM_CHAT_ID", "-100")
	_, missing, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"TELEGRAM_BOT_TOKEN"}; !reflect.DeepEqual(missing, want) {
		t.Errorf("missing = %v, want %v", missing, want)
	}
}

func TestLoadReadsACompleteConfig(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "tok")
	t.Setenv("TELEGRAM_CHAT_ID", "-100")
	t.Setenv("TELEGRAM_MIN_SEND_INTERVAL", "5s")
	cfg, missing, err := Load()
	if err != nil || missing != nil || cfg.BotToken != "tok" || cfg.ChatID != "-100" || cfg.MinSendInterval != 5*time.Second {
		t.Errorf("Load = %+v, %v", cfg, missing)
	}
}

func TestLoadReportsAnInvalidIntervalInsteadOfStopping(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "tok")
	t.Setenv("TELEGRAM_CHAT_ID", "-100")
	t.Setenv("TELEGRAM_MIN_SEND_INTERVAL", "3sec")
	if _, _, err := Load(); err == nil {
		t.Error("Load accepted TELEGRAM_MIN_SEND_INTERVAL=3sec")
	}
}

func TestParseMinSendInterval(t *testing.T) {
	if d, err := ParseMinSendInterval(""); err != nil || d != DefaultMinSendInterval {
		t.Errorf(`ParseMinSendInterval("") = %v, %v; want %v`, d, err, DefaultMinSendInterval)
	}
	if d, err := ParseMinSendInterval("500ms"); err != nil || d != 500*time.Millisecond {
		t.Errorf(`ParseMinSendInterval("500ms") = %v, %v`, d, err)
	}
	if d, err := ParseMinSendInterval("0"); err != nil || d != 0 {
		t.Errorf(`ParseMinSendInterval("0") = %v, %v; want 0 (off)`, d, err)
	}
	for _, bad := range []string{"3", "fast", "-1s"} {
		if _, err := ParseMinSendInterval(bad); err == nil {
			t.Errorf("ParseMinSendInterval(%q) accepted, want an error", bad)
		}
	}
}
