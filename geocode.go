package justrouting

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// GeocodeService converts addresses into coordinates.
type GeocodeService struct {
	client *Client
}

// GeocodeRequest describes a forward-geocoding search. Exactly one of Text
// and Structured must be set.
type GeocodeRequest struct {
	// Text is the free-form address to search for, such as
	// "Marina Bay Sands, Singapore". Mutually exclusive with Structured.
	Text string

	// Structured is an address given as typed components. Mutually
	// exclusive with Text.
	Structured *StructuredQuery

	// Limit caps the number of results. The API defaults to 5.
	Limit int

	// Offset skips the first Offset results before applying Limit.
	Offset int

	// Filters restrict results, for example "countrycode:sg". Each entry
	// is sent as its own filter parameter; the API applies all of them.
	Filters []string

	// Bias steers results toward a location, for example
	// "proximity:103.8,1.3". It is forwarded verbatim.
	Bias string

	// Type restricts results to a feature type such as "street" or "city".
	Type string

	// Lang requests results in a language, such as "en".
	Lang string
}

// StructuredQuery is an address expressed as typed fields. Only the fields
// that are set are sent.
type StructuredQuery struct {
	Name string
	// Housenumber is lowercased to match the upstream parameter name.
	Housenumber string
	Street      string
	Postcode    string
	City        string
	State       string
	Country     string
}

// GeocodeResponse is the result of a forward-geocoding search.
type GeocodeResponse struct {
	// Results are ordered best first. Empty when nothing matches.
	Results []*GeocodeResult `json:"results"`
	// Query echoes how the API interpreted the request, when returned.
	Query *GeocodeQuery `json:"query"`
}

// GeocodeQuery echoes how the API interpreted the request.
type GeocodeQuery struct {
	// Text is the free-text query as received by the API.
	Text string `json:"text"`
	// Parsed is the API's structured interpretation of Text. Its shape is
	// not guaranteed, so it is preserved as raw JSON.
	Parsed json.RawMessage `json:"parsed"`
}

// GeocodeResult is one matching place.
type GeocodeResult struct {
	// Datasource credits the data provider.
	Datasource   Datasource `json:"datasource,omitempty"`
	Housenumber  string     `json:"housenumber,omitempty"`
	Street       string     `json:"street,omitempty"`
	Suburb       string     `json:"suburb,omitempty"`
	City         string     `json:"city,omitempty"`
	County       string     `json:"county,omitempty"`
	State        string     `json:"state,omitempty"`
	StateCode    string     `json:"state_code,omitempty"`
	Postcode     string     `json:"postcode,omitempty"`
	Country      string     `json:"country,omitempty"`
	CountryCode  string     `json:"country_code,omitempty"`
	Lon          float64    `json:"lon"`
	Lat          float64    `json:"lat"`
	Formatted    string     `json:"formatted,omitempty"`
	AddressLine1 string     `json:"address_line1,omitempty"`
	AddressLine2 string     `json:"address_line2,omitempty"`
	ResultType   string     `json:"result_type,omitempty"`
	// Rank scores the result's relevance and match confidence.
	Rank Rank `json:"rank,omitempty"`
	// Timezone is the result's time zone with standard- and daylight-time
	// offsets.
	Timezone Timezone `json:"timezone,omitempty"`
	// PlaceID is the upstream's opaque identifier for the place.
	PlaceID  string `json:"place_id,omitempty"`
	Category string `json:"category,omitempty"`
	PlusCode string `json:"plus_code,omitempty"`
	Name     string `json:"name,omitempty"`
	// BBox is the result's bounding box as [lon1, lat1, lon2, lat2].
	BBox BBox `json:"bbox,omitempty"`
	// Distance is the metres from the bias location, present only when
	// Bias is set.
	Distance float64 `json:"distance,omitempty"`
}

// Datasource credits the data provider of a [GeocodeResult].
type Datasource struct {
	Sourcename  string `json:"sourcename"`
	Attribution string `json:"attribution"`
	License     string `json:"license"`
	URL         string `json:"url"`
}

