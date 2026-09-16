package justrouting

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
)

const solutionFixture = `{
  "code": 0,
  "summary": {
    "cost": 2841, "routes": 1, "unassigned": 1,
    "delivery": [3], "pickup": [0],
    "setup": 0, "service": 900, "duration": 2841,
    "waiting_time": 0, "priority": 0, "distance": 31204
  },
  "unassigned": [
    {"id": 4, "location": [103.7, 1.42], "type": "job", "description": "far depot"}
  ],
  "routes": [
    {
      "vehicle": 1, "cost": 2841, "setup": 0, "service": 900,
      "duration": 2841, "waiting_time": 0, "priority": 0, "distance": 31204,
      "delivery": [3], "pickup": [0],
      "steps": [
        {"type":"start","location":[103.8198,1.3521],"setup":0,"service":0,"waiting_time":0,"arrival":0,"duration":0,"distance":0},
        {"type":"job","location":[103.8514,1.2897],"id":1,"job":1,"setup":0,"service":300,"waiting_time":0,"arrival":842,"duration":842,"distance":9120,"load":[2]},
        {"type":"end","location":[103.8198,1.3521],"setup":0,"service":0,"waiting_time":0,"arrival":2841,"duration":2841,"distance":31204}
      ]
    }
  ]
}`

func TestOptimizationSolveDecodesResponse(t *testing.T) {
	c := newTestClient(t, jsonHandler(http.StatusOK, solutionFixture))

	solution, err := c.Optimization.Solve(context.Background(), simpleOptimization())
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}

	if solution.Code != 0 {
		t.Errorf("Code = %d, want 0", solution.Code)
	}
	if solution.Summary.Cost != 2841 {
		t.Errorf("Summary.Cost = %d, want 2841", solution.Summary.Cost)
	}
	if solution.Summary.Distance != 31204 {
		t.Errorf("Summary.Distance = %d, want 31204", solution.Summary.Distance)
	}

	if len(solution.Routes) != 1 {
		t.Fatalf("len(Routes) = %d, want 1", len(solution.Routes))
	}
	route := solution.Routes[0]
	if route.Vehicle != 1 {
		t.Errorf("Vehicle = %d, want 1", route.Vehicle)
	}
	if len(route.Steps) != 3 {
		t.Fatalf("len(Steps) = %d, want 3", len(route.Steps))
	}
	if route.Steps[0].Type != "start" || route.Steps[2].Type != "end" {
		t.Errorf("steps = %q..%q, want start..end", route.Steps[0].Type, route.Steps[2].Type)
	}
	if job := route.Steps[1]; job.Job != 1 || job.Arrival != 842 {
		t.Errorf("job step = {Job:%d Arrival:%d}, want {1 842}", job.Job, job.Arrival)
	}
	if lon := route.Steps[1].Location.Lon(); lon != 103.8514 {
		t.Errorf("step location lon = %v, want 103.8514", lon)
	}

	if len(solution.Unassigned) != 1 {
		t.Fatalf("len(Unassigned) = %d, want 1", len(solution.Unassigned))
	}
	if solution.Unassigned[0].ID != 4 {
		t.Errorf("Unassigned[0].ID = %d, want 4", solution.Unassigned[0].ID)
	}
}

// The request body must match the optimization engine's schema exactly.
func TestOptimizationRequestBody(t *testing.T) {
	var body map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/optimize" {
			t.Errorf("path = %q, want /optimize", r.URL.Path)
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("request body is not valid JSON: %v", err)
		}
		_, _ = w.Write([]byte(`{"code":0,"summary":{},"routes":[],"unassigned":[]}`))
	})

	window := TimeWindow{28800, 43200}
	req := &OptimizationRequest{
		Vehicles: []Vehicle{{
			ID:         1,
			Start:      Point{103.8198, 1.3521},
			End:        Point{103.8198, 1.3521},
			Capacity:   []int{4},
			Skills:     []int{1},
			TimeWindow: &window,
		}},
		Jobs: []Job{{
			ID:          1,
			Location:    Point{103.8514, 1.2897},
			Service:     300,
			Delivery:    []int{1},
			TimeWindows: []TimeWindow{{28800, 32400}},
		}},
		Options: &OptimizationOptions{Geometry: true},
	}

	if _, err := c.Optimization.Solve(context.Background(), req); err != nil {
		t.Fatalf("Solve: %v", err)
	}

	vehicles, ok := body["vehicles"].([]any)
	if !ok || len(vehicles) != 1 {
		t.Fatalf("vehicles = %v, want one entry", body["vehicles"])
	}
	vehicle := vehicles[0].(map[string]any)
	// Coordinates must serialise as [lon, lat] arrays.
	start, ok := vehicle["start"].([]any)
	if !ok || len(start) != 2 || start[0].(float64) != 103.8198 {
		t.Errorf("vehicle start = %v, want [103.8198, 1.3521]", vehicle["start"])
	}
	if tw, ok := vehicle["time_window"].([]any); !ok || len(tw) != 2 || tw[0].(float64) != 28800 {
		t.Errorf("time_window = %v, want [28800, 43200]", vehicle["time_window"])
	}

	jobs := body["jobs"].([]any)
	job := jobs[0].(map[string]any)
	if job["service"].(float64) != 300 {
		t.Errorf("job service = %v, want 300", job["service"])
	}
	if tws, ok := job["time_windows"].([]any); !ok || len(tws) != 1 {
		t.Errorf("time_windows = %v, want one window", job["time_windows"])
	}

	if options, ok := body["options"].(map[string]any); !ok || options["g"] != true {
		t.Errorf("options = %v, want {g:true}", body["options"])
	}

	// Shipments were not set and must be omitted rather than sent as null.
	if _, present := body["shipments"]; present {
		t.Error("shipments should be omitted when empty")
	}
}

