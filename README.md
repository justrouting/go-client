# JustRouting Go Client

Official Go client for the [JustRouting](https://justrouting.tech) API — routing, distance matrices, map matching, trips, nearest-road lookup, vehicle routing optimization, and geocoding across Southeast Asia.

No dependencies outside the standard library.

## Install

```shell
go get github.com/justrouting/go-client
```

```go
import justrouting "github.com/justrouting/go-client"
```

## Quickstart

```go
package main

import (
    "context"
    "fmt"
    "log"
    "net/http"
    "time"

    "github.com/justrouting/go-client"
)

func main() {
    client := justrouting.NewClient(
        "YOUR_API_KEY",
        justrouting.WithHTTPClient(&http.Client{
            Timeout: 10 * time.Second,
        }),
    )

    route, err := client.Routes.Get(
        context.Background(),
        &justrouting.RouteRequest{
            Origin:      []float64{103.708362, 1.357371},
            Destination: []float64{103.984748, 1.352212},
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
```

> The coordinates above are Singapore and Kuala Lumpur, which span two countries. See [Coordinates must share a country](#coordinates-must-share-a-country) — the runnable examples use same-country pairs.

## Services

A `Client` exposes eight services.

### Routes

`Routes.Get` returns the best route. `Routes.GetAll` additionally returns alternatives and the snapped input waypoints.

```go
route, err := client.Routes.Get(ctx, &justrouting.RouteRequest{
    Origin:      justrouting.Point{103.8198, 1.3521},
    Destination: justrouting.Point{103.9915, 1.3644},
    Waypoints:   []justrouting.Point{{103.8514, 1.2897}}, // stops in order
    Overview:    "full",                                   // full geometry
    Steps:       true,                                     // turn-by-turn
})

fmt.Println(route.Distance) // metres
fmt.Println(route.Duration) // seconds
```

`Route.Geometry` holds whichever encoding you asked for:

```go
polyline, err := route.Geometry.Polyline()  // default, and "polyline6"
line, err := route.Geometry.GeoJSON()       // when Geometries: "geojson"
```

### Matrix

Travel time and distance between many points at once.

```go
m, err := client.Matrix.Get(ctx, &justrouting.MatrixRequest{
    Coordinates:  []justrouting.Point{depot, stopA, stopB},
    Sources:      []int{0},    // only the depot row; cheaper than N×N
    Destinations: []int{1, 2},
})

if seconds, ok := m.Duration(0, 1); ok {
    fmt.Printf("depot → stopA: %.0f min\n", seconds/60)
}
```

The accessors return `false` for unreachable pairs. The API reports those as `null`, which is deliberately kept distinct from a genuine zero — that is why `Durations` and `Distances` hold `*float64`.

### Map Matching

Snap a noisy GPS trace onto the road network and get the route that was driven.

```go
match, err := client.MapMatching.Get(ctx, &justrouting.MapMatchingRequest{
    Coordinates: trace, // GPS points in chronological order, at least 2
})
fmt.Printf("%.0f%% confidence\n", match.Confidence*100)
fmt.Println(match.Distance) // metres, via the embedded Route
```

`Match` embeds `Route`, so every route field (`Distance`, `Duration`, `Geometry`, `Legs`, …) is promoted, plus the engine's `Confidence` (0–1). `MapMatching.GetAll` additionally returns `Tracepoints` — the input coordinates snapped to the road network, aligned with your input; an entry is `nil` when the engine could not match that point. Useful options: `Timestamps` (UNIX seconds per point), `Radiuses` (max snap distance, one value per point — the default is only a few metres, so noisy GPS points usually need it), `Gaps` (`"split"`/`"ignore"`), `Tidy`, `Waypoints` (indices to use as waypoints), and `Snapping` (`"default"`/`"any"`).

### Trip

Visit a set of points in the fastest possible order (a travelling-salesman heuristic).

```go
trip, err := client.Trip.Get(ctx, &justrouting.TripRequest{
    Coordinates: []justrouting.Point{depot, stopA, stopB},
})
fmt.Println(trip.Distance) // metres
```

By default the trip returns to its starting point; pass `Roundtrip` (a `bool` pointer) to change that, and `Source`/`Destination` (`"any"`, `"first"`/`"last"`) to pin the ends. `Trip.GetAll` also returns `Waypoints` in the order the trip visits them.

### Nearest

Find the road segment closest to a coordinate.

```go
wp, err := client.Nearest.Get(ctx, &justrouting.NearestRequest{
    Coordinate: []float64{103.8198, 1.3521},
})
fmt.Printf("%s, %.0f m away\n", wp.Name, wp.Distance)
```

Pass `Number` to get the second-, third-, … nearest segments; `Nearest.GetAll` returns all of them. The returned `Waypoint` includes the OSM `Nodes` of the matched segment.

### Geocode

`Geocode.Search` converts an address into coordinates, by free text or by structured fields (exactly one of the two):

```go
results, err := client.Geocode.Search(ctx, &justrouting.GeocodeRequest{
    Text: "Marina Bay Sands, Singapore",
})
for _, r := range results.Results {
    fmt.Println(r.Formatted, r.Location()) // address text, [lon, lat]
}
```

```go
results, err := client.Geocode.Search(ctx, &justrouting.GeocodeRequest{
    Structured: &justrouting.StructuredQuery{
        Housenumber: "10",
        Street:      "Bayfront Avenue",
        City:        "Singapore",
    },
})
```

Results are ordered best first; an empty `Results` list simply means nothing matched. The client always requests `format=json`, regardless of the API's default response format. Use `Filters` (repeatable, e.g. `countrycode:sg`) to restrict results and `Bias` (e.g. `proximity:103.8,1.3`) to prefer places near a point. `GeocodeResult.Location()` returns a `Point` in the usual `[longitude, latitude]` order, ready to feed into `Routes`, `Matrix`, or `Optimization`. Errors from the geocoding upstream are classified by HTTP status like any other failure (`401` → `ErrUnauthorized`, `429` → `ErrRateLimited`, `502` → `ErrUpstreamUnavailable`).

### Optimization

Assign tasks to a fleet and order each vehicle's stops.

```go
solution, err := client.Optimization.Solve(ctx, &justrouting.OptimizationRequest{
    Vehicles: []justrouting.Vehicle{
        {ID: 1, Start: depot, End: depot, Capacity: []int{4}},
    },
    Jobs: []justrouting.Job{
        {ID: 1, Location: stopA, Delivery: []int{1}, Service: 300},
        {ID: 2, Location: stopB, Delivery: []int{2}, Service: 300},
    },
})

for _, route := range solution.Routes {
    fmt.Printf("vehicle %d: %d stops\n", route.Vehicle, len(route.Steps))
}
fmt.Println(len(solution.Unassigned), "task(s) could not be served")
```

Use `Shipments` instead of `Jobs` for pickup-and-delivery pairs that must be served in order by the same vehicle.

### Health

The only call that works without an API key, which makes it a useful connectivity check.

```go
health, err := client.Health.Get(ctx)
fmt.Println(health.OK(), health.Upstreams)
```

## Error handling

Every failure is an `*Error`. Classify it with `errors.Is` against the package sentinels rather than matching on message text:

```go
switch {
case errors.Is(err, justrouting.ErrQuotaExceeded):
    // daily allowance used up — retrying will not help
case errors.Is(err, justrouting.ErrRateLimited):
    // throttled; the client already retried
case errors.Is(err, justrouting.ErrCrossCountry):
    // coordinates span more than one country
case errors.Is(err, justrouting.ErrNoRoute):
    // no road connects these points
}
```

| Sentinel | Meaning |
| --- | --- |
| `ErrUnauthorized` | API key missing, invalid, or revoked |
| `ErrRateLimited` | Throttled (per-second limit or daily quota) |
| `ErrQuotaExceeded` | Daily quota exhausted; retrying will not help |
| `ErrPlanLimitExceeded` | Too many matrix coordinates, jobs, or vehicles |
| `ErrCrossCountry` | Coordinates span more than one country |
| `ErrInvalidCoordinates` | Coordinate malformed or out of range |
| `ErrNoRoute` | No route exists between the points |
| `ErrUpstreamUnavailable` | Routing engine unreachable; usually transient |
| `ErrInvalidRequest` | Rejected locally before any request was sent |

Reach for the concrete type when you need the status code or raw body:

```go
var apiErr *justrouting.Error
if errors.As(err, &apiErr) {
    log.Printf("HTTP %d: %s\n%s", apiErr.StatusCode, apiErr.Message, apiErr.Body)
}
```

## Configuration

| Option | Default | Purpose |
| --- | --- | --- |
| `WithHTTPClient` | 30s timeout | Custom transport, timeouts, instrumentation |
| `WithBaseURL` | `https://api.justrouting.tech` | Target a local or staging server |
| `WithUserAgent` | `justrouting-go/<version>` | Identify your application |
| `WithMaxRetries` | `2` | Retry budget on top of the initial attempt |
| `WithBackoff` | 500ms → 8s, jittered | Replace the retry delay schedule |

`NewClient` never returns an error. If an option is invalid, the failure is retained and returned by the first API call, so a misconfigured client fails loudly at the point of use instead of silently falling back to a default.

### Retries

Rate limits (429), server errors (5xx), and transport failures are retried with exponential backoff and jitter; a `Retry-After` header takes precedence when present. Other 4xx responses are returned immediately — they would fail identically on a retry and would still consume quota.

Retries are bounded by the context, not the HTTP client's timeout. A timeout on your `http.Client` applies to each individual attempt; use a context deadline to bound the whole call:

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
```

## Things to know

### Coordinates are `[longitude, latitude]`

This is the GeoJSON order, and the reverse of the "lat, lng" used by most map UIs. Swapped coordinates are usually caught locally — a longitude in the latitude slot fails the `[-90, 90]` check before a request is sent — but a swap that stays in range will silently route somewhere unexpected.

`Point` is defined over `[]float64`, so both of these work:

```go
Origin: []float64{103.8198, 1.3521}
Origin: justrouting.Point{103.8198, 1.3521}
```

### Coordinates must share a country

Every coordinate in a single request must fall within one country; the API routes each request to a per-country engine. A Singapore → Kuala Lumpur request fails with `ErrCrossCountry`.

Supported countries: Brunei, Cambodia, Indonesia, Laos, Malaysia, Myanmar, the Philippines, Singapore, Thailand, and Vietnam.

### Plan limits

| | Free | Hobby |
| --- | --- | --- |
| Requests per day | 100 | 10,000 |
| Requests per second | 5 | 10 |
| Matrix coordinates | 100 | 500 |
| Jobs per optimization | 100 | 1,000 |
| Vehicles per optimization | 10 | 50 |

Exceeding a size limit returns `ErrPlanLimitExceeded`; exhausting the daily allowance returns `ErrQuotaExceeded`.

## Examples

Runnable programs live in [`examples/`](./examples):

```shell
export JUSTROUTING_API_KEY=<your key>
go run ./examples/route
go run ./examples/matrix
go run ./examples/matching
go run ./examples/trip
go run ./examples/nearest
go run ./examples/geocode
go run ./examples/optimization
```

## Development

```shell
go build ./...
go vet ./...
go test -race -cover ./...
```

The default suite runs entirely against `httptest` servers — no network access and no API key.

Integration tests hit a live API and are behind a build tag, so they never run by accident:

```shell
go test -tags=integration ./...                          # health and auth only
JUSTROUTING_API_KEY=<key> go test -tags=integration ./... # full coverage
JUSTROUTING_BASE_URL=http://localhost:8080 go test -tags=integration ./...
```

## License

[MIT](./LICENSE)
