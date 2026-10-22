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

	// A short GPS trace of closely-spaced points (about 50m apart) along a
	// road in Singapore. Timestamps are required: without them the match
	// has no time dimension and its confidence collapses.
	match, err := client.MapMatching.Get(ctx, &justrouting.MapMatchingRequest{
		Coordinates: []justrouting.Point{
			{103.836014, 1.367717},
			{103.836231, 1.368218},
			{103.836358, 1.368674},
			{103.836844, 1.369040},
			{103.837076, 1.369354},
			{103.837525, 1.369661},
			{103.838175, 1.369788},
			{103.838594, 1.369975},
		},
		// Real applications should use device-reported UNIX timestamps.
		// The 6-second spacing here approximates driving 50m at 30km/h.
		Timestamps: []int64{
			1694500000, 1694500006, 1694500012, 1694500018,
			1694500024, 1694500030, 1694500036, 1694500042,
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
