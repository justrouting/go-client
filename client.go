// Package justrouting provides a Go client for the JustRouting API.
//
// A client is created with an API key and, optionally, one or more options:
//
//	client := justrouting.NewClient("YOUR_API_KEY",
//		justrouting.WithHTTPClient(&http.Client{Timeout: 10 * time.Second}))
//
//	route, err := client.Routes.Get(ctx, &justrouting.RouteRequest{
//		Origin:      []float64{103.8198, 1.3521},
//		Destination: []float64{103.9915, 1.3644},
//	})
//	if err != nil {
//		log.Fatal(err)
//	}
//	fmt.Printf("Distance: %.1f km\n", route.Distance/1000)
//
// Coordinates are always [longitude, latitude], the order used by GeoJSON,
// OSRM and VROOM. All requests take a context.Context and are safe for
// concurrent use.
//
// Failed calls return an [*Error]. Use [errors.Is] with the package sentinels
// for control flow and [errors.As] when the status code or raw body is needed:
//
//	if errors.Is(err, justrouting.ErrRateLimited) {
//		// back off and retry later
//	}
//
// The package has no dependencies outside the standard library.
package justrouting

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// Version is the client version, reported in the User-Agent header.
	Version = "0.2.0"

	// DefaultBaseURL is the hosted JustRouting API endpoint.
	DefaultBaseURL = "https://api.justrouting.tech"

	// DefaultProfile is the routing profile used when a request leaves
	// Profile empty.
	DefaultProfile = "driving"

	defaultMaxRetries = 2
	defaultTimeout    = 30 * time.Second
)

// Client is a JustRouting API client. It is safe for concurrent use by
// multiple goroutines, and should be created once and reused so that
// underlying HTTP connections are pooled.
type Client struct {
	apiKey     string
	baseURL    *url.URL
	httpClient *http.Client
	userAgent  string
	maxRetries int
	backoff    func(attempt int) time.Duration

	// initErr holds the first error produced by a ClientOption. NewClient
	// cannot return an error, so it is reported by every request instead.
	initErr error

	// Routes computes routes between two or more coordinates.
	Routes *RoutesService
	// Matrix computes duration and distance matrices.
	Matrix *MatrixService
	// Optimization solves vehicle routing problems.
	Optimization *OptimizationService
	// Geocode converts addresses into coordinates.
	Geocode *GeocodeService
	// Health reports API and upstream availability.
	Health *HealthService
}

// NewClient returns a Client authenticated with apiKey.
//
// The key is sent as an "Authorization: Bearer" header on every request that
// needs it. An empty key is allowed but only [HealthService.Get] will work;
// every other call fails with [ErrUnauthorized] before any request is sent.
//
// Invalid options do not panic. The first option error is retained and
// returned by every subsequent API call, so a misconfigured client surfaces
// the problem at the first request rather than silently using a default.
func NewClient(apiKey string, opts ...ClientOption) *Client {
	base, err := url.Parse(DefaultBaseURL)
	if err != nil {
		// DefaultBaseURL is a compile-time constant and always parses.
		panic("justrouting: invalid DefaultBaseURL: " + err.Error())
	}

	c := &Client{
		apiKey:     strings.TrimSpace(apiKey),
		baseURL:    base,
		httpClient: &http.Client{Timeout: defaultTimeout},
		userAgent:  "justrouting-go/" + Version,
		maxRetries: defaultMaxRetries,
		backoff:    defaultBackoff,
	}

	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(c); err != nil && c.initErr == nil {
			c.initErr = err
		}
	}

	c.Routes = &RoutesService{client: c}
	c.Matrix = &MatrixService{client: c}
	c.Optimization = &OptimizationService{client: c}
	c.Geocode = &GeocodeService{client: c}
	c.Health = &HealthService{client: c}

	return c
}

// BaseURL returns the API endpoint the client sends requests to.
func (c *Client) BaseURL() string { return c.baseURL.String() }

// profileOrDefault returns the profile to use for a request path.
func profileOrDefault(profile string) string {
	if profile == "" {
		return DefaultProfile
	}
	return url.PathEscape(profile)
}
