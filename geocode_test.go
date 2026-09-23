package justrouting

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"testing"
)

const geocodeFixture = `{
  "results": [{
    "datasource": {
      "sourcename": "openstreetmap",
      "attribution": "© OpenStreetMap contributors",
      "license": "Open Database License",
      "url": "https://www.openstreetmap.org/copyright"
    },
    "name": "Marina Bay Sands",
    "housenumber": "10",
    "street": "Bayfront Avenue",
    "city": "Singapore",
    "county": "Singapore",
    "state": "Singapore",
    "postcode": "018956",
    "country": "Singapore",
    "country_code": "sg",
    "lon": 103.859,
    "lat": 1.2834,
    "formatted": "Marina Bay Sands, 10 Bayfront Avenue, 018956, Singapore",
    "address_line1": "Marina Bay Sands",
    "address_line2": "10 Bayfront Avenue, 018956, Singapore",
    "result_type": "building",
    "rank": {
      "importance": 0.4,
      "popularity": 4.0,
      "confidence": 1,
      "confidence_city_level": 1,
      "confidence_street_level": 1,
      "match_type": "full_match"
    },
    "timezone": {
      "name": "Asia/Singapore",
      "offset_STD": "+08:00",
      "offset_STD_seconds": 28800,
      "offset_DST": "+08:00",
      "offset_DST_seconds": 28800,
      "abbreviation_STD": "+08",
      "abbreviation_DST": "+08"
    },
    "place_id": "51667b3e1416f7594059aad55757058af43ff00103f9018322790001000000c0",
    "category": "tourism.hotel",
    "plus_code": "7VH8V75X+9X",
    "bbox": {"lon1": 103.8574, "lat1": 1.2822, "lon2": 103.8605, "lat2": 1.2846},
    "distance": 1234
  }],
  "query": {
    "text": "Marina Bay Sands, Singapore",
    "parsed": {"city": "singapore", "country": "singapore", "expected_type": "building"}
  }
}`

