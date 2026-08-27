// Command optimization assigns deliveries to a small fleet and prints the
// resulting routes.
//
// Usage:
//
//	JUSTROUTING_API_KEY=<key> go run ./examples/optimization
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	justrouting "github.com/justrouting/go-client"
)

const (
	defaultApiKey = "e7d5c0f6c1da7752488610d21fd80959"
)

func main() {
	apiKey := os.Getenv("JUSTROUTING_API_KEY")
	if apiKey == "" {
		apiKey = defaultApiKey
	}

	client := justrouting.NewClient(apiKey)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	depot := justrouting.Point{103.8198, 1.3521}

	// Two vans, each able to carry three parcels, working a morning shift.
	shift := justrouting.TimeWindow{8 * 3600, 12 * 3600}
	vehicles := []justrouting.Vehicle{
		{ID: 1, Start: depot, End: depot, Capacity: []int{3}, TimeWindow: &shift},
		{ID: 2, Start: depot, End: depot, Capacity: []int{3}, TimeWindow: &shift},
	}

	// Four deliveries, each taking five minutes on site.
	jobs := []justrouting.Job{
		{ID: 1, Location: justrouting.Point{103.8514, 1.2897}, Delivery: []int{1}, Service: 300},
		{ID: 2, Location: justrouting.Point{103.9915, 1.3644}, Delivery: []int{1}, Service: 300},
		{ID: 3, Location: justrouting.Point{103.7649, 1.3329}, Delivery: []int{2}, Service: 300},
		{ID: 4, Location: justrouting.Point{103.8198, 1.4382}, Delivery: []int{1}, Service: 300},
	}

	solution, err := client.Optimization.Solve(ctx, &justrouting.OptimizationRequest{
		Vehicles: vehicles,
		Jobs:     jobs,
	})
	if err != nil {
		if errors.Is(err, justrouting.ErrPlanLimitExceeded) {
			log.Fatal("the fleet or job count exceeds your plan limit")
		}
		log.Fatal(err)
	}

	fmt.Printf("cost %d, %d route(s), %d unassigned\n\n",
		solution.Summary.Cost, solution.Summary.Routes, solution.Summary.Unassigned)

	for _, route := range solution.Routes {
		fmt.Printf("vehicle %d — %.0f min, %.1f km\n",
			route.Vehicle, float64(route.Duration)/60, float64(route.Distance)/1000)
		for _, step := range route.Steps {
			switch step.Type {
			case "start", "end":
				fmt.Printf("  %-8s at %s\n", step.Type, step.Location)
			default:
				fmt.Printf("  %-8s job %d, arrive %.0f min in\n",
					step.Type, step.Job, float64(step.Arrival)/60)
			}
		}
		fmt.Println()
	}

	for _, task := range solution.Unassigned {
		fmt.Printf("unassigned: task %d at %s\n", task.ID, task.Location)
	}
}
