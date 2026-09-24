// Command matching snaps a noisy GPS trace onto the road network.
//
// Usage:
//
//	JUSTROUTING_API_KEY=<key> go run ./examples/matching
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

	// A GPS trace along the East Coast Parkway, from Marina Bay towards
	// Changi — points a few kilometres apart, with a little GPS noise.
	match, err := client.MapMatching.Get(ctx, &justrouting.MapMatchingRequest{
		Coordinates: []justrouting.Point{
			{103.823679, 1.355111},
			{103.831810, 1.355074},
			{103.839222, 1.346059},
			{103.856595, 1.343471},
			{103.864702, 1.329605},
			{103.887874, 1.322419},
			{103.928786, 1.335564},
			{103.962769, 1.350345},
			{103.983033, 1.344782},
			{103.990312, 1.361474},
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Confidence: %.0f%%\n", match.Confidence*100)
	fmt.Printf("Distance:   %.1f km\n", match.Distance/1000)
	fmt.Printf("Duration:   %.0f min\n", match.Duration/60)
	fmt.Printf("Legs:       %d\n", len(match.Legs))
}
