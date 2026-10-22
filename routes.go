package justrouting

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// RoutesService computes the fastest route between coordinates.
type RoutesService struct {
	client *Client
}

// RouteRequest describes a routing query. Origin and Destination are
// required; every other field is optional.
type RouteRequest struct {
	// Origin is where the route starts, as [longitude, latitude].
	Origin Point
	// Destination is where the route ends, as [longitude, latitude].
	Destination Point
	// Waypoints are intermediate stops, visited in the order given.
	Waypoints []Point

	// Profile selects the routing profile. Defaults to [DefaultProfile].
	Profile string

	// Alternatives requests up to this many alternative routes. They are
	// returned by GetAll; Get always yields the best route. The engine may
	// return fewer, or none, if no reasonable alternative exists.
	Alternatives int

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

	// ContinueStraight forces or forbids continuing straight at the first
	// waypoint. Nil leaves the decision to the engine.
	ContinueStraight *bool

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
	// the route calculation.
	SkipWaypoints bool

	// WaypointIndices lists the indices of the input coordinates that the
	// response should treat as waypoints, for example [0, 3] to keep only
	// the first and last. The engine treats every input coordinate as a
	// waypoint by default.
	WaypointIndices []int
}

// RouteResponse is the full result of a routing query.
type RouteResponse struct {
	// Code is the engine status, "Ok" on success.
	Code string `json:"code"`
	// Message explains a non-Ok Code.
	Message string `json:"message,omitempty"`
	// DataVersion is the timestamp of the OSM data the engine used.
	DataVersion string `json:"data_version,omitempty"`
	// Routes are ordered best first.
	Routes []*Route `json:"routes"`
	// Waypoints are the input coordinates snapped to the road network.
	Waypoints []*Waypoint `json:"waypoints"`
}

func (r *RouteResponse) status() *Error { return osrmStatus(r.Code, r.Message) }

// Route is a single path through the road network.
type Route struct {
	// Distance is the route length in metres.
	Distance float64 `json:"distance"`
	// Duration is the estimated travel time in seconds.
	Duration float64 `json:"duration"`
	// Weight is the value the engine minimised, in WeightName units.
	Weight float64 `json:"weight"`
	// WeightName names the optimisation metric, such as "routability".
	WeightName string `json:"weight_name"`
	// Geometry is the route's shape.
	Geometry Geometry `json:"geometry"`
	// Legs holds one entry per consecutive pair of waypoints.
	Legs []*Leg `json:"legs"`
}

// Leg is the portion of a route between two consecutive waypoints.
type Leg struct {
	Distance   float64     `json:"distance"`
	Duration   float64     `json:"duration"`
	Weight     float64     `json:"weight"`
	Summary    string      `json:"summary"`
	Steps      []*Step     `json:"steps,omitempty"`
	Annotation *Annotation `json:"annotation,omitempty"`
}

// Step is a single turn-by-turn instruction, returned when
// [RouteRequest.Steps] is set.
type Step struct {
	Distance      float64         `json:"distance"`
	Duration      float64         `json:"duration"`
	Weight        float64         `json:"weight"`
	Geometry      Geometry        `json:"geometry"`
	Name          string          `json:"name"`
	Ref           string          `json:"ref,omitempty"`
	Mode          string          `json:"mode"`
	Maneuver      Maneuver        `json:"maneuver"`
	Intersections []*Intersection `json:"intersections,omitempty"`
}

// Maneuver describes the action taken at the start of a [Step].
type Maneuver struct {
	Location      Point  `json:"location"`
	BearingBefore int    `json:"bearing_before"`
	BearingAfter  int    `json:"bearing_after"`
	Type          string `json:"type"`
	Modifier      string `json:"modifier,omitempty"`
	Exit          int    `json:"exit,omitempty"`
}

