package justrouting

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// matrixFixture includes a null entry, which is how the engine reports a pair
// it cannot connect.
const matrixFixture = `{
  "code": "Ok",
  "durations": [[0, 612.4, 1583.4], [598.1, 0, null], [1601.2, null, 0]],
  "distances": [[0, 8421.5, 24512.7], [8390.2, 0, null], [24488.1, null, 0]],
  "sources": [
    {"hint":"a","distance":4.2,"name":"Marina Boulevard","location":[103.81982,1.35211]},
    {"hint":"b","distance":2.1,"name":"Orchard Road","location":[103.83,1.3048]},
    {"hint":"c","distance":8.1,"name":"Changi Coast Road","location":[103.99151,1.36442]}
  ],
  "destinations": [
    {"hint":"a","distance":4.2,"name":"Marina Boulevard","location":[103.81982,1.35211]},
    {"hint":"b","distance":2.1,"name":"Orchard Road","location":[103.83,1.3048]},
    {"hint":"c","distance":8.1,"name":"Changi Coast Road","location":[103.99151,1.36442]}
  ]
}`

func threePoints() []Point {
	return []Point{
		{103.8198, 1.3521},
		{103.83, 1.3048},
		{103.9915, 1.3644},
	}
}

func TestMatrixGetDecodesResponse(t *testing.T) {
	c := newTestClient(t, jsonHandler(http.StatusOK, matrixFixture))

	m, err := c.Matrix.Get(context.Background(), &MatrixRequest{Coordinates: threePoints()})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if len(m.Durations) != 3 {
		t.Fatalf("len(Durations) = %d, want 3", len(m.Durations))
	}
	if len(m.Sources) != 3 || len(m.Destinations) != 3 {
		t.Fatalf("sources/destinations = %d/%d, want 3/3", len(m.Sources), len(m.Destinations))
	}

	if got, ok := m.Duration(0, 1); !ok || got != 612.4 {
		t.Errorf("Duration(0,1) = %v, %v; want 612.4, true", got, ok)
	}
	if got, ok := m.Distance(0, 2); !ok || got != 24512.7 {
		t.Errorf("Distance(0,2) = %v, %v; want 24512.7, true", got, ok)
	}
}

// A null entry means unreachable. It must stay distinguishable from a real
// zero, which is why the matrices hold pointers.
func TestMatrixUnreachablePairs(t *testing.T) {
	c := newTestClient(t, jsonHandler(http.StatusOK, matrixFixture))

	m, err := c.Matrix.Get(context.Background(), &MatrixRequest{Coordinates: threePoints()})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if got, ok := m.Duration(1, 2); ok {
		t.Errorf("Duration(1,2) = %v, true; want unreachable", got)
	}
	// The diagonal is a genuine zero and must report ok.
	if got, ok := m.Duration(0, 0); !ok || got != 0 {
		t.Errorf("Duration(0,0) = %v, %v; want 0, true", got, ok)
	}
}

func TestMatrixAccessorBounds(t *testing.T) {
	c := newTestClient(t, jsonHandler(http.StatusOK, matrixFixture))

	m, err := c.Matrix.Get(context.Background(), &MatrixRequest{Coordinates: threePoints()})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	for _, tc := range []struct{ i, j int }{{-1, 0}, {0, -1}, {3, 0}, {0, 3}, {99, 99}} {
		if _, ok := m.Duration(tc.i, tc.j); ok {
			t.Errorf("Duration(%d,%d) reported ok for an out-of-range index", tc.i, tc.j)
		}
		if _, ok := m.Distance(tc.i, tc.j); ok {
			t.Errorf("Distance(%d,%d) reported ok for an out-of-range index", tc.i, tc.j)
		}
	}
}

func TestMatrixRequestEncoding(t *testing.T) {
	tests := []struct {
		name      string
		req       *MatrixRequest
		wantPath  string
		wantQuery map[string]string
		omitted   []string
	}{
		{
			name:      "defaults request both annotations",
			req:       &MatrixRequest{Coordinates: threePoints()},
			wantPath:  "/table/v1/driving/103.8198,1.3521;103.83,1.3048;103.9915,1.3644",
			wantQuery: map[string]string{"annotations": "duration,distance"},
			omitted:   []string{"sources", "destinations"},
		},
		{
			name: "sources and destinations subsets",
			req: &MatrixRequest{
				Coordinates:  threePoints(),
				Sources:      []int{0},
				Destinations: []int{1, 2},
			},
			wantQuery: map[string]string{
				"sources":      "0",
				"destinations": "1;2",
			},
		},
		{
			name: "single annotation",
			req: &MatrixRequest{
				Coordinates: threePoints(),
				Annotations: []string{"duration"},
			},
			wantQuery: map[string]string{"annotations": "duration"},
		},
		{
			name: "custom profile",
			req: &MatrixRequest{
				Coordinates: threePoints(),
				Profile:     "walking",
			},
			wantPath: "/table/v1/walking/103.8198,1.3521;103.83,1.3048;103.9915,1.3644",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var (
				gotPath string
				query   map[string][]string
			)
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				query = r.URL.Query()
				_, _ = w.Write([]byte(`{"code":"Ok"}`))
			})

			if _, err := c.Matrix.Get(context.Background(), tc.req); err != nil {
				t.Fatalf("Get: %v", err)
			}
			if tc.wantPath != "" && gotPath != tc.wantPath {
				t.Errorf("path = %q\nwant = %q", gotPath, tc.wantPath)
			}
			for key, want := range tc.wantQuery {
				if got := query[key]; len(got) != 1 || got[0] != want {
					t.Errorf("query %q = %v, want [%q]", key, got, want)
				}
			}
			for _, key := range tc.omitted {
				if got, ok := query[key]; ok {
					t.Errorf("query %q = %v, want it omitted", key, got)
				}
			}
		})
	}
}

func TestMatrixValidation(t *testing.T) {
	tests := []struct {
		name string
		req  *MatrixRequest
		want error
	}{
		{
			name: "nil request",
			req:  nil,
			want: ErrInvalidRequest,
		},
		{
			name: "no coordinates",
			req:  &MatrixRequest{},
			want: ErrInvalidRequest,
		},
		{
			name: "a matrix needs at least two points",
			req:  &MatrixRequest{Coordinates: []Point{{103.8, 1.3}}},
			want: ErrInvalidRequest,
		},
		{
			name: "source index beyond the coordinate list",
			req:  &MatrixRequest{Coordinates: threePoints(), Sources: []int{5}},
			want: ErrInvalidRequest,
		},
		{
			name: "negative destination index",
			req:  &MatrixRequest{Coordinates: threePoints(), Destinations: []int{-1}},
			want: ErrInvalidRequest,
		},
		{
			name: "invalid coordinate",
			req:  &MatrixRequest{Coordinates: []Point{{103.8, 1.3}, {0, 91}}},
			want: ErrInvalidCoordinates,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				t.Error("no request should reach the server")
			})
			if _, err := c.Matrix.Get(context.Background(), tc.req); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// The plan caps matrix size; the API rejects oversized requests.
func TestMatrixPlanLimit(t *testing.T) {
	c := newTestClient(t, jsonHandler(http.StatusBadRequest, `{"error":"matrix size exceeds plan limit"}`),
		WithMaxRetries(0))

	_, err := c.Matrix.Get(context.Background(), &MatrixRequest{Coordinates: threePoints()})
	if !errors.Is(err, ErrPlanLimitExceeded) {
		t.Errorf("err = %v, want ErrPlanLimitExceeded", err)
	}
}
