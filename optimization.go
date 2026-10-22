package justrouting

import (
	"context"
	"fmt"
	"net/http"
)

// OptimizationService solves vehicle routing problems: given a fleet and a set
// of tasks, it assigns tasks to vehicles and orders each vehicle's stops.
type OptimizationService struct {
	client *Client
}

// OptimizationRequest describes a vehicle routing problem. At least one
// vehicle and at least one job or shipment are required.
//
// Fleet and task counts are capped by the account plan; exceeding either
// returns an error matching [ErrPlanLimitExceeded].
type OptimizationRequest struct {
	// Vehicles is the available fleet.
	Vehicles []Vehicle `json:"vehicles"`
	// Jobs are single-location tasks.
	Jobs []Job `json:"jobs,omitempty"`
	// Shipments are pickup-and-delivery pairs handled by one vehicle.
	Shipments []Shipment `json:"shipments,omitempty"`
	// Matrices are custom matrices per routing profile; the map keys are
	// profile names such as "car" or "motorcycle".
	Matrices map[string]CustomMatrix `json:"matrices,omitempty"`
	// Options tunes the solver.
	Options *OptimizationOptions `json:"options,omitempty"`
}

// OptimizationOptions tunes solver behaviour.
type OptimizationOptions struct {
	// Geometry requests a road-following geometry for each route. It costs
	// extra computation, so it is off by default.
	Geometry bool `json:"g,omitempty"`
}

// TimeWindow is an inclusive [start, end] pair, in seconds, relative to the
// same origin used by the rest of the request.
type TimeWindow [2]int

// CustomMatrix is a user-supplied travel-time, distance or cost matrix for
// one routing profile. Values are read as matrix[row][column]; rows and
// columns are indexed by the *_index fields on vehicles and tasks.
type CustomMatrix struct {
	// Durations is a travel-time matrix in seconds. Providing it for every
	// profile in use makes Location, Start and End optional.
	Durations [][]int `json:"durations,omitempty"`
	// Distances is a distance matrix in meters. It requires Durations.
	Distances [][]int `json:"distances,omitempty"`
	// Costs is a cost matrix used in all route cost evaluations.
	Costs [][]int `json:"costs,omitempty"`
}

// Break is a rest period scheduled during a vehicle's route.
type Break struct {
	// ID identifies the break. It must be unique within the vehicle.
	ID int `json:"id"`
	// TimeWindows bound when the break may start.
	TimeWindows []TimeWindow `json:"time_windows,omitempty"`
	// Service is the break length in seconds.
	Service int `json:"service,omitempty"`
	// Description is an opaque label echoed back in the solution.
	Description string `json:"description,omitempty"`
	// MaxLoad is the maximum load, per capacity dimension, under which
	// the break may happen.
	MaxLoad []int `json:"max_load,omitempty"`
}

// VehicleCosts adjusts how the solver values one vehicle.
type VehicleCosts struct {
	// Fixed is the one-time cost of using this vehicle. Defaults to 0.
	Fixed int `json:"fixed,omitempty"`
	// PerHour is the cost of one hour of travel time. Defaults to 3600.
	PerHour int `json:"per_hour,omitempty"`
	// PerTaskHour is the cost of one hour of task time (setup + service).
	// Defaults to 0.
	PerTaskHour int `json:"per_task_hour,omitempty"`
	// PerKM is the cost of one kilometer of travel. Defaults to 0.
	PerKM int `json:"per_km,omitempty"`
}

// VehicleStep is one entry of a custom route on a [Vehicle]. The hosted API
// runs in solving mode, where Steps warm-start the search and only the
// "job", "pickup" and "delivery" step types are used; the plan-only
// service_at, service_after and service_before keys would be ignored by the
// engine, so the client does not expose them.
type VehicleStep struct {
	// Type is "job", "pickup" or "delivery".
	Type string `json:"type"`
	// ID is the task ID this step refers to.
	ID int `json:"id,omitempty"`
}

// Violation describes one constraint violation. Violations are always empty
// in solving mode, the only mode the hosted API runs, but the fields exist
// for API completeness.
type Violation struct {
	// Cause is the violated constraint, e.g. "delay", "skills" or
	// "missing_break".
	Cause string `json:"cause"`
	// Duration is the lateness in seconds for "delay" causes, earliness
	// for "lead_time".
	Duration int `json:"duration"`
}

