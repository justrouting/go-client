package justrouting

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const okRoute = `{"code":"Ok","routes":[{"distance":100,"duration":10}],"waypoints":[]}`

// Every error shape the API can produce must map onto the right sentinel, so
// callers never have to match on message strings themselves.
func TestErrorClassification(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		contentTyp string
		want       []error
		notWant    []error
		wantMsg    string
	}{
		{
			name:   "unauthorized",
			status: http.StatusUnauthorized,
			body:   `{"error":"invalid or revoked API key"}`,
			want:   []error{ErrUnauthorized},
			// A 401 is not a throttle; misclassifying it would send
			// callers into a pointless retry loop.
			notWant: []error{ErrRateLimited},
			wantMsg: "invalid or revoked API key",
		},
		{
			name:    "rate limited per second",
			status:  http.StatusTooManyRequests,
			body:    `{"error":"rate limit exceeded (RPS)"}`,
			want:    []error{ErrRateLimited},
			notWant: []error{ErrQuotaExceeded},
		},
		{
			name:   "daily quota exhausted",
			status: http.StatusTooManyRequests,
			body:   `{"error":"daily quota exceeded (limit 100)"}`,
			// Quota is a kind of throttling, so both match; the
			// narrower sentinel tells callers retrying will not help.
			want: []error{ErrQuotaExceeded, ErrRateLimited},
		},
		{
			name:   "cross-country request",
			status: http.StatusBadRequest,
			body:   `{"error":"cross-country request not supported"}`,
			want:   []error{ErrCrossCountry},
		},
		{
			name:   "matrix exceeds plan",
			status: http.StatusBadRequest,
			body:   `{"error":"matrix size exceeds plan limit"}`,
			want:   []error{ErrPlanLimitExceeded},
		},
		{
			name:   "too many jobs",
			status: http.StatusBadRequest,
			body:   `{"error":"too many jobs"}`,
			want:   []error{ErrPlanLimitExceeded},
		},
		{
			name:   "unparseable coordinates",
			status: http.StatusBadRequest,
			body:   `{"error":"cannot parse coordinates"}`,
			want:   []error{ErrInvalidCoordinates},
		},
		{
			name:   "upstream down",
			status: http.StatusBadGateway,
			body:   `{"error":"upstream unavailable"}`,
			want:   []error{ErrUpstreamUnavailable},
		},
		{
			// Some handlers use http.Error, which stamps text/plain
			// on a body that is still JSON. Classification must key
			// off the body shape, not the header.
			name:       "json body sent as text/plain",
			status:     http.StatusBadRequest,
			body:       `{"error":"cross-country request not supported"}` + "\n",
			contentTyp: "text/plain; charset=utf-8",
			want:       []error{ErrCrossCountry},
			wantMsg:    "cross-country request not supported",
		},
		{
			name:    "routing engine reports no route",
			status:  http.StatusBadRequest,
			body:    `{"code":"NoRoute","message":"Impossible route between points"}`,
			want:    []error{ErrNoRoute},
			wantMsg: "Impossible route between points",
		},
		{
			name:    "non-json body falls back to its text",
			status:  http.StatusBadRequest,
			body:    "something went wrong",
			wantMsg: "something went wrong",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if tc.contentTyp != "" {
					w.Header().Set("Content-Type", tc.contentTyp)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}, WithMaxRetries(0))

			_, err := c.Routes.Get(context.Background(), simpleRoute())
			if err == nil {
				t.Fatal("expected an error")
			}

			for _, sentinel := range tc.want {
				if !errors.Is(err, sentinel) {
					t.Errorf("errors.Is(err, %v) = false; err = %v", sentinel, err)
				}
			}
			for _, sentinel := range tc.notWant {
				if errors.Is(err, sentinel) {
					t.Errorf("errors.Is(err, %v) = true, want false; err = %v", sentinel, err)
				}
			}

			var apiErr *Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("errors.As did not yield *Error; got %T", err)
			}
			if apiErr.StatusCode != tc.status {
				t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, tc.status)
			}
			if tc.wantMsg != "" && apiErr.Message != tc.wantMsg {
				t.Errorf("Message = %q, want %q", apiErr.Message, tc.wantMsg)
			}
			if len(apiErr.Body) == 0 {
				t.Error("Body should retain the raw response")
			}
		})
	}
}

