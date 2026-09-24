// Command trip finds the fastest order to visit a set of points.
//
// Usage:
//
//	JUSTROUTING_API_KEY=<key> go run ./examples/trip
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
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

	client := justrouting.NewClient(
		apiKey,
		justrouting.WithHTTPClient(&http.Client{
			Timeout: 10 * time.Second,
		}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Three points around Singapore, all in one country. The engine returns
	// the fastest order to visit them, ending where the trip began.
	resp, err := client.Trip.GetAll(ctx, &justrouting.TripRequest{
		Coordinates: []justrouting.Point{
			{103.8198, 1.3521}, // Marina Bay
			{103.8514, 1.2897}, // Sentosa
			{103.9915, 1.3644}, // Changi
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	trip := resp.Trips[0]
	fmt.Printf("Distance: %.1f km\n", trip.Distance/1000)
	fmt.Printf("Duration: %.0f min\n", trip.Duration/60)
	fmt.Printf("Legs:     %d\n", len(trip.Legs))

	// Waypoints are returned in the order the trip visits them.
	fmt.Print("Order:    ")
	for i, wp := range resp.Waypoints {
		if i > 0 {
			fmt.Print(" -> ")
		}
		fmt.Print(wp.Name)
	}
	fmt.Println()
}