// Vehicle is one member of the fleet.
type Vehicle struct {
	// ID identifies the vehicle in the solution. It must be unique.
	ID int `json:"id"`
	// Profile selects the routing profile for this vehicle.
	Profile string `json:"profile,omitempty"`
	// Start is where the vehicle begins, as [longitude, latitude]. Omit
	// for a vehicle that may start anywhere.
	Start Point `json:"start,omitempty"`
	// StartIndex is the row/column in custom matrices that replaces Start.
	StartIndex *int `json:"start_index,omitempty"`
	// End is where the vehicle must finish. Omit to end anywhere.
	End Point `json:"end,omitempty"`
	// EndIndex is the row/column in custom matrices that replaces End.
	EndIndex *int `json:"end_index,omitempty"`
	// Capacity is a multidimensional capacity vector. Its length must
	// match the Delivery and Pickup vectors on tasks.
	Capacity []int `json:"capacity,omitempty"`
	// Skills lists capabilities this vehicle provides.
	Skills []int `json:"skills,omitempty"`
	// Type names this vehicle for per-type setup and service times on
	// tasks.
	Type string `json:"type,omitempty"`
	// TimeWindow bounds when the vehicle is available.
	TimeWindow *TimeWindow `json:"time_window,omitempty"`
	// Breaks are rest periods during the route.
	Breaks []Break `json:"breaks,omitempty"`
	// SpeedFactor scales all travel times for this vehicle, in (0, 5].
	SpeedFactor float64 `json:"speed_factor,omitempty"`
	// MaxTasks caps how many tasks the vehicle may be assigned.
	MaxTasks int `json:"max_tasks,omitempty"`
	// MaxTravelTime caps total travel time in seconds.
	MaxTravelTime int `json:"max_travel_time,omitempty"`
	// MaxDistance caps total travel distance in meters.
	MaxDistance int `json:"max_distance,omitempty"`
	// Costs adjusts how the solver values this vehicle.
	Costs *VehicleCosts `json:"costs,omitempty"`
	// Steps is a custom route used to warm-start the search.
	Steps []VehicleStep `json:"steps,omitempty"`
	// Description is an opaque label echoed back in the solution.
	Description string `json:"description,omitempty"`
}

// Job is a task carried out at a single location.
type Job struct {
	// ID identifies the job in the solution. It must be unique.
	ID int `json:"id"`
	// Location is where the job happens, as [longitude, latitude]. Omit
	// when custom matrices cover every profile in use; then LocationIndex
	// is required.
	Location Point `json:"location,omitempty"`
	// LocationIndex is the row/column in custom matrices that replaces
	// Location.
	LocationIndex *int `json:"location_index,omitempty"`
	// Setup is fixed preparation time in seconds.
	Setup int `json:"setup,omitempty"`
	// Service is time spent on site, in seconds.
	Service int `json:"service,omitempty"`
	// SetupPerType overrides Setup for vehicles of each named type.
	SetupPerType map[string]int `json:"setup_per_type,omitempty"`
	// ServicePerType overrides Service for vehicles of each named type.
	ServicePerType map[string]int `json:"service_per_type,omitempty"`
	// Delivery is the amount unloaded here, matching vehicle Capacity.
	Delivery []int `json:"delivery,omitempty"`
	// Pickup is the amount loaded here, matching vehicle Capacity.
	Pickup []int `json:"pickup,omitempty"`
	// Skills lists capabilities a vehicle must have to serve this job.
	Skills []int `json:"skills,omitempty"`
	// Priority ranks this job from 0 to 100 when not everything fits.
	Priority int `json:"priority,omitempty"`
	// TimeWindows constrains when the job may be served.
	TimeWindows []TimeWindow `json:"time_windows,omitempty"`
	// Description is an opaque label echoed back in the solution.
	Description string `json:"description,omitempty"`
}

