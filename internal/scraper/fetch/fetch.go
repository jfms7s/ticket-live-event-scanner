// Package fetch performs size-capped HTTP GETs against ticketline.pt,
// restricted to a small set of allowed paths on the base URL's scheme and
// host, on the first request and on every redirect hop.
package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// maxBodySize limits response bodies to 5MB to prevent unbounded memory
// consumption.
const maxBodySize = 5 * 1024 * 1024

// maxRedirects caps how many redirect hops a single Fetch follows.
const maxRedirects = 5

// Fetcher performs GETs against a restricted set of allowed paths (see
// validateURL). Redirects are followed only while they stay inside the
// same allowlist.
type Fetcher struct {
	client    *http.Client
	userAgent string
}

// New constructs a Fetcher that sends the given User-Agent and times out
// requests after timeout.
func New(userAgent string, timeout time.Duration) *Fetcher {
	return &Fetcher{
		client: &http.Client{
			Timeout:       timeout,
			CheckRedirect: checkRedirect,
		},
		userAgent: userAgent,
	}
}

// checkRedirect re-applies validateURL to every redirect hop, using the
// scheme and host of the original request as the base.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return errors.New("too many redirects")
	}
	first := via[0].URL
	base := first.Scheme + "://" + first.Host
	if err := validateURL(req.URL.String(), base); err != nil {
		return fmt.Errorf("redirect refused: %w", err)
	}
	return nil
}

// Fetch retrieves urlStr, which must resolve under baseURL to one of the
// allowed paths (see validateURL), and returns the body of a 200 response.
func (f *Fetcher) Fetch(ctx context.Context, urlStr, baseURL string) (string, error) {
	if err := validateURL(urlStr, baseURL); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("User-Agent", f.userAgent)

	resp, err := f.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	limitedBody := io.LimitReader(resp.Body, int64(maxBodySize)+1)
	body, err := io.ReadAll(limitedBody)
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}

	if len(body) > maxBodySize {
		return "", fmt.Errorf("response body exceeds maximum size of %d bytes", maxBodySize)
	}

	return string(body), nil
}
