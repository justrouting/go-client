package justrouting

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// MapMatchingService snaps a noisy GPS trace onto the road network and
// assembles the driven route.
type MapMatchingService struct {
	client *Client
}

// MapMatchingRequest describes a map-matching query. Coordinates is required
// and must hold at least two points in chronological order.
//
// The number of coordinates is capped by the account plan; exceeding it
// returns an error matching [ErrPlanLimitExceeded].
type MapMatchingRequest struct {
	// Coordinates is the trace to match, in chronological order.
	Coordinates []Point

	// Timestamps are UNIX seconds for each coordinate, in monotonically
	// increasing order. They are required: the API specification and
	// upstream OSRM both expect them, even though the hosted gateway
	// currently accepts requests without them (with degraded matching).
	Timestamps []int64

	// Radiuses are the maximum distance in metres each coordinate may
	// snap, one value per coordinate. The engine's default snap distance
	// is only a few metres, so GPS points further from the road need this
	// set.
	Radiuses []float64

	// Gaps controls how a trace with gaps is matched: "split" (the
	// default) or "ignore".
	Gaps string

	// Tidy drops tracepoints that cannot be matched from the response.
	Tidy bool

	// Waypoints selects which coordinates to use as waypoints, by index
	// into Coordinates. Empty means all of them.
	Waypoints []int

	// Snapping controls edge snapping: "default" or "any".
	Snapping string

	// Profile selects the routing profile. Defaults to [DefaultProfile].
	Profile string

	// Steps requests turn-by-turn instructions on each leg.
	Steps bool

	// Annotations requests per-segment metadata. Valid values include
	// "duration", "distance", "speed" and "nodes".
	Annotations []string

	// Geometries selects the geometry encoding: "polyline" (the default),
	// "polyline6" or "geojson". See [Geometry].
	Geometries string

	// Overview controls geometry detail: "simplified" (the default),
	// "full" or "false" to omit it.
	Overview string

	// Exclude lists road classes to avoid, such as "motorway" or "ferry".
	// Supported values depend on the profile.
	Exclude []string

	// Bearings restricts the direction of travel at each coordinate, one
	// entry per coordinate in visiting order: "{bearing}" or
	// "{bearing},{range}" in degrees, for example "0,20" for north within
	// ±20°. Coordinates without a restriction use the default when their
	// entry is missing at the end of the list.
	Bearings []string

	// Hints are opaque tokens from a previous response's tracepoints that
	// speed up snapping, one entry per coordinate in visiting order.
	Hints []string

	// Approaches restricts the road direction at each waypoint, one entry
	// per waypoint in visiting order: "curb", "opposite", or
	// "unrestricted" (the engine default). An empty entry also selects
	// the default.
	Approaches []string

	// GenerateHints requests a "hint" on every response tracepoint for
	// reuse in later requests. The engine does this by default; set to
	// false to omit hints.
	GenerateHints *bool

	// SkipWaypoints omits the response waypoints. They still take part in
	// the matching calculation.
	SkipWaypoints bool
}

// MapMatchingResponse is the full result of a map-matching query.
type MapMatchingResponse struct {
	// Code is the engine status, "Ok" on success.
	Code string `json:"code"`
	// Message explains a non-Ok Code.
	Message string `json:"message,omitempty"`
	// Tracepoints are the input coordinates snapped to the road network,
	// aligned with req.Coordinates. An entry is nil when that coordinate
	// could not be matched.
	Tracepoints []*Tracepoint `json:"tracepoints"`
	// Matchings are the routes assembled from the trace, best first.
	Matchings []*Match `json:"matchings"`
}

func (r *MapMatchingResponse) status() *Error { return osrmStatus(r.Code, r.Message) }

// Match is one route assembled from a trace. It embeds [Route], adding the
// engine's confidence in the match.
type Match struct {
	Route
	// Confidence is the engine's trust in the match, from 0 to 1.
	Confidence float64 `json:"confidence"`
}

// Tracepoint is one input coordinate snapped to the road network.
type Tracepoint struct {
	// AlternativesCount is how many other plausible matches exist for this
	// tracepoint.
	AlternativesCount int `json:"alternatives_count,omitempty"`
	// WaypointIndex is the index of this point within the matched route's
	// waypoints.
	WaypointIndex int `json:"waypoint_index,omitempty"`
	// MatchingsIndex identifies which entry of Matchings this tracepoint
	// belongs to.
	MatchingsIndex int `json:"matchings_index,omitempty"`
	// Distance is the metres between the input coordinate and Location.
	Distance float64 `json:"distance"`
	// Name is the street the coordinate snapped to, if known.
	Name string `json:"name"`
	// Location is the snapped position.
	Location Point `json:"location"`
	// Hint is an opaque token that can speed up subsequent requests.
	Hint string `json:"hint,omitempty"`
}