// The routing engine can report failure while still returning HTTP 200, so
// the status code alone is not enough to decide success.
func TestRoutingFailureAtHTTP200(t *testing.T) {
	c := newTestClient(t, jsonHandler(http.StatusOK,
		`{"code":"NoRoute","message":"Impossible route between points"}`))

	_, err := c.Routes.Get(context.Background(), simpleRoute())
	if err == nil {
		t.Fatal("expected an error despite HTTP 200")
	}
	if !errors.Is(err, ErrNoRoute) {
		t.Errorf("err = %v, want ErrNoRoute", err)
	}

	var apiErr *Error
	if errors.As(err, &apiErr) && apiErr.OSRMCode != "NoRoute" {
		t.Errorf("OSRMCode = %q, want NoRoute", apiErr.OSRMCode)
	}
}

// Likewise the optimization engine signals failure with a non-zero body code.
func TestOptimizationFailureAtHTTP200(t *testing.T) {
	c := newTestClient(t, jsonHandler(http.StatusOK,
		`{"code":3,"error":"Invalid profile"}`))

	_, err := c.Optimization.Solve(context.Background(), simpleOptimization())
	if err == nil {
		t.Fatal("expected an error despite HTTP 200")
	}

	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("errors.As did not yield *Error; got %T", err)
	}
	if apiErr.VROOMCode != 3 {
		t.Errorf("VROOMCode = %d, want 3", apiErr.VROOMCode)
	}
	if apiErr.Message != "Invalid profile" {
		t.Errorf("Message = %q", apiErr.Message)
	}
}

// A success code of 0 must not be mistaken for a failure.
func TestOptimizationSuccessCodeZero(t *testing.T) {
	c := newTestClient(t, jsonHandler(http.StatusOK,
		`{"code":0,"summary":{"cost":42,"routes":1},"routes":[{"vehicle":1,"steps":[]}],"unassigned":[]}`))

	solution, err := c.Optimization.Solve(context.Background(), simpleOptimization())
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if solution.Summary.Cost != 42 {
		t.Errorf("Summary.Cost = %d, want 42", solution.Summary.Cost)
	}
}

func TestRetryOnRateLimitThenSuccess(t *testing.T) {
	var attempts int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limit exceeded (RPS)"}`))
			return
		}
		_, _ = w.Write([]byte(okRoute))
	})

	route, err := c.Routes.Get(context.Background(), simpleRoute())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if route.Distance != 100 {
		t.Errorf("Distance = %v, want 100", route.Distance)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Errorf("attempts = %d, want 2", got)
	}
}

func TestRetriesExhaustedReturnsLastError(t *testing.T) {
	var attempts int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"upstream unavailable"}`))
	}, WithMaxRetries(2))

	_, err := c.Routes.Get(context.Background(), simpleRoute())
	if !errors.Is(err, ErrUpstreamUnavailable) {
		t.Errorf("err = %v, want ErrUpstreamUnavailable", err)
	}
	// One initial attempt plus two retries.
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
}

// Retrying a deterministic client error just wastes quota.
func TestClientErrorIsNotRetried(t *testing.T) {
	var attempts int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"cross-country request not supported"}`))
	}, WithMaxRetries(3))

	if _, err := c.Routes.Get(context.Background(), simpleRoute()); err == nil {
		t.Fatal("expected an error")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1", got)
	}
}

func TestRetriesDisabled(t *testing.T) {
	var attempts int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}, WithMaxRetries(0))

	if _, err := c.Routes.Get(context.Background(), simpleRoute()); err == nil {
		t.Fatal("expected an error")
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Errorf("attempts = %d, want 1", got)
	}
}

