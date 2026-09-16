package justrouting

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// routeFixture mirrors a real routing response, including the snapped
// waypoints and a polyline geometry.
const routeFixture = `{
  "code": "Ok",
  "waypoints": [
    {"hint":"aaa","distance":4.216,"name":"Marina Boulevard","location":[103.81982,1.35211]},
    {"hint":"bbb","distance":8.104,"name":"Changi Coast Road","location":[103.99151,1.36442]}
  ],
  "routes": [
    {
      "geometry": "ka|` + "`" + `@_ceeEnAqB",
      "legs": [
        {"steps":[],"summary":"East Coast Parkway","weight":1583.4,"duration":1583.4,"distance":24512.7}
      ],
      "weight_name": "routability",
      "weight": 1583.4,
      "duration": 1583.4,
      "distance": 24512.7
    },
    {
      "geometry": "ab|` + "`" + `@_ceeEnAqB",
      "legs": [],
      "weight_name": "routability",
      "weight": 1712.9,
      "duration": 1712.9,
      "distance": 26104.2
    }
  ]
}`

func TestRoutesGetDecodesResponse(t *testing.T) {
	c := newTestClient(t, jsonHandler(http.StatusOK, routeFixture))

	route, err := c.Routes.Get(context.Background(), simpleRoute())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if route.Distance != 24512.7 {
		t.Errorf("Distance = %v, want 24512.7", route.Distance)
	}
	if route.Duration != 1583.4 {
		t.Errorf("Duration = %v, want 1583.4", route.Duration)
	}
	if route.WeightName != "routability" {
		t.Errorf("WeightName = %q", route.WeightName)
	}
	if len(route.Legs) != 1 {
		t.Fatalf("len(Legs) = %d, want 1", len(route.Legs))
	}
	if route.Legs[0].Summary != "East Coast Parkway" {
		t.Errorf("Legs[0].Summary = %q", route.Legs[0].Summary)
	}

	// The quickstart divides by 1000 to get kilometres.
	if km := route.Distance / 1000; km < 24.5 || km > 24.6 {
		t.Errorf("Distance/1000 = %v km, want ~24.5", km)
	}
}

// Get returns only the best route; GetAll exposes alternatives and waypoints.
func TestRoutesGetAllReturnsAlternativesAndWaypoints(t *testing.T) {
	c := newTestClient(t, jsonHandler(http.StatusOK, routeFixture))

	resp, err := c.Routes.GetAll(context.Background(), simpleRoute())
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}

	if len(resp.Routes) != 2 {
		t.Fatalf("len(Routes) = %d, want 2", len(resp.Routes))
	}
	if resp.Routes[0].Distance >= resp.Routes[1].Distance {
		t.Error("routes should be ordered best first")
	}
	if len(resp.Waypoints) != 2 {
		t.Fatalf("len(Waypoints) = %d, want 2", len(resp.Waypoints))
	}
	if resp.Waypoints[0].Name != "Marina Boulevard" {
		t.Errorf("Waypoints[0].Name = %q", resp.Waypoints[0].Name)
	}
	if lon := resp.Waypoints[0].Location.Lon(); lon != 103.81982 {
		t.Errorf("Waypoints[0].Location.Lon() = %v", lon)
	}
}

// An empty routes array is a "no route" condition, not a nil-pointer panic.
func TestRoutesGetEmptyRoutes(t *testing.T) {
	c := newTestClient(t, jsonHandler(http.StatusOK, `{"code":"Ok","routes":[],"waypoints":[]}`))

	_, err := c.Routes.Get(context.Background(), simpleRoute())
	if !errors.Is(err, ErrNoRoute) {
		t.Errorf("err = %v, want ErrNoRoute", err)
	}
}

