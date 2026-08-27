// Command matrix computes travel times from a depot to several stops.
//
// Usage:
//
//	JUSTROUTING_API_KEY=<key> go run ./examples/matrix
package main

import (
	"context"
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	depot := justrouting.Point{103.8198, 1.3521} // Marina Bay
	stops := []justrouting.Point{
		{103.8514, 1.2897}, // Marina Barrage
		{103.9915, 1.3644}, // Changi Airport
		{103.7649, 1.3329}, // Jurong East
	}

	coordinates := append([]justrouting.Point{depot}, stops...)

	// Restricting sources to the depot computes one row instead of the
	// full square matrix, which counts against a much smaller plan limit.
	destinations := make([]int, len(stops))
	for i := range stops {
		destinations[i] = i + 1
	}

	m, err := client.Matrix.Get(ctx, &justrouting.MatrixRequest{
		Coordinates:  coordinates,
		Sources:      []int{0},
		Destinations: destinations,
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("From the depot:")
	for j := range stops {
		seconds, ok := m.Duration(0, j)
		if !ok {
			fmt.Printf("  stop %d: unreachable\n", j+1)
			continue
		}
		metres, _ := m.Distance(0, j)
		fmt.Printf("  stop %d: %5.1f min  %6.1f km\n", j+1, seconds/60, metres/1000)
	}
}
