package justrouting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// maxResponseBytes bounds how much of a response is read. A 500x500 matrix
// with both annotations is a few megabytes, so the limit is generous.
const maxResponseBytes = 32 << 20

// request describes a single API call, independent of transport concerns.
type request struct {
	method    string
	path      string
	query     url.Values
	body      any
	needsAuth bool
}

// do executes r, retrying transient failures, and decodes a successful
// response into out. Passing a nil out discards the body.
func (c *Client) do(ctx context.Context, r *request, out any) error {
	if c.initErr != nil {
		return c.initErr
	}
	if ctx == nil {
		return fmt.Errorf("%w: context must not be nil", ErrInvalidRequest)
	}
	if r.needsAuth && c.apiKey == "" {
		return &Error{
			StatusCode: http.StatusUnauthorized,
			Message:    "no API key configured; pass one to NewClient",
		}
	}

	// Marshal once and replay the bytes on each attempt, so a retried POST
	// sends an identical body.
	var body []byte
	if r.body != nil {
		var err error
		if body, err = json.Marshal(r.body); err != nil {
			return fmt.Errorf("justrouting: encoding request body: %w", err)
		}
	}

	endpoint := c.endpoint(r.path, r.query)

	var (
		lastErr error
		delay   time.Duration
	)
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			if err := wait(ctx, delay); err != nil {
				return err
			}
		}

		payload, status, header, err := c.roundTrip(ctx, r.method, endpoint, body)
		switch {
		case err != nil:
			// A cancelled or expired context is final, not transient.
			if ctx.Err() != nil {
				return err
			}
			lastErr = err
		case status/100 == 2:
			return decode(payload, out)
		default:
			apiErr := parseError(status, payload)
			if !retryableStatus(status) {
				return apiErr
			}
			lastErr = apiErr
		}

		if attempt >= c.maxRetries {
			return lastErr
		}
		delay = c.nextDelay(attempt+1, header)
	}
}

// roundTrip performs one HTTP attempt and returns the body, status and
// headers. body may be nil for requests without a payload.
func (c *Client) roundTrip(ctx context.Context, method, endpoint string, body []byte) ([]byte, int, http.Header, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("justrouting: building request: %w", err)
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("justrouting: %s %s: %w", method, endpoint, err)
	}
	defer func() {
		// Drain before closing so the connection can be reused.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseBytes))
		_ = resp.Body.Close()
	}()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, resp.StatusCode, resp.Header, fmt.Errorf("justrouting: reading response: %w", err)
	}
	return payload, resp.StatusCode, resp.Header, nil
}

// decode unmarshals a successful response and then checks any in-body status
// code, which both engines can use to report failure alongside HTTP 200.
func decode(payload []byte, out any) error {
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("justrouting: decoding response: %w", err)
	}
	if s, ok := out.(apiStatus); ok {
		if apiErr := s.status(); apiErr != nil {
			apiErr.Body = truncate(payload, maxErrorBodyBytes)
			return apiErr
		}
	}
	return nil
}

// endpoint builds the absolute URL for a request path, preserving any path
// prefix on the configured base URL.
func (c *Client) endpoint(path string, query url.Values) string {
	u := *c.baseURL
	u.Path = strings.TrimSuffix(u.Path, "/") + path
	// Let net/url re-derive the escaped form; the coordinate separators
	// "," and ";" are legal in a path segment and survive unescaped.
	u.RawPath = ""
	u.Fragment = ""
	u.RawQuery = ""
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}
	return u.String()
}

// retryableStatus reports whether a status is worth retrying. Client errors
// other than throttling will fail the same way every time.
func retryableStatus(status int) bool {
	switch status {
	case http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

// nextDelay returns how long to wait before the given retry attempt,
// preferring a Retry-After header when the server sends one.
func (c *Client) nextDelay(attempt int, header http.Header) time.Duration {
	if header != nil {
		if d, ok := parseRetryAfter(header.Get("Retry-After")); ok {
			return d
		}
	}
	return c.backoff(attempt)
}

// parseRetryAfter understands both forms of the Retry-After header: a delay in
// seconds, or an HTTP date.
func parseRetryAfter(value string) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds < 0 {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}
	if t, err := http.ParseTime(value); err == nil {
		if d := time.Until(t); d > 0 {
			return d, true
		}
		return 0, true
	}
	return 0, false
}

// defaultBackoff grows exponentially from 500ms to a cap of 8s, with jitter to
// keep concurrent clients from retrying in lockstep.
func defaultBackoff(attempt int) time.Duration {
	const (
		base     = 500 * time.Millisecond
		maxDelay = 8 * time.Second
	)
	if attempt < 1 {
		attempt = 1
	}
	d := maxDelay
	if attempt <= 20 {
		if shifted := base << (attempt - 1); shifted > 0 && shifted < maxDelay {
			d = shifted
		}
	}
	// Jitter across [d/2, d].
	half := d / 2
	return half + time.Duration(rand.Int64N(int64(half)+1))
}

// wait sleeps for d, returning early if the context is cancelled.
func wait(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		// Still observe cancellation when the delay is zero.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