// The coordinate separators "," and ";" are legal in a path segment and must
// reach the server unescaped, or the API cannot parse them.
func TestRoutesRequestPathEncoding(t *testing.T) {
	tests := []struct {
		name     string
		req      *RouteRequest
		wantPath string
	}{
		{
			name:     "origin and destination",
			req:      simpleRoute(),
			wantPath: "/route/v1/driving/103.8198,1.3521;103.9915,1.3644",
		},
		{
			name: "waypoints are ordered between origin and destination",
			req: &RouteRequest{
				Origin:      Point{103.8198, 1.3521},
				Destination: Point{103.9915, 1.3644},
				Waypoints:   []Point{{103.85, 1.29}, {103.9, 1.31}},
			},
			wantPath: "/route/v1/driving/103.8198,1.3521;103.85,1.29;103.9,1.31;103.9915,1.3644",
		},
		{
			name: "custom profile",
			req: &RouteRequest{
				Origin:      Point{103.8198, 1.3521},
				Destination: Point{103.9915, 1.3644},
				Profile:     "cycling",
			},
			wantPath: "/route/v1/cycling/103.8198,1.3521;103.9915,1.3644",
		},
		{
			name: "negative and high-precision coordinates",
			req: &RouteRequest{
				Origin:      Point{-0.1276474, 51.5073219},
				Destination: Point{-3.188267, 55.953251},
			},
			wantPath: "/route/v1/driving/-0.1276474,51.5073219;-3.188267,55.953251",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath, gotRawPath string
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotRawPath = r.URL.EscapedPath()
				_, _ = w.Write([]byte(okRoute))
			})

			if _, err := c.Routes.Get(context.Background(), tc.req); err != nil {
				t.Fatalf("Get: %v", err)
			}
			if gotPath != tc.wantPath {
				t.Errorf("path  = %q\nwant  = %q", gotPath, tc.wantPath)
			}
			if gotRawPath != tc.wantPath {
				t.Errorf("escaped path = %q, want it unescaped as %q", gotRawPath, tc.wantPath)
			}
		})
	}
}

func TestRoutesQueryEncoding(t *testing.T) {
	yes := true
	tests := []struct {
		name  string
		req   *RouteRequest
		want  map[string]string
		empty []string
	}{
		{
			name:  "defaults send no options",
			req:   simpleRoute(),
			empty: []string{"alternatives", "steps", "geometries", "overview", "annotations"},
		},
		{
			name: "all options",
			req: &RouteRequest{
				Origin:           Point{103.8198, 1.3521},
				Destination:      Point{103.9915, 1.3644},
				Alternatives:     3,
				Steps:            true,
				Annotations:      []string{"duration", "distance"},
				Geometries:       "geojson",
				Overview:         "full",
				ContinueStraight: &yes,
				Exclude:          []string{"motorway", "ferry"},
			},
			want: map[string]string{
				"alternatives":      "3",
				"steps":             "true",
				"annotations":       "duration,distance",
				"geometries":        "geojson",
				"overview":          "full",
				"continue_straight": "true",
				"exclude":           "motorway,ferry",
			},
		},
		{
			name: "overview can be disabled",
			req: &RouteRequest{
				Origin:      Point{103.8198, 1.3521},
				Destination: Point{103.9915, 1.3644},
				Overview:    "false",
			},
			want: map[string]string{"overview": "false"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var query map[string][]string
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				query = r.URL.Query()
				_, _ = w.Write([]byte(okRoute))
			})

			if _, err := c.Routes.Get(context.Background(), tc.req); err != nil {
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

func TestRoutesValidation(t *testing.T) {
	tests := []struct {
		name string
		req  *RouteRequest
	}{
		{name: "nil request", req: nil},
		{name: "missing origin", req: &RouteRequest{Destination: Point{103.9, 1.3}}},
		{name: "missing destination", req: &RouteRequest{Origin: Point{103.8, 1.3}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				t.Error("no request should reach the server")
			})
			if _, err := c.Routes.Get(context.Background(), tc.req); !errors.Is(err, ErrInvalidRequest) {
				t.Errorf("err = %v, want ErrInvalidRequest", err)
			}
		})
	}
}

// Malformed coordinates are caught locally so a request is not wasted.
func TestRoutesRejectsBadCoordinatesLocally(t *testing.T) {
	tests := []struct {
		name string
		req  *RouteRequest
	}{
		{
			name: "wrong number of values",
			req:  &RouteRequest{Origin: Point{103.8}, Destination: Point{103.9, 1.3}},
		},
		{
			name: "longitude out of range",
			req:  &RouteRequest{Origin: Point{200, 1.3}, Destination: Point{103.9, 1.3}},
		},
		{
			// The classic [lat, lon] mix-up: 103.8 is not a latitude.
			name: "swapped latitude and longitude",
			req:  &RouteRequest{Origin: Point{1.3521, 103.8198}, Destination: Point{103.9, 1.3}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				t.Error("no request should reach the server")
			})
			if _, err := c.Routes.Get(context.Background(), tc.req); !errors.Is(err, ErrInvalidCoordinates) {
				t.Errorf("err = %v, want ErrInvalidCoordinates", err)
			}
		})
	}
}
