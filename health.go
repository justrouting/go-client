package justrouting

import (
	"context"
	"net/http"
)

// HealthService reports API availability.
type HealthService struct {
	client *Client
}

// Health is the API's self-reported status.
type Health struct {
	// Status is "ok" when every upstream is reachable, otherwise
	// "degraded".
	Status string `json:"status"`
	// Timestamp is when the check ran, in RFC 3339 format.
	Timestamp string `json:"timestamp"`
	// Upstreams maps each routing engine to its reachability.
	Upstreams map[string]bool `json:"upstreams"`
}

// OK reports whether every upstream is healthy.
func (h *Health) OK() bool { return h.Status == "ok" }

// Get reports the current API status. It is the only call that works without
// an API key, which makes it useful as a connectivity check.
func (s *HealthService) Get(ctx context.Context) (*Health, error) {
	out := new(Health)
	if err := s.client.do(ctx, &request{
		method: http.MethodGet,
		path:   "/health",
	}, out); err != nil {
		return nil, err
	}
	return out, nil
}
