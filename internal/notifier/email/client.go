// Package email sends calendar invitations (METHOD:REQUEST, with
// Accept/Decline/Maybe controls in Gmail/Outlook/Apple Mail, plus a
// fallback .ics attachment) through a plain SMTP relay.
package email

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"mime"
	"mime/multipart"
	"net"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
)

// smtpTimeout bounds the whole SMTP conversation, from dial to QUIT. It
// leaves margin under JetStream's 30s AckWait, matching the timeout used
// by the Telegram client. A variable so tests can shorten it.
var smtpTimeout = 20 * time.Second

// SMTPConfig holds the SMTP relay credentials used to send calendar
// invites.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	// AllowPlaintext lets the client send without TLS when the server
	// doesn't offer STARTTLS. Only for local testing against a relay
	// without TLS; off by default.
	AllowPlaintext bool
}

// SendCalendarEmail builds a .ics calendar invite for p and emails it to
// to, returning a locally-generated Message-ID on success. SMTP has no
// server-assigned message ID, so the caller-visible ID is synthesized here
// and embedded as the Message-ID header.
func SendCalendarEmail(ctx context.Context, cfg SMTPConfig, to []string, p event.Purchased) (string, error) {
	icsContent, err := BuildICS(p, cfg.From, to)
	if err != nil {
		return "", err
	}

	messageID := fmt.Sprintf("<event-%d-%d@ticket-live-event-scanner>", p.EventID, time.Now().UnixNano())
	subject := fmt.Sprintf("Calendar invite: %s", p.Title)
	textBody := fmt.Sprintf("Your calendar invite for %s is attached.\n\n%s\n", p.Title, p.URL)

	msg := buildMIMEMessage(cfg.From, to, subject, messageID, textBody, icsContent)

	if err := sendViaSMTP(ctx, cfg, to, msg); err != nil {
		return "", fmt.Errorf("send calendar email for event %d: %w", p.EventID, err)
	}

	return messageID, nil
}

