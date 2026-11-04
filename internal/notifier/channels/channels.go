// Package channels builds the delivery channels web-ui-api runs: Telegram
// (new sessions and the weekly digest) and email (calendar invites).
package channels

import (
	"context"
	"log"
	"strings"

	"github.com/jfms7s/ticket-live-event-scanner/internal/emailnotifier/config"
	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier"
	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/email"
	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/emailhandler"
	"github.com/jfms7s/ticket-live-event-scanner/internal/notifier/handler"
	"github.com/jfms7s/ticket-live-event-scanner/internal/streams"
	telegramconfig "github.com/jfms7s/ticket-live-event-scanner/internal/telegramnotifier/config"
	"github.com/nats-io/nats.go/jetstream"
)

// FromEnv builds the channels whose configuration is complete and says, for
// every channel, whether it is enabled. A channel with unset variables is
// disabled and logged, so a missing Secret key never takes the dashboard
// down with it; the status is exported on /metrics so it can be alerted on.
func FromEnv() (enabled []notifier.Notifier, status map[string]bool) {
	status = map[string]bool{}

	tcfg, missing := telegramconfig.Load()
	status["telegram"] = len(missing) == 0
	if len(missing) == 0 {
		enabled = append(enabled, Telegram(tcfg))
		log.Printf("Minimum interval between Telegram sends: %v", tcfg.MinSendInterval)
	} else {
		log.Printf("WARN Telegram notifications are DISABLED: %s not set", strings.Join(missing, ", "))
	}

	ecfg, missing := config.Load()
	status["email"] = len(missing) == 0
	if len(missing) == 0 {
		enabled = append(enabled, Email(ecfg))
	} else {
		log.Printf("WARN Calendar-invite emails are DISABLED: %s not set", strings.Join(missing, ", "))
	}
	return enabled, status
}

// telegram sends each new session, and the weekly digest, to the chat.
type telegram struct {
	cfg telegramconfig.Config
	h   *handler.Handler
}

// Telegram returns the Telegram channel.
func Telegram(cfg telegramconfig.Config) notifier.Notifier { return &telegram{cfg: cfg} }

func (t *telegram) Name() string { return "telegram" }

func (t *telegram) Bindings(ctx context.Context, js jetstream.JetStream) ([]notifier.Binding, error) {
	events, err := streams.EnsureConsumer(ctx, js, streams.TelegramConsumer())
	if err != nil {
		return nil, err
	}
	digest, err := streams.EnsureConsumer(ctx, js, streams.TelegramDigestConsumer())
	if err != nil {
		return nil, err
	}
	t.h = handler.New(t.cfg.BotToken, t.cfg.ChatID, js)
	t.h.SetMinSendInterval(t.cfg.MinSendInterval)
	d := handler.NewDigest(t.cfg.BotToken, t.cfg.ChatID)
	return []notifier.Binding{
		{Name: "events", Consumer: events, Handle: t.h.HandleMessage},
		{Name: "digest", Consumer: digest, Handle: d.HandleMessage},
	}, nil
}

func (t *telegram) Maintain(ctx context.Context) { t.h.CleanupCache(ctx) }

// emailChannel sends a calendar invite for each purchased event.
type emailChannel struct {
	cfg config.Config
	h   *emailhandler.Handler
}

// Email returns the calendar-invite channel.
func Email(cfg config.Config) notifier.Notifier { return &emailChannel{cfg: cfg} }

func (e *emailChannel) Name() string { return "email" }

func (e *emailChannel) Bindings(ctx context.Context, js jetstream.JetStream) ([]notifier.Binding, error) {
	consumer, err := streams.EnsureConsumer(ctx, js, streams.EmailConsumer())
	if err != nil {
		return nil, err
	}
	e.h = emailhandler.New(email.SMTPConfig{
		Host:           e.cfg.SMTPHost,
		Port:           e.cfg.SMTPPort,
		Username:       e.cfg.SMTPUsername,
		Password:       e.cfg.SMTPPassword,
		From:           e.cfg.EmailFrom,
		AllowPlaintext: e.cfg.SMTPAllowPlaintext,
	}, e.cfg.EmailTo, js)
	return []notifier.Binding{{Name: "purchases", Consumer: consumer, Handle: e.h.HandleMessage}}, nil
}

func (e *emailChannel) Maintain(ctx context.Context) { e.h.CleanupCache(ctx) }
