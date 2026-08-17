package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jfms7s/ticket-live-event-scanner/internal/event"
)

// MessageAction represents the decision to make on a failed message
type MessageAction int

const (
	// ActionAck means the message was successfully processed
	ActionAck MessageAction = iota
	// ActionNak means retry the message after a delay
	ActionNak
	// ActionTerm means stop retrying this message permanently
	ActionTerm
)

// DecideAction determines whether to nak with delay or terminate a message
// based on the number of deliveries and the maximum allowed.
func DecideAction(numDelivered, maxDeliver int) MessageAction {
	if numDelivered >= maxDeliver {
		return ActionTerm
	}
	return ActionNak
}

// BackoffDelay calculates linear backoff delay: numDelivered * 5 seconds
func BackoffDelay(numDelivered int) time.Duration {
	// Cap at 5 minutes to avoid excessively long delays
	maxDelay := 5 * time.Minute
	delay := time.Duration(numDelivered) * 5 * time.Second
	if delay > maxDelay {
		delay = maxDelay
	}
	return delay
}

// SendTelegramMessage sends a message to Telegram and returns the message_id on success.
func SendTelegramMessage(ctx context.Context, botToken, chatID string, disc event.Discovered) (string, error) {
	// Format message with escaped user-controlled text
	// Using HTML parse mode and escaping HTML special characters
	messageText := formatTelegramMessage(disc)

	// Build request
	apiURL := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)

	data := url.Values{
		"chat_id":    {chatID},
		"text":       {messageText},
		"parse_mode": {"HTML"},
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	// Use a client with timeout to prevent blocking indefinitely
	// Set to 20s to leave margin before JetStream's 30s AckWait timeout
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}

	// Check HTTP status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("telegram API returned %d: %s", resp.StatusCode, string(body))
	}

	// Parse Telegram API response
	var telegramResp struct {
		OK     bool `json:"ok"`
		Error  int  `json:"error_code"`
		Result struct {
			MessageID int64 `json:"message_id"`
		} `json:"result"`
	}

	if err := json.Unmarshal(body, &telegramResp); err != nil {
		return "", fmt.Errorf("parse telegram response: %w", err)
	}

	if !telegramResp.OK {
		// Extract error message if available
		if telegramResp.Error != 0 {
			return "", fmt.Errorf("telegram error code %d", telegramResp.Error)
		}
		return "", fmt.Errorf("telegram API returned ok=false")
	}

	return strconv.FormatInt(telegramResp.Result.MessageID, 10), nil
}

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
		buf.WriteString(htmlEscape(disc.EventDate))
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

// now returns the current UTC time (extracted to allow mocking in tests)
func now() time.Time {
	return time.Now().UTC()
}
