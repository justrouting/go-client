package justrouting

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// nearestFixture mirrors a real nearest response with two candidates,
// including the OSM node IDs of the closest segment.
const nearestFixture = `{
  "code": "Ok",
  "waypoints": [
    {"hint":"aaa","distance":4.216,"name":"Marina Boulevard","location":[103.81982,1.35211],"nodes":[1234,5678]},
    {"hint":"bbb","distance":21.7,"name":"Bayfront Avenue","location":[103.82001,1.35237],"nodes":[4321]}
  ]
}`

func TestNearestGetDecodesResponse(t *testing.T) {
	c := newTestClient(t, jsonHandler(http.StatusOK, nearestFixture))

	wp, err := c.Nearest.Get(context.Background(), simpleNearest())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if wp.Name != "Marina Boulevard" {
		t.Errorf("Name = %q, want %q", wp.Name, "Marina Boulevard")
	}
	if wp.Distance != 4.216 {
		t.Errorf("Distance = %v, want 4.216", wp.Distance)
	}
	if lon := wp.Location.Lon(); lon != 103.81982 {
		t.Errorf("Location.Lon() = %v, want 103.81982", lon)
	}
	if len(wp.Nodes) != 2 || wp.Nodes[0] != 1234 || wp.Nodes[1] != 5678 {
		t.Errorf("Nodes = %v, want [1234 5678]", wp.Nodes)
	}
}

// Get returns only the closest segment; GetAll exposes every candidate.
func TestNearestGetAllReturnsCandidates(t *testing.T) {
	c := newTestClient(t, jsonHandler(http.StatusOK, nearestFixture))

	resp, err := c.Nearest.GetAll(context.Background(), simpleNearest())
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}

	if len(resp.Waypoints) != 2 {
		t.Fatalf("len(Waypoints) = %d, want 2", len(resp.Waypoints))
	}
	if resp.Waypoints[0].Distance >= resp.Waypoints[1].Distance {
		t.Error("waypoints should be ordered best first")
	}
}

// An empty waypoints array on Ok is a no-segment condition, not a nil-pointer
// panic.
func TestNearestGetEmptyWaypoints(t *testing.T) {
	c := newTestClient(t, jsonHandler(http.StatusOK, `{"code":"Ok","waypoints":[]}`))

	_, err := c.Nearest.Get(context.Background(), simpleNearest())
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.OSRMCode != "NoSegment" {
		t.Errorf("err = %v, want OSRMCode NoSegment", err)
	}
}

func TestNearestRequestPathEncoding(t *testing.T) {
	tests := []struct {
		name     string
		req      *NearestRequest
		wantPath string
	}{
		{
			name:     "single coordinate",
			req:      simpleNearest(),
			wantPath: "/nearest/v1/driving/103.8198,1.3521",
		},
		{
			name: "custom profile",
			req: &NearestRequest{
				Coordinate: Point{103.8198, 1.3521},
				Profile:    "cycling",
			},
			wantPath: "/nearest/v1/cycling/103.8198,1.3521",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				_, _ = w.Write([]byte(nearestFixture))
			})

			if _, err := c.Nearest.Get(context.Background(), tc.req); err != nil {
				t.Fatalf("Get: %v", err)
			}
			if gotPath != tc.wantPath {
				t.Errorf("path  = %q\nwant  = %q", gotPath, tc.wantPath)
			}
		})
	}
}

func TestNearestQueryEncoding(t *testing.T) {
	tests := []struct {
		name  string
		req   *NearestRequest
		want  map[string]string
		empty []string
	}{
		{
			name:  "defaults send no options",
			req:   simpleNearest(),
			empty: []string{"number", "exclude"},
		},
		{
			name: "all options",
			req: &NearestRequest{
				Coordinate: Point{103.8198, 1.3521},
				Number:     3,
				Exclude:    []string{"motorway", "ferry"},
			},
			want: map[string]string{
				"number":  "3",
				"exclude": "motorway,ferry",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var query map[string][]string
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				query = r.URL.Query()
				_, _ = w.Write([]byte(nearestFixture))
			})

			if _, err := c.Nearest.Get(context.Background(), tc.req); err != nil {
				t.Fatalf("Get: %v", err)
			}
			for key, want := range tc.want {
				if got := query[key]; len(got) != 1 || got[0] != want {
					t.Errorf("query %q = %v, want [%q]", key, got, want)
				}
			}
			for _, key := range tc.empty {
				if got, ok := query[key]; ok {
					t.Errorf("query %q = %v, want it omitted", key, got)
				}
			}
		})
	}
}

func TestNearestValidation(t *testing.T) {
	tests := []struct {
		name string
		req  *NearestRequest
	}{
		{name: "nil request", req: nil},
		{name: "missing coordinate", req: &NearestRequest{Number: 2}},
		{name: "negative number", req: &NearestRequest{Coordinate: Point{103.8, 1.3}, Number: -1}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				t.Error("no request should reach the server")
			})
			if _, err := c.Nearest.Get(context.Background(), tc.req); !errors.Is(err, ErrInvalidRequest) {
				t.Errorf("err = %v, want ErrInvalidRequest", err)
			}
		})
	}
}

// Malformed coordinates are caught locally so a request is not wasted.
func TestNearestRejectsBadCoordinatesLocally(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request should reach the server")
	})

	// The classic [lat, lon] mix-up: 103.8198 is not a latitude.
	_, err := c.Nearest.Get(context.Background(), &NearestRequest{
		Coordinate: Point{1.3521, 103.8198},
	})
	if !errors.Is(err, ErrInvalidCoordinates) {
		t.Errorf("err = %v, want ErrInvalidCoordinates", err)
	}
}

func simpleNearest() *NearestRequest {
	return &NearestRequest{
		Coordinate: []float64{103.8198, 1.3521},
	}
}
