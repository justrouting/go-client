package justrouting

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// matchFixture mirrors a real matching response: a null tracepoint for a
// coordinate that could not be matched, and one matching with confidence.
const matchFixture = `{
  "code": "Ok",
  "tracepoints": [
    {"alternatives_count":2,"waypoint_index":0,"matchings_index":0,"name":"Marina Boulevard","hint":"aaa","distance":4.216,"location":[103.81982,1.35211]},
    null,
    {"alternatives_count":0,"waypoint_index":1,"matchings_index":0,"name":"Changi Coast Road","hint":"bbb","distance":8.104,"location":[103.99151,1.36442]}
  ],
  "matchings": [
    {
      "confidence": 0.95,
      "geometry": "aa_ceeEnAqB",
      "legs": [
        {"steps":[],"summary":"East Coast Parkway","weight":1583.4,"duration":1583.4,"distance":24512.7}
      ],
      "weight_name": "routability",
      "weight": 1583.4,
      "duration": 1583.4,
      "distance": 24512.7
    }
  ]
}`

func TestMapMatchingGetDecodesResponse(t *testing.T) {
	c := newTestClient(t, jsonHandler(http.StatusOK, matchFixture))

	match, err := c.MapMatching.Get(context.Background(), simpleMatch())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if match.Confidence != 0.95 {
		t.Errorf("Confidence = %v, want 0.95", match.Confidence)
	}
	// Match embeds Route, so its fields are promoted.
	if match.Distance != 24512.7 {
		t.Errorf("Distance = %v, want 24512.7", match.Distance)
	}
	if match.Duration != 1583.4 {
		t.Errorf("Duration = %v, want 1583.4", match.Duration)
	}
	if len(match.Legs) != 1 || match.Legs[0].Summary != "East Coast Parkway" {
		t.Errorf("Legs = %v, want one East Coast Parkway leg", match.Legs)
	}
}

// GetAll additionally exposes the snapped tracepoints, including nil entries
// for coordinates the engine could not match.
func TestMapMatchingGetAllReturnsTracepoints(t *testing.T) {
	c := newTestClient(t, jsonHandler(http.StatusOK, matchFixture))

	resp, err := c.MapMatching.GetAll(context.Background(), simpleMatch())
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}

	if len(resp.Matchings) != 1 {
		t.Fatalf("len(Matchings) = %d, want 1", len(resp.Matchings))
	}
	if len(resp.Tracepoints) != 3 {
		t.Fatalf("len(Tracepoints) = %d, want 3", len(resp.Tracepoints))
	}
	if resp.Tracepoints[1] != nil {
		t.Error("Tracepoints[1] should be nil for an unmatched coordinate")
	}
	if tp := resp.Tracepoints[0]; tp.AlternativesCount != 2 || tp.WaypointIndex != 0 {
		t.Errorf("Tracepoints[0] = %+v, want alternatives 2 and waypoint 0", tp)
	}
	if tp := resp.Tracepoints[2]; tp.MatchingsIndex != 0 || tp.Name != "Changi Coast Road" {
		t.Errorf("Tracepoints[2] = %+v, want matching 0 on Changi Coast Road", tp)
	}
}

// An empty matchings array is a no-match condition, not a nil-pointer panic.
func TestMapMatchingGetEmptyMatchings(t *testing.T) {
	c := newTestClient(t, jsonHandler(http.StatusOK, `{"code":"Ok","tracepoints":[],"matchings":[]}`))

	_, err := c.MapMatching.Get(context.Background(), simpleMatch())
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.OSRMCode != "NoMatch" {
		t.Errorf("err = %v, want OSRMCode NoMatch", err)
	}
}

func TestMapMatchingRequestPathEncoding(t *testing.T) {
	tests := []struct {
		name     string
		req      *MapMatchingRequest
		wantPath string
	}{
		{
			name:     "trace in order",
			req:      simpleMatch(),
			wantPath: "/match/v1/driving/103.8198,1.3521;103.8514,1.2897;103.9915,1.3644",
		},
		{
			name: "custom profile",
			req: &MapMatchingRequest{
				Coordinates: []Point{{103.8198, 1.3521}, {103.9915, 1.3644}},
				Profile:     "motorcycle",
			},
			wantPath: "/match/v1/motorcycle/103.8198,1.3521;103.9915,1.3644",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				_, _ = w.Write([]byte(matchFixture))
			})

			if _, err := c.MapMatching.Get(context.Background(), tc.req); err != nil {
				t.Fatalf("Get: %v", err)
			}
			if gotPath != tc.wantPath {
				t.Errorf("path  = %q\nwant  = %q", gotPath, tc.wantPath)
			}
		})
	}
}

