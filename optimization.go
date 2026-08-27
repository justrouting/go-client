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

// Vehicle is one member of the fleet.
type Vehicle struct {
	// ID identifies the vehicle in the solution. It must be unique.
	ID int `json:"id"`
	// Profile selects the routing profile for this vehicle.
	Profile string `json:"profile,omitempty"`
	// Start is where the vehicle begins, as [longitude, latitude]. Omit
	// for a vehicle that may start anywhere.
	Start Point `json:"start,omitempty"`
	// End is where the vehicle must finish. Omit to end anywhere.
	End Point `json:"end,omitempty"`
	// Capacity is a multidimensional capacity vector. Its length must
	// match the Delivery and Pickup vectors on tasks.
	Capacity []int `json:"capacity,omitempty"`
	// Skills lists capabilities this vehicle provides.
	Skills []int `json:"skills,omitempty"`
	// TimeWindow bounds when the vehicle is available.
	TimeWindow *TimeWindow `json:"time_window,omitempty"`
	// MaxTasks caps how many tasks the vehicle may be assigned.
	MaxTasks int `json:"max_tasks,omitempty"`
	// Description is an opaque label echoed back in the solution.
	Description string `json:"description,omitempty"`
}

// Job is a task carried out at a single location.
type Job struct {
	// ID identifies the job in the solution. It must be unique.
	ID int `json:"id"`
	// Location is where the job happens, as [longitude, latitude].
	Location Point `json:"location"`
	// Setup is fixed preparation time in seconds.
	Setup int `json:"setup,omitempty"`
	// Service is time spent on site, in seconds.
	Service int `json:"service,omitempty"`
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
	ID          int          `json:"id"`
	Location    Point        `json:"location"`
	Setup       int          `json:"setup,omitempty"`
	Service     int          `json:"service,omitempty"`
	TimeWindows []TimeWindow `json:"time_windows,omitempty"`
	Description string       `json:"description,omitempty"`
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
	Cost        int   `json:"cost"`
	Routes      int   `json:"routes"`
	Unassigned  int   `json:"unassigned"`
	Delivery    []int `json:"delivery,omitempty"`
	Pickup      []int `json:"pickup,omitempty"`
	Setup       int   `json:"setup"`
	Service     int   `json:"service"`
	Duration    int   `json:"duration"`
	WaitingTime int   `json:"waiting_time"`
	Priority    int   `json:"priority"`
	Distance    int   `json:"distance,omitempty"`
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
	// Geometry is an encoded polyline, present only when
	// [OptimizationOptions.Geometry] was set.
	Geometry string `json:"geometry,omitempty"`
	// Steps are the stops in visiting order.
	Steps []*RouteStep `json:"steps"`
}

// RouteStep is a single stop on a [VehicleRoute].
type RouteStep struct {
	// Type is one of "start", "job", "pickup", "delivery", "break" or "end".
	Type string `json:"type"`
	// Location is where the stop happens.
	Location Point `json:"location,omitempty"`
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
	Load        []int  `json:"load,omitempty"`
	Description string `json:"description,omitempty"`
}

// Unassigned is a task the solver could not fit into any route.
type Unassigned struct {
	ID          int    `json:"id"`
	Type        string `json:"type,omitempty"`
	Location    Point  `json:"location,omitempty"`
	Description string `json:"description,omitempty"`
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
		path:      "/vroom",
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
		if len(v.Start) == 0 && len(v.End) == 0 {
			return fmt.Errorf("%w: Vehicles[%d] needs a Start or an End", ErrInvalidRequest, i)
		}
	}

	for i, j := range r.Jobs {
		if err := j.Location.Validate(); err != nil {
			return fmt.Errorf("Jobs[%d].Location: %w", i, err)
		}
	}

	for i, s := range r.Shipments {
		if err := s.Pickup.Location.Validate(); err != nil {
			return fmt.Errorf("Shipments[%d].Pickup.Location: %w", i, err)
		}
		if err := s.Delivery.Location.Validate(); err != nil {
			return fmt.Errorf("Shipments[%d].Delivery.Location: %w", i, err)
		}
	}

	return nil
}
