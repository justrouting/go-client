// Command nearest finds the road segment closest to a coordinate.
//
// Usage:
//
//	JUSTROUTING_API_KEY=<key> go run ./examples/nearest
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

	wp, err := client.Nearest.Get(ctx, &justrouting.NearestRequest{
		Coordinate: []float64{103.8198, 1.3521},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Street:   %s\n", wp.Name)
	fmt.Printf("Location: %.6f, %.6f\n", wp.Location.Lon(), wp.Location.Lat())
	fmt.Printf("Distance: %.0f m\n", wp.Distance)
	if len(wp.Nodes) > 0 {
		fmt.Printf("Nodes:    %v\n", wp.Nodes)
	}
}
