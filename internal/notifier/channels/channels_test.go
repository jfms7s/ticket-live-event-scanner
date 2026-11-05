package channels

import "testing"

var allVars = []string{
	"TELEGRAM_BOT_TOKEN", "TELEGRAM_CHAT_ID", "SMTP_HOST", "SMTP_USERNAME", "SMTP_PASSWORD", "EMAIL_FROM", "EMAIL_TO",
}

func clearAll(t *testing.T) {
	t.Helper()
	for _, k := range allVars {
		t.Setenv(k, "")
	}
}

// A process with no channel configured starts: both are disabled, not fatal.
func TestFromEnvDisablesUnconfiguredChannels(t *testing.T) {
	clearAll(t)
	c := FromEnv()
	if c.Senders.Telegram != nil || c.Senders.Email != nil || c.Digest != nil || c.Enabled["telegram"] || c.Enabled["email"] {
		t.Errorf("channels = %+v, want both disabled", c.Enabled)
	}
	if _, ok := c.Enabled["telegram"]; !ok {
		t.Error("a disabled channel must still appear in the status, as false")
	}
}

func TestFromEnvEnablesWhatIsConfigured(t *testing.T) {
	clearAll(t)
	t.Setenv("TELEGRAM_BOT_TOKEN", "tok")
	t.Setenv("TELEGRAM_CHAT_ID", "-100")
	c := FromEnv()
	if c.Senders.Telegram == nil || c.Digest == nil || !c.Enabled["telegram"] || c.Senders.Email != nil || c.Enabled["email"] {
		t.Errorf("channels = %+v, want only telegram", c.Enabled)
	}
}

// A half-configured channel is disabled too, never started with holes.
func TestFromEnvDisablesAHalfConfiguredChannel(t *testing.T) {
	clearAll(t)
	t.Setenv("SMTP_HOST", "smtp.example")
	if c := FromEnv(); c.Enabled["email"] || c.Senders.Email != nil {
		t.Error("email enabled with only SMTP_HOST set")
	}
}

// A typo in an optional value disables that channel; it must not stop the
// process (and so the dashboard).
func TestFromEnvDisablesAChannelWithAMalformedValue(t *testing.T) {
	clearAll(t)
	t.Setenv("TELEGRAM_BOT_TOKEN", "tok")
	t.Setenv("TELEGRAM_CHAT_ID", "-100")
	t.Setenv("TELEGRAM_MIN_SEND_INTERVAL", "3sec")
	t.Setenv("SMTP_HOST", "h")
	t.Setenv("SMTP_USERNAME", "u")
	t.Setenv("SMTP_PASSWORD", "p")
	t.Setenv("EMAIL_FROM", "f@x")
	t.Setenv("EMAIL_TO", "t@x")
	t.Setenv("SMTP_PORT", "submission")
	c := FromEnv()
	if c.Enabled["telegram"] || c.Enabled["email"] {
		t.Errorf("enabled %v, want both disabled", c.Enabled)
	}
}