func TestMapMatchingQueryEncoding(t *testing.T) {
	tests := []struct {
		name  string
		req   *MapMatchingRequest
		want  map[string]string
		empty []string
	}{
		{
			name:  "defaults send no options",
			req:   simpleMatch(),
			empty: []string{"timestamps", "radiuses", "gaps", "tidy", "waypoints", "snapping", "steps", "geometries", "overview", "annotations"},
		},
		{
			name: "all options",
			req: &MapMatchingRequest{
				Coordinates: []Point{{103.8198, 1.3521}, {103.8514, 1.2897}, {103.9915, 1.3644}},
				Timestamps:  []int64{1720000000, 1720000060, 1720000120},
				Radiuses:    []float64{10, 15, 25},
				Gaps:        "ignore",
				Tidy:        true,
				Waypoints:   []int{0, 2},
				Snapping:    "any",
				Steps:       true,
				Annotations: []string{"duration", "distance"},
				Geometries:  "geojson",
				Overview:    "full",
				Exclude:     []string{"motorway", "ferry"},
			},
			want: map[string]string{
				"timestamps":  "1720000000;1720000060;1720000120",
				"radiuses":    "10;15;25",
				"gaps":        "ignore",
				"tidy":        "true",
				"waypoints":   "0;2",
				"snapping":    "any",
				"steps":       "true",
				"annotations": "duration,distance",
				"geometries":  "geojson",
				"overview":    "full",
				"exclude":     "motorway,ferry",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var query map[string][]string
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				query = r.URL.Query()
				_, _ = w.Write([]byte(matchFixture))
			})

			if _, err := c.MapMatching.Get(context.Background(), tc.req); err != nil {
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

func TestMapMatchingValidation(t *testing.T) {
	tests := []struct {
		name string
		req  *MapMatchingRequest
	}{
		{name: "nil request", req: nil},
		{name: "one coordinate", req: &MapMatchingRequest{Coordinates: []Point{{103.8, 1.3}}}},
		{
			name: "timestamp count mismatch",
			req: &MapMatchingRequest{
				Coordinates: []Point{{103.8, 1.3}, {103.9, 1.3}, {104.0, 1.3}},
				Timestamps:  []int64{1720000000},
			},
		},
		{
			name: "radius count mismatch",
			req: &MapMatchingRequest{
				Coordinates: []Point{{103.8, 1.3}, {103.9, 1.3}, {104.0, 1.3}},
				Radiuses:    []float64{10, 15},
			},
		},
		{
			// The engine rejects a single shared radius, so it is caught
			// locally.
			name: "single radius for multiple coordinates",
			req: &MapMatchingRequest{
				Coordinates: []Point{{103.8, 1.3}, {103.9, 1.3}, {104.0, 1.3}},
				Radiuses:    []float64{10},
			},
		},
		{
			name: "negative radius",
			req: &MapMatchingRequest{
				Coordinates: []Point{{103.8, 1.3}, {103.9, 1.3}},
				Radiuses:    []float64{-5},
			},
		},
		{
			name: "waypoint index out of range",
			req: &MapMatchingRequest{
				Coordinates: []Point{{103.8, 1.3}, {103.9, 1.3}},
				Waypoints:   []int{0, 5},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				t.Error("no request should reach the server")
			})
			if _, err := c.MapMatching.Get(context.Background(), tc.req); !errors.Is(err, ErrInvalidRequest) {
				t.Errorf("err = %v, want ErrInvalidRequest", err)
			}
		})
	}
}

// Malformed coordinates are caught locally so a request is not wasted.
func TestMapMatchingRejectsBadCoordinatesLocally(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request should reach the server")
	})

	// The classic [lat, lon] mix-up: 103.8198 is not a latitude.
	_, err := c.MapMatching.Get(context.Background(), &MapMatchingRequest{
		Coordinates: []Point{{1.3521, 103.8198}, {103.9915, 1.3644}},
	})
	if !errors.Is(err, ErrInvalidCoordinates) {
		t.Errorf("err = %v, want ErrInvalidCoordinates", err)
	}
}

func simpleMatch() *MapMatchingRequest {
	return &MapMatchingRequest{
		Coordinates: []Point{
			{103.8198, 1.3521},
			{103.8514, 1.2897},
			{103.9915, 1.3644},
		},
	}
}