func TestGeocodeSearchDecodesResponse(t *testing.T) {
	c := newTestClient(t, jsonHandler(http.StatusOK, geocodeFixture))

	resp, err := c.Geocode.Search(context.Background(), &GeocodeRequest{Text: "Marina Bay Sands"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("len(Results) = %d, want 1", len(resp.Results))
	}
	r := resp.Results[0]

	if r.Formatted != "Marina Bay Sands, 10 Bayfront Avenue, 018956, Singapore" {
		t.Errorf("Formatted = %q", r.Formatted)
	}
	if r.Street != "Bayfront Avenue" {
		t.Errorf("Street = %q", r.Street)
	}
	if r.CountryCode != "sg" {
		t.Errorf("CountryCode = %q, want sg", r.CountryCode)
	}
	if r.Lon != 103.859 || r.Lat != 1.2834 {
		t.Errorf("(Lon, Lat) = (%v, %v), want (103.859, 1.2834)", r.Lon, r.Lat)
	}
	if r.Rank.Confidence != 1 {
		t.Errorf("Rank.Confidence = %v, want 1", r.Rank.Confidence)
	}
	if r.Timezone.Name != "Asia/Singapore" {
		t.Errorf("Timezone.Name = %q", r.Timezone.Name)
	}
	if r.PlaceID != "51667b3e1416f7594059aad55757058af43ff00103f9018322790001000000c0" {
		t.Errorf("PlaceID = %q", r.PlaceID)
	}
	if r.PlusCode != "7VH8V75X+9X" {
		t.Errorf("PlusCode = %q", r.PlusCode)
	}
	if r.BBox.Lon2 != 103.8605 {
		t.Errorf("BBox.Lon2 = %v", r.BBox.Lon2)
	}
	if r.Distance != 1234 {
		t.Errorf("Distance = %v, want 1234", r.Distance)
	}

	loc := r.Location()
	if loc.Lon() != 103.859 || loc.Lat() != 1.2834 {
		t.Errorf("Location() = %v, want [103.859 1.2834]", loc)
	}
	if err := loc.Validate(); err != nil {
		t.Errorf("Location().Validate: %v", err)
	}

	if resp.Query == nil {
		t.Fatal("Query is nil")
	}
	if resp.Query.Text != "Marina Bay Sands, Singapore" {
		t.Errorf("Query.Text = %q", resp.Query.Text)
	}
	if !json.Valid(resp.Query.Parsed) {
		t.Errorf("Query.Parsed is not valid JSON: %s", resp.Query.Parsed)
	}
	var parsed map[string]any
	if err := json.Unmarshal(resp.Query.Parsed, &parsed); err != nil {
		t.Fatalf("unmarshal Query.Parsed: %v", err)
	}
	if parsed["city"] != "singapore" {
		t.Errorf("parsed[city] = %v, want singapore", parsed["city"])
	}
}

// An empty result list is a valid answer, not an error.
func TestGeocodeSearchEmptyResults(t *testing.T) {
	c := newTestClient(t, jsonHandler(http.StatusOK, `{"results":[]}`))

	resp, err := c.Geocode.Search(context.Background(), &GeocodeRequest{Text: "nowhere"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 0 {
		t.Errorf("len(Results) = %d, want 0", len(resp.Results))
	}
	if resp.Query != nil {
		t.Errorf("Query = %v, want nil", resp.Query)
	}
}

func TestGeocodeRequestEncoding(t *testing.T) {
	tests := []struct {
		name      string
		req       *GeocodeRequest
		wantPath  string
		wantQuery map[string]string
		wantMulti map[string][]string
		omitted   []string
	}{
		{
			name:     "text query",
			req:      &GeocodeRequest{Text: "Orchard Road"},
			wantPath: "/geocode/v1/search",
			wantQuery: map[string]string{
				"format": "json",
				"text":   "Orchard Road",
			},
			omitted: []string{
				"name", "housenumber", "street", "postcode", "city", "state", "country",
				"limit", "offset", "filter", "bias", "type", "lang",
			},
		},
		{
			name: "structured fields",
			req: &GeocodeRequest{Structured: &StructuredQuery{
				Name:        "Marina Bay Sands",
				Housenumber: "10",
				Street:      "Bayfront Avenue",
				Postcode:    "018956",
				City:        "Singapore",
				State:       "Singapore",
				Country:     "Singapore",
			}},
			wantPath: "/geocode/v1/search",
			wantQuery: map[string]string{
				"format":      "json",
				"name":        "Marina Bay Sands",
				"housenumber": "10",
				"street":      "Bayfront Avenue",
				"postcode":    "018956",
				"city":        "Singapore",
				"state":       "Singapore",
				"country":     "Singapore",
			},
			omitted: []string{"text"},
		},
		{
			name: "all options",
			req: &GeocodeRequest{
				Text:    "Orchard Road",
				Limit:   5,
				Offset:  10,
				Filters: []string{"countrycode:sg", "city:singapore"},
				Bias:    "proximity:103.8,1.3",
				Type:    "street",
				Lang:    "en",
			},
			wantQuery: map[string]string{
				"format": "json",
				"text":   "Orchard Road",
				"limit":  "5",
				"offset": "10",
				"bias":   "proximity:103.8,1.3",
				"type":   "street",
				"lang":   "en",
			},
			wantMulti: map[string][]string{
				"filter": {"countrycode:sg", "city:singapore"},
			},
		},
		{
			name: "zero values omitted",
			req:  &GeocodeRequest{Text: "Orchard Road", Filters: []string{""}},
			wantQuery: map[string]string{
				"format": "json",
				"text":   "Orchard Road",
			},
			omitted: []string{"limit", "offset", "filter", "bias", "type", "lang"},
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
				_, _ = w.Write([]byte(`{"results":[]}`))
			})

			if _, err := c.Geocode.Search(context.Background(), tc.req); err != nil {
				t.Fatalf("Search: %v", err)
			}
			if tc.wantPath != "" && gotPath != tc.wantPath {
				t.Errorf("path = %q\nwant = %q", gotPath, tc.wantPath)
			}
			for key, want := range tc.wantQuery {
				if got := query[key]; len(got) != 1 || got[0] != want {
					t.Errorf("query %q = %v, want [%q]", key, got, want)
				}
			}
			for key, want := range tc.wantMulti {
				if got := query[key]; !slices.Equal(got, want) {
					t.Errorf("query %q = %v, want %v", key, got, want)
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

func TestGeocodeValidation(t *testing.T) {
	tests := []struct {
		name string
		req  *GeocodeRequest
	}{
		{name: "nil request", req: nil},
		{name: "text and structured", req: &GeocodeRequest{Text: "x", Structured: &StructuredQuery{City: "Singapore"}}},
		{name: "neither input", req: &GeocodeRequest{}},
		{name: "empty structured", req: &GeocodeRequest{Structured: &StructuredQuery{}}},
		{name: "negative limit", req: &GeocodeRequest{Text: "x", Limit: -1}},
		{name: "negative offset", req: &GeocodeRequest{Text: "x", Offset: -1}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				t.Error("no request should reach the server")
			})
			if _, err := c.Geocode.Search(context.Background(), tc.req); !errors.Is(err, ErrInvalidRequest) {
				t.Errorf("err = %v, want ErrInvalidRequest", err)
			}
		})
	}
}

// Upstream geocoding errors pass through the gateway with their own body
// shape; classification must still map them onto the usual sentinels.
func TestGeocodeErrorPassthrough(t *testing.T) {
	t.Run("upstream unauthorized", func(t *testing.T) {
		c := newTestClient(t, jsonHandler(http.StatusUnauthorized,
			`{"statusCode":401,"error":"Unauthorized","message":"Invalid apiKey"}`), WithMaxRetries(0))

		_, err := c.Geocode.Search(context.Background(), &GeocodeRequest{Text: "Singapore"})
		if !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("err = %v, want ErrUnauthorized", err)
		}
		var apiErr *Error
		if !errors.As(err, &apiErr) {
			t.Fatalf("errors.As did not yield *Error; got %T", err)
		}
		if apiErr.StatusCode != http.StatusUnauthorized {
			t.Errorf("StatusCode = %d, want 401", apiErr.StatusCode)
		}
		if apiErr.Message != "Invalid apiKey" {
			t.Errorf("Message = %q, want Invalid apiKey", apiErr.Message)
		}
	})

	t.Run("upstream unavailable", func(t *testing.T) {
		c := newTestClient(t, jsonHandler(http.StatusBadGateway,
			`{"error":"upstream unavailable"}`), WithMaxRetries(0))

		_, err := c.Geocode.Search(context.Background(), &GeocodeRequest{Text: "Singapore"})
		if !errors.Is(err, ErrUpstreamUnavailable) {
			t.Fatalf("err = %v, want ErrUpstreamUnavailable", err)
		}
	})
}
