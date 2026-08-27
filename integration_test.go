//go:build integration

// Integration tests run against a live API and are excluded from the default
// build. Run them with:
//
//	go test -tags=integration ./...                    # health only
//	JUSTROUTING_API_KEY=<key> go test -tags=integration ./...
//
// Set JUSTROUTING_BASE_URL to target a local server instead of production.
package justrouting

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func integrationClient(t *testing.T) *Client {
	t.Helper()
	opts := []ClientOption{}
	if base := os.Getenv("JUSTROUTING_BASE_URL"); base != "" {
		opts = append(opts, WithBaseURL(base))
	}
	return NewClient(os.Getenv("JUSTROUTING_API_KEY"), opts...)
}

func requireAPIKey(t *testing.T) {
	t.Helper()
	if os.Getenv("JUSTROUTING_API_KEY") == "" {
		t.Skip("set JUSTROUTING_API_KEY to run this test")
	}
}

func integrationContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// Health needs no credentials, so it doubles as a connectivity check.
func TestIntegrationHealth(t *testing.T) {
	client := integrationClient(t)

	health, err := client.Health.Get(integrationContext(t))
	if err != nil {
		t.Fatalf("Health.Get: %v", err)
	}
	if health.Status == "" {
		t.Error("Status is empty")
	}
	if health.Timestamp == "" {
		t.Error("Timestamp is empty")
	}
	if len(health.Upstreams) == 0 {
		t.Error("Upstreams is empty")
	}
	t.Logf("status=%s upstreams=%v", health.Status, health.Upstreams)
	if !health.OK() {
		t.Skipf("API reports %q; skipping routing assertions", health.Status)
	}
}

// An absent key must be rejected by the API, confirming the client sends the
// header in the form the API expects.
func TestIntegrationUnauthorized(t *testing.T) {
	client := NewClient("definitely-not-a-valid-key")
	if base := os.Getenv("JUSTROUTING_BASE_URL"); base != "" {
		client = NewClient("definitely-not-a-valid-key", WithBaseURL(base))
	}

	_, err := client.Routes.Get(integrationContext(t), &RouteRequest{
		Origin:      Point{103.8198, 1.3521},
		Destination: Point{103.9915, 1.3644},
	})
	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("err = %v, want ErrUnauthorized", err)
	}
}

func TestIntegrationRoute(t *testing.T) {
	requireAPIKey(t)
	client := integrationClient(t)

	// Marina Bay to Changi Airport, both in Singapore.
	route, err := client.Routes.Get(integrationContext(t), &RouteRequest{
		Origin:      Point{103.8198, 1.3521},
		Destination: Point{103.9915, 1.3644},
		Overview:    "full",
	})
	if err != nil {
		t.Fatalf("Routes.Get: %v", err)
	}

	if route.Distance <= 0 {
		t.Errorf("Distance = %v, want positive", route.Distance)
	}
	if route.Duration <= 0 {
		t.Errorf("Duration = %v, want positive", route.Duration)
	}
	if route.Geometry.IsZero() {
		t.Error("Geometry is empty despite overview=full")
	} else if _, err := route.Geometry.Polyline(); err != nil {
		t.Errorf("Polyline: %v", err)
	}
	t.Logf("%.2f km in %.0f min", route.Distance/1000, route.Duration/60)
}

// The API routes each request to a per-country engine, so a request spanning
// two countries is rejected.
func TestIntegrationCrossCountryRejected(t *testing.T) {
	requireAPIKey(t)
	client := integrationClient(t)

	// Singapore to Kuala Lumpur.
	_, err := client.Routes.Get(integrationContext(t), &RouteRequest{
		Origin:      Point{103.8198, 1.3521},
		Destination: Point{101.6869, 3.1390},
	})
	if !errors.Is(err, ErrCrossCountry) {
		t.Errorf("err = %v, want ErrCrossCountry", err)
	}
}

func TestIntegrationMatrix(t *testing.T) {
	requireAPIKey(t)
	client := integrationClient(t)

	m, err := client.Matrix.Get(integrationContext(t), &MatrixRequest{
		Coordinates: []Point{
			{103.8198, 1.3521},
			{103.8514, 1.2897},
			{103.9915, 1.3644},
		},
	})
	if err != nil {
		t.Fatalf("Matrix.Get: %v", err)
	}

	if len(m.Durations) != 3 {
		t.Fatalf("len(Durations) = %d, want 3", len(m.Durations))
	}
	if got, ok := m.Duration(0, 0); !ok || got != 0 {
		t.Errorf("Duration(0,0) = %v, %v; want 0, true", got, ok)
	}
	if got, ok := m.Duration(0, 1); !ok || got <= 0 {
		t.Errorf("Duration(0,1) = %v, %v; want a positive duration", got, ok)
	}
}

func TestIntegrationOptimization(t *testing.T) {
	requireAPIKey(t)
	client := integrationClient(t)

	depot := Point{103.8198, 1.3521}
	solution, err := client.Optimization.Solve(integrationContext(t), &OptimizationRequest{
		Vehicles: []Vehicle{{ID: 1, Start: depot, End: depot, Capacity: []int{4}}},
		Jobs: []Job{
			{ID: 1, Location: Point{103.8514, 1.2897}, Delivery: []int{1}, Service: 300},
			{ID: 2, Location: Point{103.9915, 1.3644}, Delivery: []int{1}, Service: 300},
		},
	})
	if err != nil {
		t.Fatalf("Optimization.Solve: %v", err)
	}

	if solution.Code != 0 {
		t.Errorf("Code = %d, want 0", solution.Code)
	}
	if len(solution.Routes) == 0 {
		t.Fatal("no routes returned")
	}
	if len(solution.Routes[0].Steps) == 0 {
		t.Error("route has no steps")
	}
	t.Logf("cost=%d routes=%d unassigned=%d",
		solution.Summary.Cost, len(solution.Routes), len(solution.Unassigned))
}
