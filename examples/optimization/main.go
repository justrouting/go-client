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

	// Two vans, each able to carry three parcels, working a morning shift.
	shift := justrouting.TimeWindow{8 * 3600, 12 * 3600}
	vehicles := []justrouting.Vehicle{
		{ID: 1, Profile: "car", Start: justrouting.Point{103.79234106, 1.32463108}, End: justrouting.Point{103.79234106, 1.32463108}, Capacity: []int{500}, TimeWindow: &shift},
		{ID: 2, Profile: "car", Start: justrouting.Point{103.82324228, 1.32408622}, End: justrouting.Point{103.82324228, 1.32408622}, Capacity: []int{1000}, TimeWindow: &shift},
	}

	// Four deliveries, each taking five minutes on site.
	jobs := []justrouting.Job{
		{ID: 1, Location: justrouting.Point{103.79751693, 1.31035001}, Pickup: []int{20}, Service: 300},
		{ID: 2, Location: justrouting.Point{103.78432387, 1.31490148}, Pickup: []int{15}, Service: 300},
		{ID: 3, Location: justrouting.Point{103.79763397, 1.31980519}, Pickup: []int{95}, Service: 300},
		{ID: 4, Location: justrouting.Point{103.81234512, 1.31824846}, Pickup: []int{60}, Service: 300},
		{ID: 5, Location: justrouting.Point{103.82152987, 1.30984208}, Pickup: []int{35}, Service: 300},
		{ID: 6, Location: justrouting.Point{103.83701939, 1.32143977}, Delivery: []int{75}, Service: 300},
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
