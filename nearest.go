package justrouting

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// NearestService finds the road segments closest to a coordinate.
type NearestService struct {
	client *Client
}

// NearestRequest describes a nearest-segment query. Coordinate is required.
type NearestRequest struct {
	// Coordinate is the point to snap, as [longitude, latitude].
	Coordinate Point

	// Number is how many nearby segments to return, ordered by distance.
	// Defaults to 1.
	Number int

	// Profile selects the routing profile. Defaults to [DefaultProfile].
	Profile string

	// Exclude lists road classes to avoid, such as "motorway" or "ferry".
	// Supported values depend on the profile.
	Exclude []string
}

// NearestResponse is the result of a nearest-segment query.
type NearestResponse struct {
	// Code is the engine status, "Ok" on success.
	Code string `json:"code"`
	// Message explains a non-Ok Code.
	Message string `json:"message,omitempty"`
	// Waypoints are the segments nearest to the coordinate, best first.
	Waypoints []*Waypoint `json:"waypoints"`
}

func (r *NearestResponse) status() *Error { return osrmStatus(r.Code, r.Message) }

// Get returns the road segment closest to req.Coordinate.
//
//	wp, err := client.Nearest.Get(ctx, &justrouting.NearestRequest{
//		Coordinate: []float64{103.8198, 1.3521},
//	})
//	fmt.Printf("snapped to %s, %.0f m away\n", wp.Name, wp.Distance)
//
// Use [NearestService.GetAll] for the second-, third-, ... nearest segments.
func (s *NearestService) Get(ctx context.Context, req *NearestRequest) (*Waypoint, error) {
	resp, err := s.GetAll(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(resp.Waypoints) == 0 || resp.Waypoints[0] == nil {
		return nil, &Error{
			StatusCode: http.StatusOK,
			OSRMCode:   "NoSegment",
			Message:    "no segment found near the given coordinate",
		}
	}
	return resp.Waypoints[0], nil
}

// GetAll returns up to req.Number road segments near req.Coordinate, best
// first.
func (s *NearestService) GetAll(ctx context.Context, req *NearestRequest) (*NearestResponse, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	coords, err := encodePoints([]Point{req.Coordinate})
	if err != nil {
		return nil, err
	}

	out := new(NearestResponse)
	err = s.client.do(ctx, &request{
		method:    http.MethodGet,
		path:      "/nearest/v1/" + profileOrDefault(req.Profile) + "/" + coords,
		query:     req.query(),
		needsAuth: true,
	}, out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *NearestRequest) validate() error {
	if r == nil {
		return fmt.Errorf("%w: request must not be nil", ErrInvalidRequest)
	}
	if len(r.Coordinate) == 0 {
		return fmt.Errorf("%w: Coordinate is required", ErrInvalidRequest)
	}
	if r.Number < 0 {
		return fmt.Errorf("%w: Number must not be negative, got %d", ErrInvalidRequest, r.Number)
	}
	return nil
}

func (r *NearestRequest) query() url.Values {
	q := url.Values{}
	if r.Number > 0 {
		q.Set("number", strconv.Itoa(r.Number))
	}
	if len(r.Exclude) > 0 {
		q.Set("exclude", strings.Join(r.Exclude, ","))
	}
	return q
}
