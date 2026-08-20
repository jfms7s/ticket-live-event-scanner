// Package telegram sends event notifications through the Telegram Bot API.
package telegram

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

// maxPhotoCaptionLen is Telegram's limit on sendPhoto captions — shorter
// than sendMessage's 4096-char text limit, so a message that doesn't fit
// falls back to a plain text message instead of a captioned photo.
const maxPhotoCaptionLen = 1024

// SendTelegramMessage sends a message to Telegram and returns the message_id on success.
func SendTelegramMessage(ctx context.Context, botToken, chatID string, disc event.Discovered) (string, error) {
	// Use a client with timeout to prevent blocking indefinitely
	// Set to 20s to leave margin before JetStream's 30s AckWait timeout
	client := &http.Client{Timeout: 20 * time.Second}
	apiBaseURL := fmt.Sprintf("https://api.telegram.org/bot%s", botToken)
	return sendTelegramMessage(ctx, client, apiBaseURL, chatID, disc)
}

// sendTelegramMessage sends disc to chatID via the Telegram Bot API rooted
// at apiBaseURL, returning the resulting message_id. When disc has an
// image and the formatted message fits Telegram's caption limit, it's sent
// as a photo with the event details as its caption (so the poster shows
// inline in the chat); otherwise it falls back to a plain text message.
func sendTelegramMessage(ctx context.Context, client *http.Client, apiBaseURL, chatID string, disc event.Discovered) (string, error) {
	// Format message with escaped user-controlled text
	// Using HTML parse mode and escaping HTML special characters
	messageText := formatTelegramMessage(disc)

	method := "sendMessage"
	data := url.Values{
		"chat_id":    {chatID},
		"text":       {messageText},
		"parse_mode": {"HTML"},
	}

	if disc.ImageURL != "" && len(messageText) <= maxPhotoCaptionLen {
		method = "sendPhoto"
		data = url.Values{
			"chat_id":    {chatID},
			"photo":      {disc.ImageURL},
			"caption":    {messageText},
			"parse_mode": {"HTML"},
		}
	}

	apiURL := fmt.Sprintf("%s/%s", apiBaseURL, method)

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, strings.NewReader(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

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
