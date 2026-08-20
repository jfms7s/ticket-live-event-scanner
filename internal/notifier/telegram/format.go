package telegram

import (
	"strings"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
)

// formatTelegramMessage formats the event into a human-readable Telegram message with HTML escaping
func formatTelegramMessage(disc event.Discovered) string {
	var buf strings.Builder

	buf.WriteString("<b>")
	buf.WriteString(htmlEscape(disc.Title))
	buf.WriteString("</b>\n")

	if disc.Venue != "" {
		buf.WriteString("<i>")
		buf.WriteString(htmlEscape(disc.Venue))
		buf.WriteString("</i>\n")
	}

	if disc.Category != "" {
		buf.WriteString("📂 <code>")
		buf.WriteString(htmlEscape(disc.Category))
		buf.WriteString("</code>\n")
	}

	if disc.EventDate != "" {
		buf.WriteString("📅 ")
		buf.WriteString(htmlEscape(strings.Replace(disc.EventDate, "T", " ", 1)))
		buf.WriteString("\n")
	}

	if disc.URL != "" {
		buf.WriteString("<a href=\"")
		buf.WriteString(htmlEscape(disc.URL))
		buf.WriteString("\">View Event</a>")
	}

	return buf.String()
}

// htmlEscape escapes HTML special characters for Telegram's HTML parse_mode
func htmlEscape(s string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		"\"", "&quot;",
		"'", "&#39;",
	).Replace(s)
}