// buildMIMEMessage renders a multipart/mixed RFC 5322 message shaped as a
// real calendar invitation, not just a file attachment: an inner
// multipart/alternative carries the plain-text body alongside an inline
// text/calendar;method=REQUEST part (what Gmail/Outlook/Apple Mail look
// for to render Accept/Decline/Maybe controls), and the outer part also
// attaches the same .ics content as a named file for clients that only
// support opening/importing calendar attachments.
func buildMIMEMessage(from string, to []string, subject, messageID, textBody, icsContent string) []byte {
	// Build the inner multipart/alternative first so its boundary is known
	// before writing the outer part's Content-Type header.
	var innerBody bytes.Buffer
	inner := multipart.NewWriter(&innerBody)

	textHeader := textproto.MIMEHeader{}
	textHeader.Set("Content-Type", "text/plain; charset=UTF-8")
	if textPart, err := inner.CreatePart(textHeader); err == nil {
		textPart.Write([]byte(textBody))
	}

	// Inline (no Content-Disposition: attachment) text/calendar part: this
	// is what makes Gmail/Outlook/Apple Mail render Accept/Decline/Maybe
	// controls instead of just showing a file to download.
	calHeader := textproto.MIMEHeader{}
	calHeader.Set("Content-Type", "text/calendar; method=REQUEST; charset=UTF-8")
	calHeader.Set("Content-Transfer-Encoding", "base64")
	if calPart, err := inner.CreatePart(calHeader); err == nil {
		writeBase64(calPart, icsContent)
	}
	inner.Close()

	var body bytes.Buffer
	outer := multipart.NewWriter(&body)

	recipients := make([]string, len(to))
	for i, addr := range to {
		recipients[i] = headerValue(addr)
	}

	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("From: %s\r\n", headerValue(from)))
	buf.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(recipients, ", ")))
	buf.WriteString(fmt.Sprintf("Subject: %s\r\n", mime.QEncoding.Encode("utf-8", headerValue(subject))))
	buf.WriteString(fmt.Sprintf("Message-ID: %s\r\n", headerValue(messageID)))
	buf.WriteString(fmt.Sprintf("Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z)))
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=%q\r\n", outer.Boundary()))
	buf.WriteString("\r\n")

	altHeader := textproto.MIMEHeader{}
	altHeader.Set("Content-Type", fmt.Sprintf("multipart/alternative; boundary=%q", inner.Boundary()))
	if altPart, err := outer.CreatePart(altHeader); err == nil {
		altPart.Write(innerBody.Bytes())
	}

	// Same .ics content again, this time as a named attachment for clients
	// that only support opening/importing calendar files.
	icsHeader := textproto.MIMEHeader{}
	icsHeader.Set("Content-Type", "text/calendar; method=REQUEST; charset=UTF-8; name=\"invite.ics\"")
	icsHeader.Set("Content-Transfer-Encoding", "base64")
	icsHeader.Set("Content-Disposition", `attachment; filename="invite.ics"`)
	if icsPart, err := outer.CreatePart(icsHeader); err == nil {
		writeBase64(icsPart, icsContent)
	}

	outer.Close()
	buf.Write(body.Bytes())

	return buf.Bytes()
}

// headerValue makes s safe to write as a single header value: CR and LF
// (which would end the header and let scraped text add new ones, such as
// Bcc:) become spaces, and runs of whitespace collapse to one space.
func headerValue(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// writeBase64 writes s to w as base64, wrapped at 76 characters per RFC
// 2045.
func writeBase64(w interface{ Write([]byte) (int, error) }, s string) {
	encoded := base64.StdEncoding.EncodeToString([]byte(s))
	for i := 0; i < len(encoded); i += 76 {
		end := i + 76
		if end > len(encoded) {
			end = len(encoded)
		}
		w.Write([]byte(encoded[i:end] + "\r\n"))
	}
}

// sendViaSMTP dials cfg's SMTP relay, upgrades to TLS with STARTTLS,
// authenticates when credentials are configured, and sends msg to to. It
// refuses to send in cleartext unless cfg.AllowPlaintext is set, and
// refuses to send unauthenticated when credentials are configured but the
// server doesn't offer AUTH. The whole conversation is bounded by
// smtpTimeout (or ctx's earlier deadline) so a stalled relay can't outlive
// JetStream's AckWait, and cancelling ctx ends it.
func sendViaSMTP(ctx context.Context, cfg SMTPConfig, to []string, msg []byte) error {
	deadline := time.Now().Add(smtpTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	sessionCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	err := smtpSession(sessionCtx, cfg, to, msg)
	if err != nil && ctx.Err() != nil {
		return fmt.Errorf("%w: %w", ctx.Err(), err)
	}
	return err
}

// smtpSession runs one SMTP conversation. Its connection's deadline is
// ctx's deadline, and cancelling ctx pulls that deadline in to now so any
// blocked read or write returns.
func smtpSession(ctx context.Context, cfg SMTPConfig, to []string, msg []byte) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)))
	if err != nil {
		return fmt.Errorf("dial smtp server: %w", err)
	}
	defer conn.Close()

	// The deadline survives StartTLS, which wraps this same net.Conn.
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("set smtp deadline: %w", err)
	}
	stop := context.AfterFunc(ctx, func() {
		conn.SetDeadline(time.Now())
	})
	defer stop()

	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return fmt.Errorf("create smtp client: %w", err)
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	} else if !cfg.AllowPlaintext {
		return fmt.Errorf("smtp server %s does not offer STARTTLS (set SMTP_ALLOW_PLAINTEXT to send in cleartext)", cfg.Host)
	}

	if cfg.Username != "" {
		if ok, _ := client.Extension("AUTH"); !ok {
			return fmt.Errorf("smtp server %s does not offer AUTH but credentials are configured", cfg.Host)
		}
		auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
		if err := client.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	if err := client.Mail(cfg.From); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	for _, addr := range to {
		if err := client.Rcpt(addr); err != nil {
			return fmt.Errorf("RCPT TO %s: %w", addr, err)
		}
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		w.Close()
		return fmt.Errorf("write message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("close data writer: %w", err)
	}

	return client.Quit()
}
