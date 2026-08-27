package justrouting

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testAPIKey = "0123456789abcdef0123456789abcdef"

// newTestClient starts a server running handler and returns a client aimed at
// it. Backoff is zeroed so retry tests run instantly.
func newTestClient(t *testing.T, handler http.HandlerFunc, opts ...ClientOption) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	base := []ClientOption{
		WithBaseURL(srv.URL),
		WithBackoff(func(int) time.Duration { return 0 }),
	}
	return NewClient(testAPIKey, append(base, opts...)...)
}

// jsonHandler replies with a fixed status and body.
func jsonHandler(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func TestNewClientDefaults(t *testing.T) {
	c := NewClient("key")

	if got := c.BaseURL(); got != DefaultBaseURL {
		t.Errorf("BaseURL() = %q, want %q", got, DefaultBaseURL)
	}
	if c.maxRetries != defaultMaxRetries {
		t.Errorf("maxRetries = %d, want %d", c.maxRetries, defaultMaxRetries)
	}
	if c.userAgent != "justrouting-go/"+Version {
		t.Errorf("userAgent = %q", c.userAgent)
	}
	for name, svc := range map[string]any{
		"Routes":       c.Routes,
		"Matrix":       c.Matrix,
		"Optimization": c.Optimization,
		"Health":       c.Health,
	} {
		if svc == nil {
			t.Errorf("service %s is nil", name)
		}
	}
}

func TestNewClientTrimsAPIKey(t *testing.T) {
	if c := NewClient("  spaced-key\n"); c.apiKey != "spaced-key" {
		t.Errorf("apiKey = %q, want %q", c.apiKey, "spaced-key")
	}
}

func TestOptions(t *testing.T) {
	t.Run("WithHTTPClient", func(t *testing.T) {
		hc := &http.Client{Timeout: time.Second}
		if c := NewClient("k", WithHTTPClient(hc)); c.httpClient != hc {
			t.Error("http client was not applied")
		}
	})

	t.Run("WithBaseURL", func(t *testing.T) {
		c := NewClient("k", WithBaseURL("http://localhost:8080/"))
		if c.initErr != nil {
			t.Fatalf("unexpected init error: %v", c.initErr)
		}
		if got := c.BaseURL(); got != "http://localhost:8080/" {
			t.Errorf("BaseURL() = %q", got)
		}
	})

	t.Run("WithUserAgent", func(t *testing.T) {
		if c := NewClient("k", WithUserAgent("my-app/2.0")); c.userAgent != "my-app/2.0" {
			t.Errorf("userAgent = %q", c.userAgent)
		}
	})

	t.Run("WithMaxRetries", func(t *testing.T) {
		if c := NewClient("k", WithMaxRetries(0)); c.maxRetries != 0 {
			t.Errorf("maxRetries = %d, want 0", c.maxRetries)
		}
	})

	t.Run("nil option is ignored", func(t *testing.T) {
		if c := NewClient("k", nil); c.initErr != nil {
			t.Errorf("unexpected init error: %v", c.initErr)
		}
	})
}

// A bad option must not panic; it should surface at the first API call.
func TestInvalidOptionDeferredToRequest(t *testing.T) {
	cases := map[string]ClientOption{
		"relative base URL": WithBaseURL("localhost:8080"),
		"empty base URL":    WithBaseURL("  "),
		"nil http client":   WithHTTPClient(nil),
		"empty user agent":  WithUserAgent(""),
		"negative retries":  WithMaxRetries(-1),
		"nil backoff":       WithBackoff(nil),
	}

	for name, opt := range cases {
		t.Run(name, func(t *testing.T) {
			c := NewClient("k", opt)
			if c.initErr == nil {
				t.Fatal("expected an init error to be recorded")
			}
			if _, err := c.Health.Get(context.Background()); err == nil {
				t.Fatal("expected the init error to be returned by the request")
			}
		})
	}
}

// Only the first option error is kept, so the root cause is not masked.
func TestFirstOptionErrorWins(t *testing.T) {
	c := NewClient("k", WithMaxRetries(-1), WithUserAgent(""))
	if c.initErr == nil {
		t.Fatal("expected an init error")
	}
	if got := c.initErr.Error(); !strings.Contains(got, "WithMaxRetries") {
		t.Errorf("initErr = %q, want the first failing option", got)
	}
}

func TestRequestHeaders(t *testing.T) {
	var got http.Header
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Write([]byte(`{"code":"Ok","routes":[{"distance":1}],"waypoints":[]}`))
	}, WithUserAgent("test-agent/1.0"))

	if _, err := c.Routes.Get(context.Background(), simpleRoute()); err != nil {
		t.Fatalf("Get: %v", err)
	}

	if want := "Bearer " + testAPIKey; got.Get("Authorization") != want {
		t.Errorf("Authorization = %q, want %q", got.Get("Authorization"), want)
	}
	if got.Get("User-Agent") != "test-agent/1.0" {
		t.Errorf("User-Agent = %q", got.Get("User-Agent"))
	}
	if got.Get("Accept") != "application/json" {
		t.Errorf("Accept = %q", got.Get("Accept"))
	}
	// GET requests carry no body, so no Content-Type should be sent.
	if ct := got.Get("Content-Type"); ct != "" {
		t.Errorf("Content-Type = %q, want empty on a GET", ct)
	}
}

