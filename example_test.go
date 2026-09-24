package justrouting_test

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	justrouting "github.com/justrouting/go-client"
)

// This is the package quickstart. It compiles as part of the test suite, so
// the documented usage cannot drift from the API.
func Example() {
	client := justrouting.NewClient(
		"YOUR_API_KEY",
		justrouting.WithHTTPClient(&http.Client{
			Timeout: 10 * time.Second,
		}),
	)

	route, err := client.Routes.Get(
		context.Background(),
		&justrouting.RouteRequest{
			Origin:      []float64{103.8198, 1.3521},
			Destination: []float64{103.9915, 1.3644},
		},
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf(
		"Distance: %.1f km\n",
		route.Distance/1000,
	)
}

func ExampleRoutesService_Get() {
	client := justrouting.NewClient("YOUR_API_KEY")

	route, err := client.Routes.Get(context.Background(), &justrouting.RouteRequest{
		Origin:      justrouting.Point{103.8198, 1.3521},
		Destination: justrouting.Point{103.9915, 1.3644},
		Waypoints:   []justrouting.Point{{103.8514, 1.2897}},
		Overview:    "full",
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%.1f km in %.0f min\n", route.Distance/1000, route.Duration/60)

	if polyline, err := route.Geometry.Polyline(); err == nil {
		fmt.Println("geometry:", polyline)
	}
}

func ExampleMatrixService_Get() {
	client := justrouting.NewClient("YOUR_API_KEY")

	depot := justrouting.Point{103.8198, 1.3521}
	stops := []justrouting.Point{
		{103.8514, 1.2897},
		{103.9915, 1.3644},
	}

	m, err := client.Matrix.Get(context.Background(), &justrouting.MatrixRequest{
		Coordinates: append([]justrouting.Point{depot}, stops...),
		// Only the depot-to-stop row is needed, which is cheaper than
		// computing the full square matrix.
		Sources:      []int{0},
		Destinations: []int{1, 2},
	})
	if err != nil {
		log.Fatal(err)
	}

	for j := range stops {
		if seconds, ok := m.Duration(0, j); ok {
			fmt.Printf("stop %d: %.0f min\n", j+1, seconds/60)
		} else {
			fmt.Printf("stop %d: unreachable\n", j+1)
		}
	}
}

func ExampleMapMatchingService_Get() {
	client := justrouting.NewClient("YOUR_API_KEY")

	// A noisy GPS trace, in chronological order.
	trace := []justrouting.Point{
		{103.8198, 1.3521},
		{103.8514, 1.2897},
		{103.9915, 1.3644},
	}

	match, err := client.MapMatching.Get(context.Background(), &justrouting.MapMatchingRequest{
		Coordinates: trace,
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%.0f%% confidence, %.1f km\n", match.Confidence*100, match.Distance/1000)
}

func ExampleTripService_Get() {
	client := justrouting.NewClient("YOUR_API_KEY")

	depot := justrouting.Point{103.8198, 1.3521}
	trip, err := client.Trip.Get(context.Background(), &justrouting.TripRequest{
		Coordinates: []justrouting.Point{
			depot,
			{103.8514, 1.2897},
			{103.9915, 1.3644},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%.1f km in %.0f min\n", trip.Distance/1000, trip.Duration/60)
}

func ExampleNearestService_Get() {
	client := justrouting.NewClient("YOUR_API_KEY")

	wp, err := client.Nearest.Get(context.Background(), &justrouting.NearestRequest{
		Coordinate: justrouting.Point{103.8198, 1.3521},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%s, %.0f m away\n", wp.Name, wp.Distance)
}

func ExampleGeocodeService_Search() {
	client := justrouting.NewClient("YOUR_API_KEY")

	results, err := client.Geocode.Search(context.Background(), &justrouting.GeocodeRequest{
		Text: "Marina Bay Sands, Singapore",
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, r := range results.Results {
		fmt.Printf("%s (%.4f, %.4f)\n", r.Formatted, r.Lon, r.Lat)
	}
}

func ExampleOptimizationService_Solve() {
	client := justrouting.NewClient("YOUR_API_KEY")

	depot := justrouting.Point{103.8198, 1.3521}

	solution, err := client.Optimization.Solve(context.Background(), &justrouting.OptimizationRequest{
		Vehicles: []justrouting.Vehicle{
			{ID: 1, Start: depot, End: depot, Capacity: []int{4}},
		},
		Jobs: []justrouting.Job{
			{ID: 1, Location: justrouting.Point{103.8514, 1.2897}, Delivery: []int{1}, Service: 300},
			{ID: 2, Location: justrouting.Point{103.9915, 1.3644}, Delivery: []int{2}, Service: 300},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	for _, route := range solution.Routes {
		fmt.Printf("vehicle %d serves %d stops\n", route.Vehicle, len(route.Steps))
	}
	fmt.Printf("%d task(s) unassigned\n", len(solution.Unassigned))
}

// Failures are classified with sentinel errors, so callers never match on
// message text.
func ExampleError() {
	client := justrouting.NewClient("YOUR_API_KEY")

	_, err := client.Routes.Get(context.Background(), &justrouting.RouteRequest{
		Origin:      justrouting.Point{103.8198, 1.3521},
		Destination: justrouting.Point{101.6869, 3.1390},
	})

	switch {
	case err == nil:
		fmt.Println("ok")
	case errors.Is(err, justrouting.ErrCrossCountry):
		fmt.Println("all coordinates must be in the same country")
	case errors.Is(err, justrouting.ErrQuotaExceeded):
		fmt.Println("daily quota used up; retrying will not help")
	case errors.Is(err, justrouting.ErrRateLimited):
		fmt.Println("throttled; the client already retried")
	case errors.Is(err, justrouting.ErrNoRoute):
		fmt.Println("no road connects these points")
	default:
		// Reach for the concrete type when the status or raw body matters.
		var apiErr *justrouting.Error
		if errors.As(err, &apiErr) {
			fmt.Printf("HTTP %d: %s\n", apiErr.StatusCode, apiErr.Message)
		}
	}
}
