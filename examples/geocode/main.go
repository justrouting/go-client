// Command geocode searches for an address and prints the matches.
//
// Usage:
//
//	JUSTROUTING_API_KEY=<key> go run ./examples/geocode
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

	results, err := client.Geocode.Search(ctx, &justrouting.GeocodeRequest{
		Text:    "Marina Bay Sands, Singapore",
		Limit:   5,
		Filters: []string{"countrycode:sg"},
	})
	if err != nil {
		log.Fatal(err)
	}

	for i, r := range results.Results {
		loc := r.Location()
		fmt.Printf("#%d %s\n", i+1, r.Formatted)
		fmt.Printf("  Location:   %.6f, %.6f\n", loc.Lon(), loc.Lat())
		fmt.Printf("  Type:       %s\n", r.ResultType)
		fmt.Printf("  Place ID:   %s\n", r.PlaceID)
	}
}
