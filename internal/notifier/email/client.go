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
	"mime/multipart"
	"net"
	"net/smtp"
	"net/textproto"
	"strings"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
)

// smtpDialTimeout leaves margin under JetStream's 30s AckWait, matching
// the timeout used by the Telegram client.
const smtpDialTimeout = 20 * time.Second

// SMTPConfig holds the SMTP relay credentials used to send calendar
// invites.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
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

	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("From: %s\r\n", from))
	buf.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(to, ", ")))
	buf.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	buf.WriteString(fmt.Sprintf("Message-ID: %s\r\n", messageID))
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

// sendViaSMTP dials cfg's SMTP relay, negotiates STARTTLS/AUTH when
// offered, and sends msg to to. Dialing is bounded by smtpDialTimeout so a
// hung connection can't outlive JetStream's AckWait.
func sendViaSMTP(ctx context.Context, cfg SMTPConfig, to []string, msg []byte) error {
	dialCtx, cancel := context.WithTimeout(ctx, smtpDialTimeout)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(dialCtx, "tcp", fmt.Sprintf("%s:%d", cfg.Host, cfg.Port))
	if err != nil {
		return fmt.Errorf("dial smtp server: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return fmt.Errorf("create smtp client: %w", err)
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: cfg.Host}); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}

	if cfg.Username != "" {
		if ok, _ := client.Extension("AUTH"); ok {
			auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
			if err := client.Auth(auth); err != nil {
				return fmt.Errorf("smtp auth: %w", err)
			}
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
