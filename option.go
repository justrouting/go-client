package justrouting

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// A ClientOption configures a Client. Options are applied by [NewClient] in
// the order they are given.
type ClientOption func(*Client) error

// WithHTTPClient sets the HTTP client used for all requests. Use it to control
// timeouts, proxies, or transport-level instrumentation:
//
//	justrouting.NewClient(key, justrouting.WithHTTPClient(&http.Client{
//		Timeout: 10 * time.Second,
//	}))
//
// The timeout on the supplied client covers a single HTTP attempt, not the
// whole retry sequence; use a context deadline to bound the total call.
func WithHTTPClient(hc *http.Client) ClientOption {
	return func(c *Client) error {
		if hc == nil {
			return errors.New("justrouting: WithHTTPClient: http client must not be nil")
		}
		c.httpClient = hc
		return nil
	}
}

// WithBaseURL points the client at a different API endpoint, such as a local
// development server:
//
//	justrouting.WithBaseURL("http://localhost:8080")
//
// The URL must be absolute. Any path is kept as a prefix for every request.
func WithBaseURL(raw string) ClientOption {
	return func(c *Client) error {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return errors.New("justrouting: WithBaseURL: url must not be empty")
		}
		u, err := url.Parse(trimmed)
		if err != nil {
			return fmt.Errorf("justrouting: WithBaseURL: %w", err)
		}
		if u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("justrouting: WithBaseURL: %q is not an absolute URL", raw)
		}
		c.baseURL = u
		return nil
	}
}

// WithUserAgent overrides the User-Agent header. Identifying your application
// helps when diagnosing traffic against the API.
func WithUserAgent(ua string) ClientOption {
	return func(c *Client) error {
		if strings.TrimSpace(ua) == "" {
			return errors.New("justrouting: WithUserAgent: user agent must not be empty")
		}
		c.userAgent = ua
		return nil
	}
}

// WithMaxRetries sets how many times a failed request is retried, on top of
// the initial attempt. The default is 2. Pass 0 to disable retries.
//
// Only rate-limit (429), server (5xx) and transport errors are retried; other
// 4xx responses are returned immediately.
func WithMaxRetries(n int) ClientOption {
	return func(c *Client) error {
		if n < 0 {
			return fmt.Errorf("justrouting: WithMaxRetries: must not be negative, got %d", n)
		}
		c.maxRetries = n
		return nil
	}
}

// WithBackoff replaces the delay function used between retries. attempt is
// 1 for the first retry, 2 for the second, and so on.
//
// The default is exponential backoff with jitter, starting at 500ms and capped
// at 8s. A Retry-After response header, when present, takes precedence over
// this function.
func WithBackoff(fn func(attempt int) time.Duration) ClientOption {
	return func(c *Client) error {
		if fn == nil {
			return errors.New("justrouting: WithBackoff: function must not be nil")
		}
		c.backoff = fn
		return nil
	}
}
