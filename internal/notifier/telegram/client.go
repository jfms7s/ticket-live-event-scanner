// Package telegram sends event notifications through the Telegram Bot API.
package telegram

import (
	"context"
	"encoding/json"
	"errors"
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
		return "", requestError(method, apiBaseURL, err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return "", requestError(method, apiBaseURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return "", rateLimited(body)
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

// requestError wraps a request-creation or transport error without the
// request URL, which embeds the bot token. net/http returns these as
// *url.Error, whose text includes the full URL, so it's unwrapped to its
// cause; redactToken is a backstop for any cause that repeats the URL.
func requestError(method, apiBaseURL string, err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return redactToken(fmt.Errorf("telegram %s request failed: %w", method, err), apiBaseURL)
}

// redactToken replaces the bot token (the part of apiBaseURL after "/bot")
// in err's text with "<redacted>".
func redactToken(err error, apiBaseURL string) error {
	i := strings.LastIndex(apiBaseURL, "/bot")
	if i < 0 {
		return err
	}
	token := apiBaseURL[i+len("/bot"):]
	if token == "" || !strings.Contains(err.Error(), token) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), token, "<redacted>"))
}

// defaultRetryAfter is used when a 429 doesn't say how long to wait.
const defaultRetryAfter = 30 * time.Second

// RateLimitedError is Telegram's 429 Too Many Requests: the bot must wait
// RetryAfter before sending again. Its text carries no URL or token.
type RateLimitedError struct {
	RetryAfter time.Duration
}

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("telegram rate limited, retry after %v", e.RetryAfter)
}

// rateLimited builds the RateLimitedError for a 429 response body
// ({"parameters":{"retry_after":N}}, N in seconds).
func rateLimited(body []byte) error {
	var resp struct {
		Parameters struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	retryAfter := defaultRetryAfter
	if err := json.Unmarshal(body, &resp); err == nil && resp.Parameters.RetryAfter > 0 {
		retryAfter = time.Duration(resp.Parameters.RetryAfter) * time.Second
	}
	return &RateLimitedError{RetryAfter: retryAfter}
}
