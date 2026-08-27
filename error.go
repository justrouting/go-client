package justrouting

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Sentinel errors for use with [errors.Is]. They classify an [*Error] by the
// condition it represents rather than by an exact message, which keeps calling
// code stable if the API's wording changes.
var (
	// ErrUnauthorized means the API key is missing, invalid or revoked.
	ErrUnauthorized = errors.New("justrouting: unauthorized")

	// ErrRateLimited means the request was throttled. It matches both
	// per-second throttling and daily quota exhaustion; use
	// ErrQuotaExceeded to distinguish the latter.
	ErrRateLimited = errors.New("justrouting: rate limit exceeded")

	// ErrQuotaExceeded means the plan's daily request quota is used up.
	// Retrying before the quota resets will not help.
	ErrQuotaExceeded = errors.New("justrouting: daily quota exceeded")

	// ErrPlanLimitExceeded means the request is larger than the plan
	// allows — too many matrix coordinates, jobs, or vehicles.
	ErrPlanLimitExceeded = errors.New("justrouting: plan limit exceeded")

	// ErrCrossCountry means the coordinates span more than one country.
	// Every coordinate in a request must fall within a single country.
	ErrCrossCountry = errors.New("justrouting: cross-country request not supported")

	// ErrInvalidCoordinates means a coordinate was malformed, out of range,
	// or could not be parsed by the API.
	ErrInvalidCoordinates = errors.New("justrouting: invalid coordinates")

	// ErrNoRoute means no route exists between the given coordinates.
	ErrNoRoute = errors.New("justrouting: no route found")

	// ErrUpstreamUnavailable means the routing engine behind the API could
	// not be reached. This is usually transient.
	ErrUpstreamUnavailable = errors.New("justrouting: upstream unavailable")

	// ErrInvalidRequest means the request was rejected before being sent
	// because required fields were missing or inconsistent.
	ErrInvalidRequest = errors.New("justrouting: invalid request")
)

// Error describes a failed API call.
//
// It is returned whenever the API responds with a non-2xx status, and also
// when a 2xx response carries an in-body failure code — the routing and
// optimization engines can both report errors alongside HTTP 200.
//
// Match it with [errors.Is] against the package sentinels, or retrieve it with
// [errors.As] when the status code or raw body is needed:
//
//	var apiErr *justrouting.Error
//	if errors.As(err, &apiErr) {
//		log.Printf("status %d: %s", apiErr.StatusCode, apiErr.Message)
//	}
type Error struct {
	// StatusCode is the HTTP status of the response.
	StatusCode int

	// Message is the human-readable reason reported by the API.
	Message string

	// OSRMCode is the routing engine's status code, such as "NoRoute" or
	// "InvalidValue". Empty when the error did not come from the routing
	// engine.
	OSRMCode string

	// VROOMCode is the optimization engine's non-zero status code. Zero
	// when the error did not come from the optimization engine.
	VROOMCode int

	// Body is the raw response body, truncated to a sane limit. Useful for
	// logging responses this package does not model.
	Body []byte
}

// Error implements the error interface.
func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("justrouting: ")
	if e.StatusCode > 0 {
		fmt.Fprintf(&b, "HTTP %d", e.StatusCode)
	} else {
		b.WriteString("request failed")
	}
	switch {
	case e.OSRMCode != "":
		fmt.Fprintf(&b, " (%s)", e.OSRMCode)
	case e.VROOMCode != 0:
		fmt.Fprintf(&b, " (optimization code %d)", e.VROOMCode)
	}
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}
	return b.String()
}

// Is reports whether e matches one of the package sentinel errors, so that
// [errors.Is] classifies API failures without string matching in user code.
func (e *Error) Is(target error) bool {
	msg := strings.ToLower(e.Message)

	switch target {
	case ErrUnauthorized:
		return e.StatusCode == http.StatusUnauthorized
	case ErrQuotaExceeded:
		return e.StatusCode == http.StatusTooManyRequests && strings.Contains(msg, "daily quota")
	case ErrRateLimited:
		return e.StatusCode == http.StatusTooManyRequests
	case ErrUpstreamUnavailable:
		return e.StatusCode == http.StatusBadGateway || strings.Contains(msg, "upstream unavailable")
	case ErrCrossCountry:
		return strings.Contains(msg, "cross-country")
	case ErrPlanLimitExceeded:
		return strings.Contains(msg, "exceeds plan limit") ||
			strings.Contains(msg, "too many jobs") ||
			strings.Contains(msg, "too many vehicles")
	case ErrNoRoute:
		return strings.EqualFold(e.OSRMCode, "NoRoute")
	case ErrInvalidCoordinates:
		return strings.Contains(msg, "cannot parse coordinates") ||
			strings.Contains(msg, "invalid coordinates")
	}
	return false
}

// maxErrorBodyBytes bounds how much of a response body is retained on an
// Error, so that a large or misbehaving upstream cannot balloon a log line.
const maxErrorBodyBytes = 8 << 10

// errorPayload covers every error shape the API can produce:
//
//	{"error": "..."}                       gateway errors
//	{"code": "NoRoute", "message": "..."}  routing engine (code is a string)
//	{"code": 3, "error": "..."}            optimization engine (code is a number)
type errorPayload struct {
	Error   string          `json:"error"`
	Message string          `json:"message"`
	Code    json.RawMessage `json:"code"`
}

// parseError builds an Error from a response. Bodies are classified by shape
// rather than Content-Type: some API handlers emit a JSON body while leaving
// the header set to text/plain.
func parseError(statusCode int, body []byte) *Error {
	e := &Error{StatusCode: statusCode, Body: truncate(body, maxErrorBodyBytes)}

	var payload errorPayload
	if err := json.Unmarshal(bytes.TrimSpace(body), &payload); err == nil {
		switch {
		case payload.Error != "":
			e.Message = payload.Error
		case payload.Message != "":
			e.Message = payload.Message
		}
		applyEngineCode(e, payload.Code)
	}

	if e.Message == "" {
		e.Message = fallbackMessage(statusCode, body)
	}
	return e
}

// applyEngineCode records the engine status code, whose JSON type identifies
// which engine produced it: a string for routing, a number for optimization.
func applyEngineCode(e *Error, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		if !strings.EqualFold(s, "Ok") {
			e.OSRMCode = s
		}
		return
	}
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		e.VROOMCode = n
	}
}

// fallbackMessage produces a message for responses with no recognisable error
// field, such as an HTML error page from an intermediary proxy.
func fallbackMessage(statusCode int, body []byte) string {
	s := strings.TrimSpace(string(body))
	if s != "" && !strings.HasPrefix(s, "{") && !strings.HasPrefix(s, "<") && len(s) <= 200 {
		return s
	}
	if text := http.StatusText(statusCode); text != "" {
		return strings.ToLower(text)
	}
	return "unexpected response"
}

func truncate(b []byte, max int) []byte {
	if len(b) <= max {
		return b
	}
	return b[:max]
}

// apiStatus is implemented by response types carrying an in-body status code.
// Both engines can report a failure alongside HTTP 200, so decoding checks
// this in addition to the HTTP status.
type apiStatus interface {
	// status returns a non-nil Error when the payload reports a failure.
	status() *Error
}

// osrmStatus converts a routing engine status code into an Error, or nil when
// the response was successful.
func osrmStatus(code, message string) *Error {
	if code == "" || strings.EqualFold(code, "Ok") {
		return nil
	}
	if message == "" {
		message = "routing engine returned " + code
	}
	return &Error{StatusCode: http.StatusOK, OSRMCode: code, Message: message}
}
