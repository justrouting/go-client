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