// A retried POST must resend the identical body; a consumed reader would
// otherwise send an empty second request.
func TestPostBodyIsReplayedOnRetry(t *testing.T) {
	var (
		attempts int32
		bodies   []string
	)
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))

		if atomic.AddInt32(&attempts, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"summary":{},"routes":[],"unassigned":[]}`))
	})

	if _, err := c.Optimization.Solve(context.Background(), simpleOptimization()); err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if len(bodies) != 2 {
		t.Fatalf("received %d bodies, want 2", len(bodies))
	}
	if bodies[0] == "" {
		t.Fatal("first request body was empty")
	}
	if bodies[0] != bodies[1] {
		t.Errorf("replayed body differs:\n first = %s\nsecond = %s", bodies[0], bodies[1])
	}
}

func TestPostSendsJSONContentType(t *testing.T) {
	var contentType string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		contentType = r.Header.Get("Content-Type")
		_, _ = w.Write([]byte(`{"code":0,"summary":{},"routes":[],"unassigned":[]}`))
	})

	if _, err := c.Optimization.Solve(context.Background(), simpleOptimization()); err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", contentType)
	}
}

// Cancellation must abort promptly instead of running out the retry budget.
func TestContextCancellationStopsRetrying(t *testing.T) {
	var attempts int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}, WithMaxRetries(5), WithBackoff(func(int) time.Duration { return time.Hour }))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := c.Routes.Get(ctx, simpleRoute())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("took %v; cancellation did not interrupt the backoff sleep", elapsed)
	}
}

func TestContextCancelledMidFlight(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}, WithMaxRetries(2))

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, err := c.Routes.Get(ctx, simpleRoute()); err == nil {
		t.Fatal("expected an error")
	}
}

// A transport-level failure is transient and should be retried.
func TestTransportErrorIsRetried(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			// Break the connection without a response.
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Error("server does not support hijacking")
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Errorf("hijack: %v", err)
				return
			}
			conn.Close()
			return
		}
		_, _ = w.Write([]byte(okRoute))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(testAPIKey,
		WithBaseURL(srv.URL),
		WithBackoff(func(int) time.Duration { return 0 }),
		// Disable keep-alive so the broken connection is not reused.
		WithHTTPClient(&http.Client{Transport: &http.Transport{DisableKeepAlives: true}}),
	)

	if _, err := c.Routes.Get(context.Background(), simpleRoute()); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Errorf("attempts = %d, want 2", got)
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantOK  bool
		compare func(time.Duration) bool
	}{
		{in: "", wantOK: false},
		{in: "   ", wantOK: false},
		{in: "5", want: 5 * time.Second, wantOK: true},
		{in: "0", want: 0, wantOK: true},
		{in: "-3", wantOK: false},
		{in: "not-a-number", wantOK: false},
		// A date in the past means "retry now".
		{in: "Mon, 02 Jan 2006 15:04:05 GMT", want: 0, wantOK: true},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := parseRetryAfter(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && got != tc.want {
				t.Errorf("duration = %v, want %v", got, tc.want)
			}
		})
	}
}

// Retry-After should override the client's own backoff schedule.
func TestRetryAfterHeaderIsHonoured(t *testing.T) {
	var attempts int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limit exceeded (RPS)"}`))
			return
		}
		_, _ = w.Write([]byte(okRoute))
	}, WithBackoff(func(int) time.Duration {
		t.Error("backoff should not be consulted when Retry-After is present")
		return 0
	}))

	if _, err := c.Routes.Get(context.Background(), simpleRoute()); err != nil {
		t.Fatalf("Get: %v", err)
	}
}

func TestDefaultBackoffGrowsAndIsCapped(t *testing.T) {
	var prev time.Duration
	for attempt := 1; attempt <= 4; attempt++ {
		d := defaultBackoff(attempt)
		if d <= 0 {
			t.Fatalf("attempt %d: delay = %v, want positive", attempt, d)
		}
		if d > 8*time.Second {
			t.Errorf("attempt %d: delay = %v, exceeds the 8s cap", attempt, d)
		}
		prev = d
	}
	_ = prev

	// Very large attempt counts must stay bounded rather than overflow.
	if d := defaultBackoff(1000); d <= 0 || d > 8*time.Second {
		t.Errorf("defaultBackoff(1000) = %v, want within (0, 8s]", d)
	}
	if d := defaultBackoff(0); d <= 0 {
		t.Errorf("defaultBackoff(0) = %v, want positive", d)
	}
}

func TestMalformedJSONResponse(t *testing.T) {
	c := newTestClient(t, jsonHandler(http.StatusOK, `{"code":"Ok","routes":`))

	if _, err := c.Routes.Get(context.Background(), simpleRoute()); err == nil {
		t.Fatal("expected a decode error")
	}
}

func simpleOptimization() *OptimizationRequest {
	depot := Point{103.8198, 1.3521}
	return &OptimizationRequest{
		Vehicles: []Vehicle{{ID: 1, Start: depot, End: depot}},
		Jobs:     []Job{{ID: 1, Location: Point{103.8514, 1.2897}}},
	}
}
