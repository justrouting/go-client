package justrouting

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// MatrixService computes travel duration and distance between many
// coordinates at once.
type MatrixService struct {
	client *Client
}

// MatrixRequest describes a matrix query. Coordinates is required and must
// hold at least two points.
//
// By default every coordinate is used as both a source and a destination,
// producing an NxN matrix. Set Sources and/or Destinations to compute a
// rectangular subset instead, which is considerably cheaper.
//
// The number of coordinates is capped by the account plan; exceeding it
// returns an error matching [ErrPlanLimitExceeded].
type MatrixRequest struct {
	// Coordinates are the points to measure between.
	Coordinates []Point

	// Sources selects which coordinates act as row origins, by index into
	// Coordinates. Empty means all of them.
	Sources []int

	// Destinations selects which coordinates act as column targets, by
	// index into Coordinates. Empty means all of them.
	Destinations []int

	// Annotations selects which matrices to compute: "duration",
	// "distance", or both. Defaults to both.
	Annotations []string

	// FallbackSpeed estimates the duration of unreachable pairs from their
	// as-the-crow-flies distance at this speed, in km/h. The response
	// reports which cells were estimated in [MatrixResponse.FallbackSpeedCells].
	FallbackSpeed *float64

	// FallbackCoordinate selects which coordinates fallback estimates are
	// measured from: "input" (the engine default) or "snapped".
	FallbackCoordinate string

	// ScaleFactor scales every duration by this factor, for example 1.2
	// for +20%.
	ScaleFactor *float64

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

	// Exclude lists road classes to avoid, such as "motorway" or "ferry".
	// Supported values depend on the profile.
	Exclude []string

	// Snapping selects how coordinates snap onto the road network:
	// "default" (the engine default) avoids edges that cannot be used as
	// start or end points; "any" snaps to any edge.
	Snapping string

	// GenerateHints requests a "hint" on every response waypoint for
	// reuse in later requests. The engine does this by default; set to
	// false to omit hints.
	GenerateHints *bool

	// SkipWaypoints omits the response sources and destinations. They
	// still take part in the matrix calculation.
	SkipWaypoints bool

	// Profile selects the routing profile. Defaults to [DefaultProfile].
	Profile string
}

// MatrixResponse holds the computed matrices.
//
// Entries are pointers because the engine reports an unreachable pair as
// null, which must stay distinguishable from a genuine zero. Prefer the
// [MatrixResponse.Duration] and [MatrixResponse.Distance] accessors.
type MatrixResponse struct {
	// Code is the engine status, "Ok" on success.
	Code string `json:"code"`
	// Message explains a non-Ok Code.
	Message string `json:"message,omitempty"`

	// Durations holds travel times in seconds, indexed [source][destination].
	Durations [][]*float64 `json:"durations,omitempty"`
	// Distances holds travel distances in metres, indexed [source][destination].
	Distances [][]*float64 `json:"distances,omitempty"`

	// Sources are the source coordinates snapped to the road network.
	Sources []*Waypoint `json:"sources"`
	// Destinations are the destination coordinates snapped to the road network.
	Destinations []*Waypoint `json:"destinations"`

	// FallbackSpeedCells lists the [source, destination] pairs whose
	// duration was estimated via the fallback_speed option.
	FallbackSpeedCells [][]int `json:"fallback_speed_cells,omitempty"`
}

func (r *MatrixResponse) status() *Error { return osrmStatus(r.Code, r.Message) }

// Duration returns the travel time in seconds from source i to destination j.
// The boolean is false when the indices are out of range or the pair is
// unreachable.
func (r *MatrixResponse) Duration(i, j int) (float64, bool) {
	return matrixAt(r.Durations, i, j)
}

// Distance returns the travel distance in metres from source i to
// destination j. The boolean is false when the indices are out of range or the
// pair is unreachable.
func (r *MatrixResponse) Distance(i, j int) (float64, bool) {
	return matrixAt(r.Distances, i, j)
}

func matrixAt(m [][]*float64, i, j int) (float64, bool) {
	if i < 0 || i >= len(m) {
		return 0, false
	}
	row := m[i]
	if j < 0 || j >= len(row) || row[j] == nil {
		return 0, false
	}
	return *row[j], true
}

// Get computes the duration and distance matrices for req.
//
//	m, err := client.Matrix.Get(ctx, &justrouting.MatrixRequest{
//		Coordinates: []justrouting.Point{depot, stopA, stopB},
//	})
//	if secs, ok := m.Duration(0, 1); ok {
//		fmt.Printf("depot -> stopA: %.0f min", secs/60)
//	}
func (s *MatrixService) Get(ctx context.Context, req *MatrixRequest) (*MatrixResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: request must not be nil", ErrInvalidRequest)
	}
	if len(req.Coordinates) < 2 {
		return nil, fmt.Errorf("%w: Coordinates needs at least 2 points, got %d", ErrInvalidRequest, len(req.Coordinates))
	}
	coords, err := encodePoints(req.Coordinates)
	if err != nil {
		return nil, err
	}

	query, err := req.query()
	if err != nil {
		return nil, err
	}

	out := new(MatrixResponse)
	err = s.client.do(ctx, &request{
		method:    http.MethodGet,
		path:      "/table/v1/" + profileOrDefault(req.Profile) + "/" + coords,
		query:     query,
		needsAuth: true,
	}, out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *MatrixRequest) query() (url.Values, error) {
	q := url.Values{}

	annotations := r.Annotations
	if len(annotations) == 0 {
		annotations = []string{"duration", "distance"}
	}
	q.Set("annotations", strings.Join(annotations, ","))

	sources, err := encodeIndices("Sources", r.Sources, len(r.Coordinates))
	if err != nil {
		return nil, err
	}
	if sources != "" {
		q.Set("sources", sources)
	}

	destinations, err := encodeIndices("Destinations", r.Destinations, len(r.Coordinates))
	if err != nil {
		return nil, err
	}
	if destinations != "" {
		q.Set("destinations", destinations)
	}

	if r.FallbackSpeed != nil {
		q.Set("fallback_speed", strconv.FormatFloat(*r.FallbackSpeed, 'f', -1, 64))
	}
	if r.FallbackCoordinate != "" {
		q.Set("fallback_coordinate", r.FallbackCoordinate)
	}
	if r.ScaleFactor != nil {
		q.Set("scale_factor", strconv.FormatFloat(*r.ScaleFactor, 'f', -1, 64))
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
	if len(r.Exclude) > 0 {
		q.Set("exclude", strings.Join(r.Exclude, ","))
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

	return q, nil
}

// encodeIndices renders coordinate indices as a semicolon-separated list,
// rejecting anything outside the coordinate slice before a request is spent.
func encodeIndices(field string, indices []int, total int) (string, error) {
	if len(indices) == 0 {
		return "", nil
	}
	parts := make([]string, len(indices))
	for i, idx := range indices {
		if idx < 0 || idx >= total {
			return "", fmt.Errorf("%w: %s[%d] = %d is out of range for %d coordinates", ErrInvalidRequest, field, i, idx, total)
		}
		parts[i] = strconv.Itoa(idx)
	}
	return strings.Join(parts, ";"), nil
}