// Get returns the best matching for req.
//
//	match, err := client.MapMatching.Get(ctx, &justrouting.MapMatchingRequest{
//		Coordinates: trace, // GPS points, in order
//	})
//	fmt.Printf("%.0f%% confidence, %.1f km\n", match.Confidence*100, match.Distance/1000)
//
// It returns an error with OSRMCode "NoMatch" when no coordinate can be
// matched. Use [MapMatchingService.GetAll] for the snapped tracepoints.
func (s *MapMatchingService) Get(ctx context.Context, req *MapMatchingRequest) (*Match, error) {
	resp, err := s.GetAll(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(resp.Matchings) == 0 || resp.Matchings[0] == nil {
		return nil, &Error{
			StatusCode: http.StatusOK,
			OSRMCode:   "NoMatch",
			Message:    "no matching found for the given coordinates",
		}
	}
	return resp.Matchings[0], nil
}

// GetAll returns every matching the engine produced for req, along with the
// snapped tracepoints.
func (s *MapMatchingService) GetAll(ctx context.Context, req *MapMatchingRequest) (*MapMatchingResponse, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	coords, err := encodePoints(req.Coordinates)
	if err != nil {
		return nil, err
	}

	query, err := req.query()
	if err != nil {
		return nil, err
	}

	out := new(MapMatchingResponse)
	err = s.client.do(ctx, &request{
		method:    http.MethodGet,
		path:      "/match/v1/" + profileOrDefault(req.Profile) + "/" + coords,
		query:     query,
		needsAuth: true,
	}, out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *MapMatchingRequest) validate() error {
	if r == nil {
		return fmt.Errorf("%w: request must not be nil", ErrInvalidRequest)
	}
	if len(r.Coordinates) < 2 {
		return fmt.Errorf("%w: Coordinates needs at least 2 points, got %d", ErrInvalidRequest, len(r.Coordinates))
	}
	if len(r.Timestamps) == 0 {
		return fmt.Errorf("%w: Timestamps is required for map matching", ErrInvalidRequest)
	}
	if len(r.Timestamps) != len(r.Coordinates) {
		return fmt.Errorf("%w: Timestamps has %d values for %d coordinates", ErrInvalidRequest, len(r.Timestamps), len(r.Coordinates))
	}
	// The engine accepts exactly one radius per coordinate; a single
	// shared value is rejected, so catch it locally.
	if n := len(r.Radiuses); n > 0 && n != len(r.Coordinates) {
		return fmt.Errorf("%w: Radiuses needs one value per coordinate, got %d for %d coordinates", ErrInvalidRequest, n, len(r.Coordinates))
	}
	for i, radius := range r.Radiuses {
		if radius < 0 {
			return fmt.Errorf("%w: Radiuses[%d] must not be negative, got %v", ErrInvalidRequest, i, radius)
		}
	}
	return nil
}

func (r *MapMatchingRequest) query() (url.Values, error) {
	q := url.Values{}

	if len(r.Timestamps) > 0 {
		parts := make([]string, len(r.Timestamps))
		for i, ts := range r.Timestamps {
			parts[i] = strconv.FormatInt(ts, 10)
		}
		q.Set("timestamps", strings.Join(parts, ";"))
	}
	if len(r.Radiuses) > 0 {
		parts := make([]string, len(r.Radiuses))
		for i, radius := range r.Radiuses {
			parts[i] = formatCoord(radius)
		}
		q.Set("radiuses", strings.Join(parts, ";"))
	}
	if r.Gaps != "" {
		q.Set("gaps", r.Gaps)
	}
	if r.Tidy {
		q.Set("tidy", "true")
	}
	if len(r.Waypoints) > 0 {
		waypoints, err := encodeIndices("Waypoints", r.Waypoints, len(r.Coordinates))
		if err != nil {
			return nil, err
		}
		q.Set("waypoints", waypoints)
	}
	if r.Snapping != "" {
		q.Set("snapping", r.Snapping)
	}
	if r.Steps {
		q.Set("steps", "true")
	}
	if len(r.Annotations) > 0 {
		q.Set("annotations", strings.Join(r.Annotations, ","))
	}
	if r.Geometries != "" {
		q.Set("geometries", r.Geometries)
	}
	if r.Overview != "" {
		q.Set("overview", r.Overview)
	}
	if len(r.Exclude) > 0 {
		q.Set("exclude", strings.Join(r.Exclude, ","))
	}
	if len(r.Bearings) > 0 {
		q.Set("bearings", strings.Join(r.Bearings, ";"))
	}
	if len(r.Hints) > 0 {
		q.Set("hints", strings.Join(r.Hints, ";"))
	}
	if len(r.Approaches) > 0 {
		q.Set("approaches", strings.Join(r.Approaches, ";"))
	}
	if r.GenerateHints != nil {
		q.Set("generate_hints", strconv.FormatBool(*r.GenerateHints))
	}
	if r.SkipWaypoints {
		q.Set("skip_waypoints", "true")
	}
	return q, nil
}
