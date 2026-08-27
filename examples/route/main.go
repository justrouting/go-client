// Command route computes a route between two points and prints its distance.
//
// Usage:
//
//	JUSTROUTING_API_KEY=<key> go run ./examples/route
package main

import (
	"context"
	"errors"
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

	// Marina Bay to Changi Airport. Every coordinate in a request must lie
	// within one country, so both points are in Singapore.
	route, err := client.Routes.Get(ctx, &justrouting.RouteRequest{
		Origin:      []float64{103.8198, 1.3521},
		Destination: []float64{103.9915, 1.3644},
		Overview:    "full",
	})
	if err != nil {
		if errors.Is(err, justrouting.ErrCrossCountry) {
			log.Fatal("origin and destination must be in the same country")
		}
		log.Fatal(err)
	}

	fmt.Printf("Distance: %.1f km\n", route.Distance/1000)
	fmt.Printf("Duration: %.0f min\n", route.Duration/60)
	fmt.Printf("Legs:     %d\n", len(route.Legs))

	if polyline, err := route.Geometry.Polyline(); err == nil {
		fmt.Printf("Geometry(Polyline): %v\n", polyline)
	}
}
