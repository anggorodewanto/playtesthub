package adt

import (
	"errors"
	"fmt"
)

// Sentinel error values let the service layer pattern-match by class
// rather than by HTTP status. Callers map them to gRPC codes per
// docs/errors.md (rate-limited → ResourceExhausted; unavailable →
// Unavailable; client 401/403 → FailedPrecondition "adt linkage no
// longer exists or service token rejected, re-link required" per
// STATUS_M5.md B6; other 4xx → Internal/InvalidArgument upstream).
var (
	// ErrRateLimited maps to gRPC ResourceExhausted (mirrors
	// pkg/ags.ErrRateLimited). Returned without retry — ADT 429s are
	// admin-actionable.
	ErrRateLimited = errors.New("adt: upstream rate limited (HTTP 429)")

	// ErrUnavailable maps to gRPC Unavailable. Returned only after
	// the retry budget is exhausted on 5xx / timeout.
	ErrUnavailable = errors.New("adt: upstream unavailable after retry exhausted")

	// ErrLinkageMissing maps to gRPC FailedPrecondition "adt linkage
	// no longer exists or service token rejected, re-link required"
	// (docs/errors.md row authored in B1). Surfaced when ADT returns
	// 401/403 on a call that carries a valid service JWT — the
	// linkage flag is missing on ADT's side. See STATUS_M5.md B6.
	ErrLinkageMissing = errors.New("adt: linkage missing or service token rejected")
)

// ClientError is the typed wrapper for HTTP 4xx (other than 429 /
// 401 / 403) returned by ADT. Mirrors pkg/ags.ClientError so service
// callers can use the same errors.As pattern.
type ClientError struct {
	StatusCode int
	Op         string
	Message    string
}

func (e *ClientError) Error() string {
	if e.Op == "" {
		return fmt.Sprintf("adt: client error %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("adt: %s: client error %d: %s", e.Op, e.StatusCode, e.Message)
}

// IsRateLimited returns true when err (or any error in its chain) is
// ErrRateLimited.
func IsRateLimited(err error) bool { return errors.Is(err, ErrRateLimited) }

// IsUnavailable returns true when err (or any error in its chain) is
// ErrUnavailable.
func IsUnavailable(err error) bool { return errors.Is(err, ErrUnavailable) }

// IsLinkageMissing returns true when err (or any error in its chain)
// is ErrLinkageMissing.
func IsLinkageMissing(err error) bool { return errors.Is(err, ErrLinkageMissing) }

// IsClientError returns true when err (or any error in its chain) is
// a *ClientError.
func IsClientError(err error) bool {
	var ce *ClientError
	return errors.As(err, &ce)
}
