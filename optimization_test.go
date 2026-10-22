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
    "waiting_time": 0, "priority": 0, "distance": 31204,
    "violations": []
  },
  "unassigned": [
    {"id": 4, "location": [103.7, 1.42], "location_index": 3, "type": "job", "description": "far depot"}
  ],
  "routes": [
    {
      "vehicle": 1, "cost": 2841, "setup": 0, "service": 900,
      "duration": 2841, "waiting_time": 0, "priority": 0, "distance": 31204,
      "delivery": [3], "pickup": [0],
      "description": "van one", "geometry": "encoded_polyline", "violations": [],
      "steps": [
        {"type":"start","location":[103.8198,1.3521],"setup":0,"service":0,"waiting_time":0,"arrival":0,"duration":0,"distance":0,"violations":[]},
        {"type":"job","location":[103.8514,1.2897],"location_index":1,"id":1,"job":1,"setup":0,"service":300,"waiting_time":0,"arrival":842,"duration":842,"distance":9120,"load":[2],"violations":[]},
        {"type":"end","location":[103.8198,1.3521],"setup":0,"service":0,"waiting_time":0,"arrival":2841,"duration":2841,"distance":31204,"violations":[]}
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
	if solution.Unassigned[0].LocationIndex != 3 {
		t.Errorf("Unassigned[0].LocationIndex = %d, want 3", solution.Unassigned[0].LocationIndex)
	}

	if route.Description != "van one" {
		t.Errorf("Description = %q, want \"van one\"", route.Description)
	}
	if route.Geometry != "encoded_polyline" {
		t.Errorf("Geometry = %q, want \"encoded_polyline\"", route.Geometry)
	}
	if job := route.Steps[1]; job.ID != 1 || job.Load[0] != 2 || job.LocationIndex != 1 {
		t.Errorf("job step = {ID:%d Load:%v LocationIndex:%d}, want {1 [2] 1}",
			job.ID, job.Load, job.LocationIndex)
	}
	if len(solution.Summary.Violations) != 0 {
		t.Errorf("Summary.Violations = %v, want empty", solution.Summary.Violations)
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
			Type:       "van",
			Breaks: []Break{{
				ID:          1,
				TimeWindows: []TimeWindow{{36000, 39600}},
				Service:     1800,
				MaxLoad:     []int{2},
			}},
			SpeedFactor:   0.8,
			MaxTasks:      3,
			MaxTravelTime: 14400,
			MaxDistance:   50000,
			Costs:         &VehicleCosts{Fixed: 10, PerHour: 2000, PerTaskHour: 300, PerKM: 2},
			Steps:         []VehicleStep{{Type: "job", ID: 1}},
		}},
		Jobs: []Job{{
			ID:             1,
			Location:       Point{103.8514, 1.2897},
			Service:        300,
			SetupPerType:   map[string]int{"van": 60},
			ServicePerType: map[string]int{"van": 600},
			Delivery:       []int{1},
			TimeWindows:    []TimeWindow{{28800, 32400}},
		}},
		Matrices: map[string]CustomMatrix{
			"car": {
				Durations: [][]int{{0, 14}, {21, 0}},
				Distances: [][]int{{0, 1500}, {1500, 0}},
				Costs:     [][]int{{0, 1}, {1, 0}},
			},
		},
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
	if vehicle["type"] != "van" {
		t.Errorf("vehicle type = %v, want \"van\"", vehicle["type"])
	}
	if vehicle["speed_factor"].(float64) != 0.8 {
		t.Errorf("speed_factor = %v, want 0.8", vehicle["speed_factor"])
	}
	if vehicle["max_tasks"].(float64) != 3 || vehicle["max_travel_time"].(float64) != 14400 ||
		vehicle["max_distance"].(float64) != 50000 {
		t.Errorf("vehicle limits = %v/%v/%v, want 3/14400/50000",
			vehicle["max_tasks"], vehicle["max_travel_time"], vehicle["max_distance"])
	}
	costs, ok := vehicle["costs"].(map[string]any)
	if !ok || costs["fixed"].(float64) != 10 || costs["per_hour"].(float64) != 2000 ||
		costs["per_task_hour"].(float64) != 300 || costs["per_km"].(float64) != 2 {
		t.Errorf("costs = %v, want {fixed:10 per_hour:2000 per_task_hour:300 per_km:2}", vehicle["costs"])
	}
	breaks, ok := vehicle["breaks"].([]any)
	if !ok || len(breaks) != 1 {
		t.Fatalf("breaks = %v, want one entry", vehicle["breaks"])
	}
	brk := breaks[0].(map[string]any)
	if brk["id"].(float64) != 1 || brk["service"].(float64) != 1800 {
		t.Errorf("break = %v, want {id:1 service:1800}", brk)
	}
	if mls, ok := brk["max_load"].([]any); !ok || len(mls) != 1 || mls[0].(float64) != 2 {
		t.Errorf("break max_load = %v, want [2]", brk["max_load"])
	}
	steps, ok := vehicle["steps"].([]any)
	if !ok || len(steps) != 1 || steps[0].(map[string]any)["type"] != "job" ||
		steps[0].(map[string]any)["id"].(float64) != 1 {
		t.Errorf("steps = %v, want [{type:job id:1}]", vehicle["steps"])
	}

	jobs := body["jobs"].([]any)
	job := jobs[0].(map[string]any)
	if job["service"].(float64) != 300 {
		t.Errorf("job service = %v, want 300", job["service"])
	}
	if tws, ok := job["time_windows"].([]any); !ok || len(tws) != 1 {
		t.Errorf("time_windows = %v, want one window", job["time_windows"])
	}
	if spt, ok := job["setup_per_type"].(map[string]any); !ok || spt["van"].(float64) != 60 {
		t.Errorf("setup_per_type = %v, want {van:60}", job["setup_per_type"])
	}
	if spt, ok := job["service_per_type"].(map[string]any); !ok || spt["van"].(float64) != 600 {
		t.Errorf("service_per_type = %v, want {van:600}", job["service_per_type"])
	}

	matrices, ok := body["matrices"].(map[string]any)
	if !ok {
		t.Fatalf("matrices = %v, want a car entry", body["matrices"])
	}
	car := matrices["car"].(map[string]any)
	if durs, ok := car["durations"].([]any); !ok || len(durs) != 2 {
		t.Errorf("car durations = %v, want a 2x2 matrix", car["durations"])
	}
	if _, ok := car["distances"]; !ok {
		t.Error("car distances should be present")
	}
	if _, ok := car["costs"]; !ok {
		t.Error("car costs should be present")
	}

	if options, ok := body["options"].(map[string]any); !ok || options["g"] != true {
		t.Errorf("options = %v, want {g:true}", body["options"])
	}

	// Shipments were not set and must be omitted rather than sent as null.
	if _, present := body["shipments"]; present {
		t.Error("shipments should be omitted when empty")
	}
}

// Zero-valued new fields must be omitted rather than serialized, and index-
// only locations must pass validation and serialize without coordinates.
func TestOptimizationNewFieldsOmissionAndIndices(t *testing.T) {
	startIndex, locationIndex := 0, 1
	req := &OptimizationRequest{
		Vehicles: []Vehicle{{
			ID:         1,
			StartIndex: &startIndex,
			Type:       "van",
		}},
		Jobs: []Job{{
			ID:            1,
			LocationIndex: &locationIndex,
			Service:       300,
		}},
		Matrices: map[string]CustomMatrix{
			"car": {Durations: [][]int{{0, 14}, {21, 0}}},
		},
	}

	var body map[string]any
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("request body is not valid JSON: %v", err)
		}
		_, _ = w.Write([]byte(`{"code":0,"summary":{},"routes":[],"unassigned":[]}`))
	})

	if _, err := c.Optimization.Solve(context.Background(), req); err != nil {
		t.Fatalf("Solve: %v", err)
	}

	vehicle := body["vehicles"].([]any)[0].(map[string]any)
	if _, ok := vehicle["start"]; ok {
		t.Error("start should be omitted when only start_index is set")
	}
	if vehicle["start_index"].(float64) != 0 {
		t.Errorf("start_index = %v, want 0 (row 0 must survive serialization)", vehicle["start_index"])
	}
	if vehicle["type"] != "van" {
		t.Errorf("type = %v, want \"van\"", vehicle["type"])
	}
	for _, key := range []string{"end", "end_index", "capacity", "skills", "time_window",
		"breaks", "speed_factor", "max_tasks", "max_travel_time", "max_distance",
		"costs", "steps", "description"} {
		if _, ok := vehicle[key]; ok {
			t.Errorf("vehicles[0].%s should be omitted when empty", key)
		}
	}

	job := body["jobs"].([]any)[0].(map[string]any)
	if _, ok := job["location"]; ok {
		t.Error("location should be omitted when only location_index is set")
	}
	if job["location_index"].(float64) != 1 {
		t.Errorf("location_index = %v, want 1", job["location_index"])
	}
	if _, ok := job["setup"]; ok {
		t.Error("job setup should be omitted when zero")
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
			name: "vehicle without start/end or indices",
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
			name: "job with neither location nor index",
			req: &OptimizationRequest{
				Vehicles: []Vehicle{{ID: 1, Start: depot}},
				Jobs:     []Job{{ID: 1}},
			},
			want: ErrInvalidRequest,
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
		{
			name: "shipment step with neither location nor index",
			req: &OptimizationRequest{
				Vehicles: []Vehicle{{ID: 1, Start: depot}},
				Shipments: []Shipment{{
					Pickup:   ShipmentStep{ID: 1, Location: depot},
					Delivery: ShipmentStep{ID: 2},
				}},
			},
			want: ErrInvalidRequest,
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
