package justrouting

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// TripService finds the fastest order to visit a set of coordinates.
type TripService struct {
	client *Client
}

// TripRequest describes a trip query. Coordinates is required and must hold
// at least two points.
//
// The engine solves the travelling-salesman problem with a greedy heuristic:
// the returned trip visits every coordinate once, in whatever order is
// fastest. The number of coordinates is capped by the account plan; exceeding
// it returns an error matching [ErrPlanLimitExceeded].
type TripRequest struct {
	// Coordinates are the points to visit, in any order.
	Coordinates []Point

	// Roundtrip makes the trip end where it started. Nil leaves the
	// engine default, which is true.
	Roundtrip *bool

	// Source pins where the trip starts: "any" (the default) or "first".
	Source string

	// Destination pins where the trip ends: "any" (the default) or
	// "last".
	Destination string

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

	// Radiuses sets the snapping radius in meters per coordinate, one
	// entry per coordinate in visiting order; "unlimited" disables
	// snapping for that coordinate.
	Radiuses []string

	// Hints are opaque tokens from a previous response's waypoints that
	// speed up snapping, one entry per coordinate in visiting order.
	Hints []string

	// Approaches restricts the road direction at each waypoint, one entry
	// per waypoint in visiting order: "curb", "opposite", or
	// "unrestricted" (the engine default). An empty entry also selects
	// the default.
	Approaches []string

	// Snapping selects how coordinates snap onto the road network:
	// "default" (the engine default) avoids edges that cannot be used as
	// start or end points; "any" snaps to any edge.
	Snapping string

	// GenerateHints requests a "hint" on every response waypoint for
	// reuse in later requests. The engine does this by default; set to
	// false to omit hints.
	GenerateHints *bool

	// SkipWaypoints omits the response waypoints. They still take part in
	// the trip calculation.
	SkipWaypoints bool
}

// TripResponse is the full result of a trip query.
type TripResponse struct {
	// Code is the engine status, "Ok" on success.
	Code string `json:"code"`
	// Message explains a non-Ok Code.
	Message string `json:"message,omitempty"`
	// Trips are the candidate round trips, best first.
	Trips []*Route `json:"trips"`
	// Waypoints are the input coordinates snapped to the road network, in
	// the order the trip visits them.
	Waypoints []*Waypoint `json:"waypoints"`
}

func (r *TripResponse) status() *Error { return osrmStatus(r.Code, r.Message) }

// Get returns the best trip for req.
//
//	trip, err := client.Trip.Get(ctx, &justrouting.TripRequest{
//		Coordinates: []justrouting.Point{depot, stopA, stopB},
//	})
//	fmt.Printf("fastest order covers %.1f km\n", trip.Distance/1000)
//
// The waypoints in [TripResponse.Waypoints] are ordered as visited. Use
// [TripService.GetAll] to retrieve them.
func (s *TripService) Get(ctx context.Context, req *TripRequest) (*Route, error) {
	resp, err := s.GetAll(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(resp.Trips) == 0 || resp.Trips[0] == nil {
		return nil, &Error{
			StatusCode: http.StatusOK,
			OSRMCode:   "NoTrip",
			Message:    "no trip found for the given coordinates",
		}
	}
	return resp.Trips[0], nil
}

// GetAll returns every trip the engine produced for req, along with the
// snapped waypoints in visiting order.
func (s *TripService) GetAll(ctx context.Context, req *TripRequest) (*TripResponse, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	coords, err := encodePoints(req.Coordinates)
	if err != nil {
		return nil, err
	}

	out := new(TripResponse)
	err = s.client.do(ctx, &request{
		method:    http.MethodGet,
		path:      "/trip/v1/" + profileOrDefault(req.Profile) + "/" + coords,
		query:     req.query(),
		needsAuth: true,
	}, out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *TripRequest) validate() error {
	if r == nil {
		return fmt.Errorf("%w: request must not be nil", ErrInvalidRequest)
	}
	if len(r.Coordinates) < 2 {
		return fmt.Errorf("%w: Coordinates needs at least 2 points, got %d", ErrInvalidRequest, len(r.Coordinates))
	}
	return nil
}

func (r *TripRequest) query() url.Values {
	q := url.Values{}
	if r.Roundtrip != nil {
		q.Set("roundtrip", strconv.FormatBool(*r.Roundtrip))
	}
	if r.Source != "" {
		q.Set("source", r.Source)
	}
	if r.Destination != "" {
		q.Set("destination", r.Destination)
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
	if len(r.Radiuses) > 0 {
		q.Set("radiuses", strings.Join(r.Radiuses, ";"))
	}
	if len(r.Hints) > 0 {
		q.Set("hints", strings.Join(r.Hints, ";"))
	}
	if len(r.Approaches) > 0 {
		q.Set("approaches", strings.Join(r.Approaches, ";"))
	}
	if r.Snapping != "" {
		q.Set("snapping", r.Snapping)
	}
	if r.GenerateHints != nil {
		q.Set("generate_hints", strconv.FormatBool(*r.GenerateHints))
	}
	if r.SkipWaypoints {
		q.Set("skip_waypoints", "true")
	}
	return q
}
