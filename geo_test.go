package justrouting

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
)

// The quickstart assigns a plain []float64 literal to a Point field. This
// compiles only because Point is defined over []float64, so guard it.
func TestPointAcceptsFloatSliceLiteral(t *testing.T) {
	req := &RouteRequest{
		Origin:      []float64{103.8198, 1.3521},
		Destination: []float64{103.9915, 1.3644},
		Waypoints:   []Point{{103.85, 1.29}},
	}
	if req.Origin.Lon() != 103.8198 {
		t.Errorf("Lon() = %v, want 103.8198", req.Origin.Lon())
	}
	if req.Origin.Lat() != 1.3521 {
		t.Errorf("Lat() = %v, want 1.3521", req.Origin.Lat())
	}
}

func TestPointAccessorsOnMalformedValues(t *testing.T) {
	for _, p := range []Point{nil, {}, {1.0}} {
		if got := p.Lon(); got != 0 {
			t.Errorf("Point(%v).Lon() = %v, want 0", p, got)
		}
		if got := p.Lat(); got != 0 {
			t.Errorf("Point(%v).Lat() = %v, want 0", p, got)
		}
	}
}

func TestPointValidate(t *testing.T) {
	tests := []struct {
		name    string
		point   Point
		wantErr bool
	}{
		{name: "singapore", point: Point{103.8198, 1.3521}},
		{name: "null island", point: Point{0, 0}},
		{name: "extremes", point: Point{-180, -90}},
		{name: "opposite extremes", point: Point{180, 90}},
		{name: "nil", point: nil, wantErr: true},
		{name: "empty", point: Point{}, wantErr: true},
		{name: "one value", point: Point{1}, wantErr: true},
		{name: "three values", point: Point{1, 2, 3}, wantErr: true},
		{name: "longitude too large", point: Point{180.1, 0}, wantErr: true},
		{name: "longitude too small", point: Point{-180.1, 0}, wantErr: true},
		{name: "latitude too large", point: Point{0, 90.1}, wantErr: true},
		{name: "latitude too small", point: Point{0, -90.1}, wantErr: true},
		{name: "NaN", point: Point{math.NaN(), 0}, wantErr: true},
		{name: "infinity", point: Point{math.Inf(1), 0}, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.point.Validate()
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error")
				}
				if !errors.Is(err, ErrInvalidCoordinates) {
					t.Errorf("err = %v, want ErrInvalidCoordinates", err)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// Coordinates must not lose precision or gain exponent notation on the way
// into a URL path.
func TestPointString(t *testing.T) {
	tests := []struct {
		point Point
		want  string
	}{
		{Point{103.8198, 1.3521}, "103.8198,1.3521"},
		{Point{-0.1276474, 51.5073219}, "-0.1276474,51.5073219"},
		{Point{0, 0}, "0,0"},
		{Point{103.819836000001, 1.352100000009}, "103.819836000001,1.352100000009"},
		{Point{0.0000001, 0.0000001}, "0.0000001,0.0000001"},
		{Point{1}, ""},
		{nil, ""},
	}

	for _, tc := range tests {
		if got := tc.point.String(); got != tc.want {
			t.Errorf("Point(%v).String() = %q, want %q", []float64(tc.point), got, tc.want)
		}
	}
}

func TestEncodePoints(t *testing.T) {
	t.Run("joins with semicolons", func(t *testing.T) {
		got, err := encodePoints([]Point{{103.8, 1.35}, {103.9, 1.36}, {104.0, 1.37}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if want := "103.8,1.35;103.9,1.36;104,1.37"; got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("rejects an empty list", func(t *testing.T) {
		if _, err := encodePoints(nil); !errors.Is(err, ErrInvalidCoordinates) {
			t.Errorf("err = %v, want ErrInvalidCoordinates", err)
		}
	})

	t.Run("reports which coordinate is bad", func(t *testing.T) {
		_, err := encodePoints([]Point{{103.8, 1.35}, {0, 91}})
		if err == nil {
			t.Fatal("expected an error")
		}
		if !errors.Is(err, ErrInvalidCoordinates) {
			t.Errorf("err = %v, want ErrInvalidCoordinates", err)
		}
		if got := err.Error(); !strings.Contains(got, "coordinate 1") {
			t.Errorf("err = %q, want it to identify coordinate 1", got)
		}
	})
}

func TestGeometryPolyline(t *testing.T) {
	var g Geometry
	if err := json.Unmarshal([]byte(`"ka|ceeEnAqB"`), &g); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	got, err := g.Polyline()
	if err != nil {
		t.Fatalf("Polyline: %v", err)
	}
	if want := "ka|ceeEnAqB"; got != want {
		t.Errorf("Polyline() = %q, want %q", got, want)
	}

	// Asking for the wrong encoding should explain the mismatch, not panic.
	if _, err := g.GeoJSON(); err == nil {
		t.Error("GeoJSON() on a polyline should fail")
	}
	if g.IsZero() {
		t.Error("IsZero() = true for a populated geometry")
	}
}

func TestGeometryGeoJSON(t *testing.T) {
	var g Geometry
	raw := `{"type":"LineString","coordinates":[[103.8198,1.3521],[103.9915,1.3644]]}`
	if err := json.Unmarshal([]byte(raw), &g); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	ls, err := g.GeoJSON()
	if err != nil {
		t.Fatalf("GeoJSON: %v", err)
	}
	if ls.Type != "LineString" {
		t.Errorf("Type = %q, want LineString", ls.Type)
	}
	if len(ls.Coordinates) != 2 {
		t.Fatalf("len(Coordinates) = %d, want 2", len(ls.Coordinates))
	}
	if ls.Coordinates[0].Lon() != 103.8198 {
		t.Errorf("first lon = %v, want 103.8198", ls.Coordinates[0].Lon())
	}

	if _, err := g.Polyline(); err == nil {
		t.Error("Polyline() on GeoJSON should fail")
	}
}

// overview=false omits the geometry entirely.
func TestGeometryAbsent(t *testing.T) {
	var g Geometry
	if !g.IsZero() {
		t.Error("zero Geometry should report IsZero")
	}
	if err := json.Unmarshal([]byte(`null`), &g); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !g.IsZero() {
		t.Error("null Geometry should report IsZero")
	}
	if _, err := g.Polyline(); err == nil {
		t.Error("Polyline() on an empty geometry should fail")
	}
	if _, err := g.GeoJSON(); err == nil {
		t.Error("GeoJSON() on an empty geometry should fail")
	}
}

func TestGeometryRoundTrip(t *testing.T) {
	for _, raw := range []string{
		`"ka|ceeEnAqB"`,
		`{"type":"LineString","coordinates":[[103.8198,1.3521]]}`,
	} {
		var g Geometry
		if err := json.Unmarshal([]byte(raw), &g); err != nil {
			t.Fatalf("Unmarshal(%s): %v", raw, err)
		}
		out, err := json.Marshal(g)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if string(out) != raw {
			t.Errorf("round trip = %s, want %s", out, raw)
		}
	}

	// A zero Geometry marshals as null rather than failing.
	out, err := json.Marshal(Geometry{})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(out) != "null" {
		t.Errorf("zero Geometry marshalled to %s, want null", out)
	}
}

// A Geometry is reused across attempts; unmarshalling twice must not append.
func TestGeometryUnmarshalReplaces(t *testing.T) {
	var g Geometry
	if err := json.Unmarshal([]byte(`"first"`), &g); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`"second"`), &g); err != nil {
		t.Fatal(err)
	}
	got, err := g.Polyline()
	if err != nil {
		t.Fatalf("Polyline: %v", err)
	}
	if got != "second" {
		t.Errorf("Polyline() = %q, want %q", got, "second")
	}
}
