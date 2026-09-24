package justrouting

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// tripFixture mirrors a real trip response: the waypoints reordered into the
// fastest visiting order, and the round trip itself.
const tripFixture = `{
  "code": "Ok",
  "waypoints": [
    {"hint":"aaa","distance":4.216,"name":"Marina Boulevard","location":[103.81982,1.35211]},
    {"hint":"ccc","distance":2.3,"name":"Sentosa Gateway","location":[103.8514,1.2897]},
    {"hint":"bbb","distance":8.104,"name":"Changi Coast Road","location":[103.99151,1.36442]}
  ],
  "trips": [
    {
      "geometry": "aa_ceeEnAqB",
      "legs": [
        {"steps":[],"summary":"","weight":1900.0,"duration":1900.0,"distance":30100.0}
      ],
      "weight_name": "routability",
      "weight": 1900.0,
      "duration": 1900.0,
      "distance": 30100.0
    }
  ]
}`

func TestTripGetDecodesResponse(t *testing.T) {
	c := newTestClient(t, jsonHandler(http.StatusOK, tripFixture))

	trip, err := c.Trip.Get(context.Background(), simpleTrip())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if trip.Distance != 30100.0 {
		t.Errorf("Distance = %v, want 30100.0", trip.Distance)
	}
	if trip.Duration != 1900.0 {
		t.Errorf("Duration = %v, want 1900.0", trip.Duration)
	}
	if len(trip.Legs) != 1 {
		t.Fatalf("len(Legs) = %d, want 1", len(trip.Legs))
	}
}

// GetAll additionally exposes the waypoints, reordered into the order the
// trip visits them.
func TestTripGetAllReturnsVisitingOrder(t *testing.T) {
	c := newTestClient(t, jsonHandler(http.StatusOK, tripFixture))

	resp, err := c.Trip.GetAll(context.Background(), simpleTrip())
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}

	if len(resp.Trips) != 1 {
		t.Fatalf("len(Trips) = %d, want 1", len(resp.Trips))
	}
	if len(resp.Waypoints) != 3 {
		t.Fatalf("len(Waypoints) = %d, want 3", len(resp.Waypoints))
	}
	// The input order was Marina, Sentosa, Changi; the engine may visit
	// Sentosa second.
	if resp.Waypoints[1].Name != "Sentosa Gateway" {
		t.Errorf("Waypoints[1].Name = %q, want %q", resp.Waypoints[1].Name, "Sentosa Gateway")
	}
}

// An empty trips array is a no-trip condition, not a nil-pointer panic.
func TestTripGetEmptyTrips(t *testing.T) {
	c := newTestClient(t, jsonHandler(http.StatusOK, `{"code":"Ok","trips":[],"waypoints":[]}`))

	_, err := c.Trip.Get(context.Background(), simpleTrip())
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.OSRMCode != "NoTrip" {
		t.Errorf("err = %v, want OSRMCode NoTrip", err)
	}
}

func TestTripRequestPathEncoding(t *testing.T) {
	tests := []struct {
		name     string
		req      *TripRequest
		wantPath string
	}{
		{
			name:     "points in any order",
			req:      simpleTrip(),
			wantPath: "/trip/v1/driving/103.8198,1.3521;103.8514,1.2897;103.9915,1.3644",
		},
		{
			name: "custom profile",
			req: &TripRequest{
				Coordinates: []Point{{103.8198, 1.3521}, {103.9915, 1.3644}},
				Profile:     "motorcycle",
			},
			wantPath: "/trip/v1/motorcycle/103.8198,1.3521;103.9915,1.3644",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				_, _ = w.Write([]byte(tripFixture))
			})

			if _, err := c.Trip.Get(context.Background(), tc.req); err != nil {
				t.Fatalf("Get: %v", err)
			}
			if gotPath != tc.wantPath {
				t.Errorf("path  = %q\nwant  = %q", gotPath, tc.wantPath)
			}
		})
	}
}

func TestTripQueryEncoding(t *testing.T) {
	no := false
	tests := []struct {
		name  string
		req   *TripRequest
		want  map[string]string
		empty []string
	}{
		{
			name:  "defaults send no options",
			req:   simpleTrip(),
			empty: []string{"roundtrip", "source", "destination", "steps", "geometries", "overview", "annotations"},
		},
		{
			name: "all options",
			req: &TripRequest{
				Coordinates: []Point{{103.8198, 1.3521}, {103.8514, 1.2897}},
				Roundtrip:   &no,
				Source:      "first",
				Destination: "last",
				Steps:       true,
				Annotations: []string{"duration", "distance"},
				Geometries:  "geojson",
				Overview:    "full",
				Exclude:     []string{"motorway", "ferry"},
			},
			want: map[string]string{
				"roundtrip":   "false",
				"source":      "first",
				"destination": "last",
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
				_, _ = w.Write([]byte(tripFixture))
			})

			if _, err := c.Trip.Get(context.Background(), tc.req); err != nil {
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

func TestTripValidation(t *testing.T) {
	tests := []struct {
		name string
		req  *TripRequest
	}{
		{name: "nil request", req: nil},
		{name: "one coordinate", req: &TripRequest{Coordinates: []Point{{103.8, 1.3}}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				t.Error("no request should reach the server")
			})
			if _, err := c.Trip.Get(context.Background(), tc.req); !errors.Is(err, ErrInvalidRequest) {
				t.Errorf("err = %v, want ErrInvalidRequest", err)
			}
		})
	}
}

// Malformed coordinates are caught locally so a request is not wasted.
func TestTripRejectsBadCoordinatesLocally(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no request should reach the server")
	})

	// The classic [lat, lon] mix-up: 103.8198 is not a latitude.
	_, err := c.Trip.Get(context.Background(), &TripRequest{
		Coordinates: []Point{{1.3521, 103.8198}, {103.9915, 1.3644}},
	})
	if !errors.Is(err, ErrInvalidCoordinates) {
		t.Errorf("err = %v, want ErrInvalidCoordinates", err)
	}
}

func simpleTrip() *TripRequest {
	return &TripRequest{
		Coordinates: []Point{
			{103.8198, 1.3521},
			{103.8514, 1.2897},
			{103.9915, 1.3644},
		},
	}
}
