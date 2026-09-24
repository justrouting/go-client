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
	return q
}
