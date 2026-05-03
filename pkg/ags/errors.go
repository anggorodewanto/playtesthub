package ags

import (
	"errors"
	"fmt"
)

// Sentinel error values let the service layer pattern-match by class
// rather than by HTTP status. Callers map them to gRPC codes per
// docs/errors.md (rate-limited → ResourceExhausted; unavailable →
// Unavailable; client → Internal/InvalidArgument upstream).
var (
	// ErrRateLimited maps to gRPC ResourceExhausted (errors.md row
	// "AGS-backed RPCs ... HTTP 429"). Returned without retry: AGS
	// 429s are admin-actionable, not transient backoff candidates.
	ErrRateLimited = errors.New("ags: upstream rate limited (HTTP 429)")

	// ErrUnavailable maps to gRPC Unavailable (errors.md row
	// "AGS-backed RPCs ... HTTP 5xx / timeout after 3 retries").
	// Returned only after the retry budget is exhausted.
	ErrUnavailable = errors.New("ags: upstream unavailable after retry exhausted")
)

// ClientError is the typed wrapper for HTTP 4xx (other than 429)
// returned by AGS. The status code lets the caller decide whether the
// 4xx is admin-actionable (e.g. 400 InvalidArgument from a malformed
// item spec) or programmer error.
type ClientError struct {
	StatusCode int
	Op         string
	Message    string
}

func (e *ClientError) Error() string {
	if e.Op == "" {
		return fmt.Sprintf("ags: client error %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("ags: %s: client error %d: %s", e.Op, e.StatusCode, e.Message)
}

// IsRateLimited returns true when err (or any error in its chain) is
// ErrRateLimited. Convenience for service-layer mappers.
func IsRateLimited(err error) bool { return errors.Is(err, ErrRateLimited) }

// IsUnavailable returns true when err (or any error in its chain) is
// ErrUnavailable.
func IsUnavailable(err error) bool { return errors.Is(err, ErrUnavailable) }

// IsClientError returns true when err (or any error in its chain) is
// a *ClientError.
func IsClientError(err error) bool {
	var ce *ClientError
	return errors.As(err, &ce)
}