func TestOptimizationShipments(t *testing.T) {
	var body map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{"code":0,"summary":{},"routes":[],"unassigned":[]}`))
	})

	req := &OptimizationRequest{
		Vehicles: []Vehicle{{ID: 1, Start: Point{103.8198, 1.3521}}},
		Shipments: []Shipment{{
			Pickup:   ShipmentStep{ID: 1, Location: Point{103.85, 1.29}},
			Delivery: ShipmentStep{ID: 2, Location: Point{103.9, 1.31}},
			Amount:   []int{1},
		}},
	}

	if _, err := c.Optimization.Solve(context.Background(), req); err != nil {
		t.Fatalf("Solve: %v", err)
	}
	shipments, ok := body["shipments"].([]any)
	if !ok || len(shipments) != 1 {
		t.Fatalf("shipments = %v, want one entry", body["shipments"])
	}
	if _, present := body["jobs"]; present {
		t.Error("jobs should be omitted when only shipments are given")
	}
}

func TestOptimizationValidation(t *testing.T) {
	depot := Point{103.8198, 1.3521}

	tests := []struct {
		name string
		req  *OptimizationRequest
		want error
	}{
		{
			name: "nil request",
			req:  nil,
			want: ErrInvalidRequest,
		},
		{
			name: "no vehicles",
			req:  &OptimizationRequest{Jobs: []Job{{ID: 1, Location: depot}}},
			want: ErrInvalidRequest,
		},
		{
			name: "no jobs or shipments",
			req:  &OptimizationRequest{Vehicles: []Vehicle{{ID: 1, Start: depot}}},
			want: ErrInvalidRequest,
		},
		{
			name: "vehicle without a start or end",
			req: &OptimizationRequest{
				Vehicles: []Vehicle{{ID: 1}},
				Jobs:     []Job{{ID: 1, Location: depot}},
			},
			want: ErrInvalidRequest,
		},
		{
			name: "job with an invalid location",
			req: &OptimizationRequest{
				Vehicles: []Vehicle{{ID: 1, Start: depot}},
				Jobs:     []Job{{ID: 1, Location: Point{500, 1.3}}},
			},
			want: ErrInvalidCoordinates,
		},
		{
			name: "job with no location",
			req: &OptimizationRequest{
				Vehicles: []Vehicle{{ID: 1, Start: depot}},
				Jobs:     []Job{{ID: 1}},
			},
			want: ErrInvalidCoordinates,
		},
		{
			name: "shipment with an invalid delivery location",
			req: &OptimizationRequest{
				Vehicles: []Vehicle{{ID: 1, Start: depot}},
				Shipments: []Shipment{{
					Pickup:   ShipmentStep{ID: 1, Location: depot},
					Delivery: ShipmentStep{ID: 2, Location: Point{0, 91}},
				}},
			},
			want: ErrInvalidCoordinates,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				t.Error("no request should reach the server")
			})
			if _, err := c.Optimization.Solve(context.Background(), tc.req); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestOptimizationPlanLimits(t *testing.T) {
	for _, body := range []string{`{"error":"too many jobs"}`, `{"error":"too many vehicles"}`} {
		t.Run(body, func(t *testing.T) {
			c := newTestClient(t, jsonHandler(http.StatusBadRequest, body), WithMaxRetries(0))
			_, err := c.Optimization.Solve(context.Background(), simpleOptimization())
			if !errors.Is(err, ErrPlanLimitExceeded) {
				t.Errorf("err = %v, want ErrPlanLimitExceeded", err)
			}
		})
	}
}