// Rank scores a [GeocodeResult]'s relevance and match confidence.
type Rank struct {
	Importance            float64 `json:"importance"`
	Popularity            float64 `json:"popularity"`
	Confidence            float64 `json:"confidence"`
	ConfidenceCityLevel   float64 `json:"confidence_city_level"`
	ConfidenceStreetLevel float64 `json:"confidence_street_level"`
	MatchType             string  `json:"match_type"`
}

// Timezone is a [GeocodeResult]'s time zone with standard- and daylight-time
// offsets.
type Timezone struct {
	Name             string `json:"name"`
	OffsetSTD        string `json:"offset_STD"`
	OffsetSTDSeconds int    `json:"offset_STD_seconds"`
	OffsetDST        string `json:"offset_DST"`
	OffsetDSTSeconds int    `json:"offset_DST_seconds"`
	AbbreviationSTD  string `json:"abbreviation_STD"`
	AbbreviationDST  string `json:"abbreviation_DST"`
}

// BBox is a bounding box as [lon1, lat1, lon2, lat2].
type BBox struct {
	Lon1 float64 `json:"lon1"`
	Lat1 float64 `json:"lat1"`
	Lon2 float64 `json:"lon2"`
	Lat2 float64 `json:"lat2"`
}

// Location returns the result's position as a [Point], in the package's
// usual [longitude, latitude] order.
func (r *GeocodeResult) Location() Point { return Point{r.Lon, r.Lat} }

// Search geocodes req and returns matching places, ordered best first.
//
//	results, err := client.Geocode.Search(ctx, &justrouting.GeocodeRequest{
//		Text: "Marina Bay Sands, Singapore",
//	})
//	fmt.Println(results.Results[0].Formatted)
//
// A response with an empty Results list means nothing matched; it is not an
// error.
func (s *GeocodeService) Search(ctx context.Context, req *GeocodeRequest) (*GeocodeResponse, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}

	out := new(GeocodeResponse)
	err := s.client.do(ctx, &request{
		method:    http.MethodGet,
		path:      "/geocode/v1/search",
		query:     req.query(),
		needsAuth: true,
	}, out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *GeocodeRequest) validate() error {
	if r == nil {
		return fmt.Errorf("%w: request must not be nil", ErrInvalidRequest)
	}
	hasText := r.Text != ""
	hasStructured := r.Structured != nil && r.Structured.any()
	switch {
	case hasText && hasStructured:
		return fmt.Errorf("%w: Text and Structured are mutually exclusive", ErrInvalidRequest)
	case !hasText && !hasStructured:
		return fmt.Errorf("%w: exactly one of Text or Structured is required", ErrInvalidRequest)
	}
	if r.Limit < 0 {
		return fmt.Errorf("%w: Limit must not be negative, got %d", ErrInvalidRequest, r.Limit)
	}
	if r.Offset < 0 {
		return fmt.Errorf("%w: Offset must not be negative, got %d", ErrInvalidRequest, r.Offset)
	}
	return nil
}

// any reports whether at least one structured field is set.
func (s *StructuredQuery) any() bool {
	return s.Name != "" || s.Housenumber != "" || s.Street != "" ||
		s.Postcode != "" || s.City != "" || s.State != "" || s.Country != ""
}

func (r *GeocodeRequest) query() url.Values {
	q := url.Values{}
	// The client decodes JSON, so pin the response format instead of
	// relying on the API's default.
	q.Set("format", "json")
	if r.Text != "" {
		q.Set("text", r.Text)
	}
	if s := r.Structured; s != nil {
		set := func(key, v string) {
			if v != "" {
				q.Set(key, v)
			}
		}
		set("name", s.Name)
		set("housenumber", s.Housenumber)
		set("street", s.Street)
		set("postcode", s.Postcode)
		set("city", s.City)
		set("state", s.State)
		set("country", s.Country)
	}
	if r.Limit > 0 {
		q.Set("limit", strconv.Itoa(r.Limit))
	}
	if r.Offset > 0 {
		q.Set("offset", strconv.Itoa(r.Offset))
	}
	for _, f := range r.Filters {
		if f != "" {
			// Filter is repeatable; Add preserves the given order.
			q.Add("filter", f)
		}
	}
	if r.Bias != "" {
		q.Set("bias", r.Bias)
	}
	if r.Type != "" {
		q.Set("type", r.Type)
	}
	if r.Lang != "" {
		q.Set("lang", r.Lang)
	}
	return q
}