// ShipmentStep is one half of a [Shipment].
type ShipmentStep struct {
	ID int `json:"id"`
	// Location is where the step happens, as [longitude, latitude]. Omit
	// when custom matrices cover every profile in use; then LocationIndex
	// is required.
	Location Point `json:"location,omitempty"`
	// LocationIndex is the row/column in custom matrices that replaces
	// Location.
	LocationIndex *int `json:"location_index,omitempty"`
	// Setup is fixed preparation time in seconds.
	Setup int `json:"setup,omitempty"`
	// Service is time spent on site, in seconds.
	Service int `json:"service,omitempty"`
	// SetupPerType overrides Setup for vehicles of each named type.
	SetupPerType map[string]int `json:"setup_per_type,omitempty"`
	// ServicePerType overrides Service for vehicles of each named type.
	ServicePerType map[string]int `json:"service_per_type,omitempty"`
	// TimeWindows constrains when the step may be served.
	TimeWindows []TimeWindow `json:"time_windows,omitempty"`
	// Description is an opaque label echoed back in the solution.
	Description string `json:"description,omitempty"`
}

// Shipment is a pickup and a delivery that must be served in order by the
// same vehicle.
type Shipment struct {
	Pickup   ShipmentStep `json:"pickup"`
	Delivery ShipmentStep `json:"delivery"`
	// Amount is the load carried between the two steps.
	Amount []int `json:"amount,omitempty"`
	// Skills lists capabilities a vehicle must have.
	Skills []int `json:"skills,omitempty"`
	// Priority ranks this shipment from 0 to 100.
	Priority int `json:"priority,omitempty"`
}

// Solution is the result of an optimization run.
type Solution struct {
	// Code is the engine status, 0 on success.
	Code int `json:"code"`
	// Error explains a non-zero Code.
	Error string `json:"error,omitempty"`
	// Summary aggregates the whole solution.
	Summary Summary `json:"summary"`
	// Routes holds one entry per vehicle that was used.
	Routes []*VehicleRoute `json:"routes"`
	// Unassigned lists tasks that could not be served.
	Unassigned []*Unassigned `json:"unassigned"`
}

func (s *Solution) status() *Error {
	if s.Code == 0 {
		return nil
	}
	message := s.Error
	if message == "" {
		message = fmt.Sprintf("optimization engine returned code %d", s.Code)
	}
	return &Error{StatusCode: http.StatusOK, VROOMCode: s.Code, Message: message}
}

// Summary aggregates the cost and time of a whole [Solution].
type Summary struct {
	Cost        int         `json:"cost"`
	Routes      int         `json:"routes"`
	Unassigned  int         `json:"unassigned"`
	Delivery    []int       `json:"delivery,omitempty"`
	Pickup      []int       `json:"pickup,omitempty"`
	Setup       int         `json:"setup"`
	Service     int         `json:"service"`
	Duration    int         `json:"duration"`
	WaitingTime int         `json:"waiting_time"`
	Priority    int         `json:"priority"`
	Distance    int         `json:"distance,omitempty"`
	Violations  []Violation `json:"violations,omitempty"`
}

// VehicleRoute is the itinerary assigned to one vehicle.
type VehicleRoute struct {
	// Vehicle is the ID of the vehicle serving this route.
	Vehicle     int   `json:"vehicle"`
	Cost        int   `json:"cost"`
	Setup       int   `json:"setup"`
	Service     int   `json:"service"`
	Duration    int   `json:"duration"`
	WaitingTime int   `json:"waiting_time"`
	Priority    int   `json:"priority"`
	Distance    int   `json:"distance,omitempty"`
	Delivery    []int `json:"delivery,omitempty"`
	Pickup      []int `json:"pickup,omitempty"`
	// Description is the vehicle description echoed from the request.
	Description string `json:"description,omitempty"`
	// Geometry is an encoded polyline, present only when
	// [OptimizationOptions.Geometry] was set.
	Geometry string       `json:"geometry,omitempty"`
	Steps    []*RouteStep `json:"steps"`
	// Violations holds constraint violations for this route. It is
	// always empty in solving mode, the only mode the hosted API runs.
	Violations []Violation `json:"violations,omitempty"`
}

// RouteStep is a single stop on a [VehicleRoute].
type RouteStep struct {
	// Type is one of "start", "job", "pickup", "delivery", "break" or "end".
	Type string `json:"type"`
	// Location is where the stop happens.
	Location Point `json:"location,omitempty"`
	// LocationIndex is the row/column in custom matrices, when the
	// request used matrices.
	LocationIndex int `json:"location_index,omitempty"`
	// ID is the task ID, for task steps.
	ID int `json:"id,omitempty"`
	// Job is the job ID, for job steps.
	Job         int `json:"job,omitempty"`
	Setup       int `json:"setup"`
	Service     int `json:"service"`
	WaitingTime int `json:"waiting_time"`
	// Arrival is the arrival time in seconds.
	Arrival  int `json:"arrival"`
	Duration int `json:"duration"`
	Distance int `json:"distance,omitempty"`
	// Load is the vehicle load after this stop.
	Load        []int       `json:"load,omitempty"`
	Description string      `json:"description,omitempty"`
	Violations  []Violation `json:"violations,omitempty"`
}

