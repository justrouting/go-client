package justrouting

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Point is a geographic coordinate expressed as [longitude, latitude] — the
// order used by GeoJSON, OSRM and VROOM, and the reverse of the "lat, lng"
// convention used by most map UIs.
//
// Point is defined over []float64, so an ordinary slice literal works
// anywhere a Point is expected:
//
//	Origin: []float64{103.8198, 1.3521}   // Singapore
type Point []float64

// Lon returns the longitude, or 0 if p is not a well-formed pair.
func (p Point) Lon() float64 {
	if len(p) < 2 {
		return 0
	}
	return p[0]
}

// Lat returns the latitude, or 0 if p is not a well-formed pair.
func (p Point) Lat() float64 {
	if len(p) < 2 {
		return 0
	}
	return p[1]
}

// Validate reports whether p is a well-formed [longitude, latitude] pair
// within the valid ranges. It wraps [ErrInvalidCoordinates].
func (p Point) Validate() error {
	if len(p) != 2 {
		return fmt.Errorf("%w: expected [longitude, latitude], got %d value(s)", ErrInvalidCoordinates, len(p))
	}
	lon, lat := p[0], p[1]
	if math.IsNaN(lon) || math.IsNaN(lat) || math.IsInf(lon, 0) || math.IsInf(lat, 0) {
		return fmt.Errorf("%w: longitude and latitude must be finite numbers", ErrInvalidCoordinates)
	}
	if lon < -180 || lon > 180 {
		return fmt.Errorf("%w: longitude %v is outside [-180, 180]", ErrInvalidCoordinates, lon)
	}
	if lat < -90 || lat > 90 {
		// Catches the common mistake of passing [lat, lon].
		return fmt.Errorf("%w: latitude %v is outside [-90, 90] (coordinates are [longitude, latitude])", ErrInvalidCoordinates, lat)
	}
	return nil
}

// String formats p as "longitude,latitude" using the shortest representation
// that round-trips exactly.
func (p Point) String() string {
	if len(p) != 2 {
		return ""
	}
	return formatCoord(p[0]) + "," + formatCoord(p[1])
}

func formatCoord(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// encodePoints renders points as the "lon,lat;lon,lat" path segment that the
// OSRM services expect, validating each point along the way.
func encodePoints(points []Point) (string, error) {
	if len(points) == 0 {
		return "", fmt.Errorf("%w: at least one coordinate is required", ErrInvalidCoordinates)
	}
	var b strings.Builder
	for i, p := range points {
		if err := p.Validate(); err != nil {
			return "", fmt.Errorf("coordinate %d: %w", i, err)
		}
		if i > 0 {
			b.WriteByte(';')
		}
		b.WriteString(p.String())
	}
	return b.String(), nil
}

// Geometry holds a route geometry. OSRM encodes it either as an encoded
// polyline string (geometries=polyline, the default, or polyline6) or as a
// GeoJSON LineString object (geometries=geojson). Geometry preserves whichever
// form the server sent; read it with [Geometry.Polyline] or [Geometry.GeoJSON].
type Geometry struct {
	raw json.RawMessage
}

// UnmarshalJSON implements [json.Unmarshaler].
func (g *Geometry) UnmarshalJSON(b []byte) error {
	g.raw = append(g.raw[:0], b...)
	return nil
}

// MarshalJSON implements [json.Marshaler].
func (g Geometry) MarshalJSON() ([]byte, error) {
	if len(g.raw) == 0 {
		return []byte("null"), nil
	}
	return g.raw, nil
}

// IsZero reports whether the server omitted the geometry, which happens when
// a request sets overview=false.
func (g Geometry) IsZero() bool {
	return len(g.raw) == 0 || string(g.raw) == "null"
}

// Raw returns the geometry exactly as the server encoded it.
func (g Geometry) Raw() json.RawMessage { return g.raw }

// Polyline returns the geometry as an encoded polyline string. It reports an
// error if the request asked for GeoJSON instead.
func (g Geometry) Polyline() (string, error) {
	if g.IsZero() {
		return "", fmt.Errorf("justrouting: geometry is empty")
	}
	var s string
	if err := json.Unmarshal(g.raw, &s); err != nil {
		return "", fmt.Errorf("justrouting: geometry is not an encoded polyline; set Geometries to \"polyline\" or \"polyline6\"")
	}
	return s, nil
}

// GeoJSON returns the geometry as a GeoJSON LineString. It reports an error if
// the request used the default polyline encoding.
func (g Geometry) GeoJSON() (*LineString, error) {
	if g.IsZero() {
		return nil, fmt.Errorf("justrouting: geometry is empty")
	}
	var ls LineString
	if err := json.Unmarshal(g.raw, &ls); err != nil {
		return nil, fmt.Errorf("justrouting: geometry is not GeoJSON; set Geometries to \"geojson\"")
	}
	return &ls, nil
}

// LineString is a GeoJSON LineString geometry.
type LineString struct {
	Type        string  `json:"type"`
	Coordinates []Point `json:"coordinates"`
}

// Waypoint is an input coordinate snapped to the road network.
type Waypoint struct {
	// Name is the street the coordinate snapped to, if known.
	Name string `json:"name"`
	// Location is the snapped position.
	Location Point `json:"location"`
	// Distance is the metres between the input coordinate and Location.
	Distance float64 `json:"distance"`
	// Hint is an opaque token that can speed up subsequent requests.
	Hint string `json:"hint,omitempty"`
}