// An empty key must fail before a request is sent, rather than burning a round
// trip to learn what the client already knows.
func TestMissingAPIKeyFailsFast(t *testing.T) {
	var called bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	t.Cleanup(srv.Close)

	c := NewClient("", WithBaseURL(srv.URL))
	_, err := c.Routes.Get(context.Background(), simpleRoute())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized", err)
	}
	if called {
		t.Error("no request should have been sent")
	}
}

// Health needs no credentials, so it must work on a keyless client.
func TestHealthWorksWithoutAPIKey(t *testing.T) {
	srv := httptest.NewServer(jsonHandler(http.StatusOK,
		`{"status":"ok","timestamp":"2026-08-26T12:00:00Z","upstreams":{"osrm":true,"vroom":true}}`))
	t.Cleanup(srv.Close)

	c := NewClient("", WithBaseURL(srv.URL))
	health, err := c.Health.Get(context.Background())
	if err != nil {
		t.Fatalf("Health.Get: %v", err)
	}
	if !health.OK() {
		t.Errorf("OK() = false, want true (status %q)", health.Status)
	}
	if !health.Upstreams["osrm"] {
		t.Error("upstream osrm should be healthy")
	}
}

func TestHealthDegraded(t *testing.T) {
	c := newTestClient(t, jsonHandler(http.StatusOK,
		`{"status":"degraded","timestamp":"2026-08-26T12:00:00Z","upstreams":{"osrm":true,"vroom":false}}`))

	health, err := c.Health.Get(context.Background())
	if err != nil {
		t.Fatalf("Health.Get: %v", err)
	}
	if health.OK() {
		t.Error("OK() = true, want false")
	}
}

func TestBaseURLPathPrefixIsPreserved(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(testAPIKey, WithBaseURL(srv.URL+"/gateway"))
	if _, err := c.Health.Get(context.Background()); err != nil {
		t.Fatalf("Health.Get: %v", err)
	}
	if want := "/gateway/health"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

func TestNilContextRejected(t *testing.T) {
	c := newTestClient(t, jsonHandler(http.StatusOK, `{"status":"ok"}`))
	//lint:ignore SA1012 deliberately exercising the nil-context guard
	_, err := c.Health.Get(nil)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("err = %v, want ErrInvalidRequest", err)
	}
}

func simpleRoute() *RouteRequest {
	return &RouteRequest{
		Origin:      []float64{103.8198, 1.3521},
		Destination: []float64{103.9915, 1.3644},
	}
}