// Unassigned is a task the solver could not fit into any route.
type Unassigned struct {
	ID int `json:"id"`
	// Type is the task type, e.g. "job".
	Type string `json:"type,omitempty"`
	// Location is the task location, when provided in the request.
	Location Point `json:"location,omitempty"`
	// LocationIndex is the row/column in custom matrices, when the
	// request used matrices.
	LocationIndex int    `json:"location_index,omitempty"`
	Description   string `json:"description,omitempty"`
}

// Solve assigns the request's tasks to its vehicles and orders each route.
//
//	solution, err := client.Optimization.Solve(ctx, &justrouting.OptimizationRequest{
//		Vehicles: []justrouting.Vehicle{{ID: 1, Start: depot, End: depot}},
//		Jobs: []justrouting.Job{
//			{ID: 1, Location: stopA},
//			{ID: 2, Location: stopB},
//		},
//	})
//
// All coordinates in a request must lie within a single country; otherwise the
// call fails with an error matching [ErrCrossCountry].
func (s *OptimizationService) Solve(ctx context.Context, req *OptimizationRequest) (*Solution, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}

	out := new(Solution)
	err := s.client.do(ctx, &request{
		method:    http.MethodPost,
		path:      "/optimize",
		body:      req,
		needsAuth: true,
	}, out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *OptimizationRequest) validate() error {
	if r == nil {
		return fmt.Errorf("%w: request must not be nil", ErrInvalidRequest)
	}
	if len(r.Vehicles) == 0 {
		return fmt.Errorf("%w: at least one Vehicle is required", ErrInvalidRequest)
	}
	if len(r.Jobs) == 0 && len(r.Shipments) == 0 {
		return fmt.Errorf("%w: at least one Job or Shipment is required", ErrInvalidRequest)
	}

	for i, v := range r.Vehicles {
		// Start and End are both optional, but must be valid when given.
		if len(v.Start) > 0 {
			if err := v.Start.Validate(); err != nil {
				return fmt.Errorf("Vehicles[%d].Start: %w", i, err)
			}
		}
		if len(v.End) > 0 {
			if err := v.End.Validate(); err != nil {
				return fmt.Errorf("Vehicles[%d].End: %w", i, err)
			}
		}
		// A custom-matrix row counts as an anchor just like coordinates.
		if len(v.Start) == 0 && v.StartIndex == nil && len(v.End) == 0 && v.EndIndex == nil {
			return fmt.Errorf(
				"%w: Vehicles[%d] needs a Start, StartIndex, End or EndIndex",
				ErrInvalidRequest,
				i,
			)
		}
	}

	for i, j := range r.Jobs {
		if len(j.Location) > 0 {
			if err := j.Location.Validate(); err != nil {
				return fmt.Errorf("Jobs[%d].Location: %w", i, err)
			}
		}
		if len(j.Location) == 0 && j.LocationIndex == nil {
			return fmt.Errorf("%w: Jobs[%d] needs a Location or a LocationIndex", ErrInvalidRequest, i)
		}
	}

	for i, s := range r.Shipments {
		if err := validateStepLocation(s.Pickup.Location, s.Pickup.LocationIndex); err != nil {
			return fmt.Errorf("Shipments[%d].Pickup: %w", i, err)
		}
		if err := validateStepLocation(s.Delivery.Location, s.Delivery.LocationIndex); err != nil {
			return fmt.Errorf("Shipments[%d].Delivery: %w", i, err)
		}
	}

	return nil
}

// validateStepLocation checks that a shipment step has a location or, with
// custom matrices, a location index.
func validateStepLocation(location Point, locationIndex *int) error {
	if len(location) > 0 {
		return location.Validate()
	}
	if locationIndex == nil {
		return fmt.Errorf("%w: needs a Location or a LocationIndex", ErrInvalidRequest)
	}
	return nil
}