// Intersection is a junction passed during a [Step].
type Intersection struct {
	Location Point  `json:"location"`
	Bearings []int  `json:"bearings"`
	Entry    []bool `json:"entry"`
	In       *int   `json:"in,omitempty"`
	Out      *int   `json:"out,omitempty"`
	Lanes    []Lane `json:"lanes,omitempty"`
	// Classes lists the road classes of the exiting roads, such as "toll"
	// and "ferry". The engine reports it only when turn-by-turn steps are
	// requested (see [RouteRequest.Steps]).
	Classes []string `json:"classes,omitempty"`
}

// Lane describes a turn lane at an [Intersection].
type Lane struct {
	Indications []string `json:"indications"`
	Valid       bool     `json:"valid"`
}

// Annotation holds per-segment metadata, returned when
// [RouteRequest.Annotations] is set.
type Annotation struct {
	Distance    []float64 `json:"distance,omitempty"`
	Duration    []float64 `json:"duration,omitempty"`
	Speed       []float64 `json:"speed,omitempty"`
	Weight      []float64 `json:"weight,omitempty"`
	Nodes       []int64   `json:"nodes,omitempty"`
	Datasources []int     `json:"datasources,omitempty"`
}

// Get returns the best route for req.
//
//	route, err := client.Routes.Get(ctx, &justrouting.RouteRequest{
//		Origin:      []float64{103.8198, 1.3521},
//		Destination: []float64{103.9915, 1.3644},
//	})
//	fmt.Printf("%.1f km", route.Distance/1000)
//
// It returns an error matching [ErrNoRoute] when the coordinates cannot be
// connected. Use [RoutesService.GetAll] for alternatives and snapped
// waypoints.
func (s *RoutesService) Get(ctx context.Context, req *RouteRequest) (*Route, error) {
	resp, err := s.GetAll(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(resp.Routes) == 0 || resp.Routes[0] == nil {
		return nil, &Error{
			StatusCode: http.StatusOK,
			OSRMCode:   "NoRoute",
			Message:    "no route found between the given coordinates",
		}
	}
	return resp.Routes[0], nil
}

// GetAll returns every route the engine produced for req, along with the
// snapped input waypoints.
func (s *RoutesService) GetAll(ctx context.Context, req *RouteRequest) (*RouteResponse, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	coords, err := encodePoints(req.coordinates())
	if err != nil {
		return nil, err
	}

	out := new(RouteResponse)
	err = s.client.do(ctx, &request{
		method:    http.MethodGet,
		path:      "/route/v1/" + profileOrDefault(req.Profile) + "/" + coords,
		query:     req.query(),
		needsAuth: true,
	}, out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *RouteRequest) validate() error {
	if r == nil {
		return fmt.Errorf("%w: request must not be nil", ErrInvalidRequest)
	}
	if len(r.Origin) == 0 {
		return fmt.Errorf("%w: Origin is required", ErrInvalidRequest)
	}
	if len(r.Destination) == 0 {
		return fmt.Errorf("%w: Destination is required", ErrInvalidRequest)
	}
	return nil
}

// coordinates flattens the request into the order the engine expects.
func (r *RouteRequest) coordinates() []Point {
	points := make([]Point, 0, len(r.Waypoints)+2)
	points = append(points, r.Origin)
	points = append(points, r.Waypoints...)
	points = append(points, r.Destination)
	return points
}

func (r *RouteRequest) query() url.Values {
	q := url.Values{}
	if r.Alternatives > 0 {
		q.Set("alternatives", strconv.Itoa(r.Alternatives))
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
	if r.ContinueStraight != nil {
		q.Set("continue_straight", strconv.FormatBool(*r.ContinueStraight))
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
	if len(r.WaypointIndices) > 0 {
		indices := make([]string, len(r.WaypointIndices))
		for i, w := range r.WaypointIndices {
			indices[i] = strconv.Itoa(w)
		}
		q.Set("waypoints", strings.Join(indices, ";"))
	}
	return q
}
