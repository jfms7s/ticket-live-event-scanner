// Package channels builds the delivery channels web-ui-api sends through:
// Telegram (each new session, the weekly digest) and email (calendar
// invites).
package channels

import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/deliver"
	"github.com/jfms7s/ticket-live-event-scanner/internal/emailnotifier/config"
	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/email"
	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/telegram"
	"github.com/jfms7s/ticket-live-event-scanner/internal/scraper/digest"
	telegramconfig "github.com/jfms7s/ticket-live-event-scanner/internal/telegramnotifier/config"
)

// Channels is what the process sends with.
type Channels struct {
	// Senders go to the delivery poller; a nil one is a disabled channel.
	Senders deliver.Senders
	// TelegramGap is the minimum time between two Telegram sends.
	TelegramGap time.Duration
	// Digest posts the weekly digest to Telegram; nil when it is disabled.
	Digest digest.Sender
	// Enabled says, for every channel, whether it is enabled (exported on
	// /metrics).
	Enabled map[string]bool
}

// FromEnv builds the channels whose configuration is complete. A channel with
// unset or malformed variables is disabled and logged, so a missing Secret
// key never takes the dashboard down with it; the status is exported on
// /metrics so it can be alerted on.
func FromEnv() Channels {
	c := Channels{Enabled: map[string]bool{"telegram": false, "email": false}}

	tcfg, missing, err := telegramconfig.Load()
	switch {
	case err != nil:
		log.Printf("ERROR Telegram notifications are DISABLED: %v", err)
	case len(missing) > 0:
		log.Printf("WARN Telegram notifications are DISABLED: %s not set", strings.Join(missing, ", "))
	default:
		c.Enabled["telegram"] = true
		c.TelegramGap = tcfg.MinSendInterval
		c.Senders.Telegram = func(ctx context.Context, d event.Discovered) (string, error) {
			return telegram.SendTelegramMessage(ctx, tcfg.BotToken, tcfg.ChatID, d)
		}
		c.Digest = func(ctx context.Context, text string) (string, error) {
			return telegram.SendText(ctx, tcfg.BotToken, tcfg.ChatID, text)
		}
		log.Printf("Minimum interval between Telegram sends: %v", tcfg.MinSendInterval)
	}

	ecfg, missing, err := config.Load()
	switch {
	case err != nil:
		log.Printf("ERROR Calendar-invite emails are DISABLED: %v", err)
	case len(missing) > 0:
		log.Printf("WARN Calendar-invite emails are DISABLED: %s not set", strings.Join(missing, ", "))
	default:
		c.Enabled["email"] = true
		smtp := email.SMTPConfig{
			Host:           ecfg.SMTPHost,
			Port:           ecfg.SMTPPort,
			Username:       ecfg.SMTPUsername,
			Password:       ecfg.SMTPPassword,
			From:           ecfg.EmailFrom,
			AllowPlaintext: ecfg.SMTPAllowPlaintext,
		}
		c.Senders.Email = func(ctx context.Context, p event.Purchased) (string, error) {
			return email.SendCalendarEmail(ctx, smtp, ecfg.EmailTo, p)
		}
	}
	return c
}
