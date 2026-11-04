package channels

import "testing"

var allVars = []string{
	"TELEGRAM_BOT_TOKEN", "TELEGRAM_CHAT_ID", "SMTP_HOST", "SMTP_USERNAME", "SMTP_PASSWORD", "EMAIL_FROM", "EMAIL_TO",
}

// A process with no channel configured starts: both are disabled, not fatal.
func TestFromEnvDisablesUnconfiguredChannels(t *testing.T) {
	for _, k := range allVars {
		t.Setenv(k, "")
	}
	enabled, status := FromEnv()
	if len(enabled) != 0 || status["telegram"] || status["email"] {
		t.Errorf("enabled %d, status %v; want both channels disabled", len(enabled), status)
	}
	if _, ok := status["telegram"]; !ok {
		t.Error("a disabled channel must still appear in the status, as 0")
	}
}

func TestFromEnvEnablesWhatIsConfigured(t *testing.T) {
	for _, k := range allVars {
		t.Setenv(k, "")
	}
	t.Setenv("TELEGRAM_BOT_TOKEN", "tok")
	t.Setenv("TELEGRAM_CHAT_ID", "-100")
	enabled, status := FromEnv()
	if len(enabled) != 1 || enabled[0].Name() != "telegram" || !status["telegram"] || status["email"] {
		t.Errorf("enabled %d, status %v; want only telegram", len(enabled), status)
	}
}

// A half-configured channel is disabled too, never started with holes.
func TestFromEnvDisablesAHalfConfiguredChannel(t *testing.T) {
	for _, k := range allVars {
		t.Setenv(k, "")
	}
	t.Setenv("SMTP_HOST", "smtp.example")
	if _, status := FromEnv(); status["email"] {
		t.Error("email enabled with only SMTP_HOST set")
	}
}
