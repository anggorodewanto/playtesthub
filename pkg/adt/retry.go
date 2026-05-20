package adt

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// RetryPolicy carries the knobs for the ADT retry loop. Mirrors
// pkg/ags.RetryPolicy by design so service-layer callers reason about
// one shape, not two (STATUS_M5.md B3: "Retry policy mirrors pkg/ags").
type RetryPolicy struct {
	// MaxAttempts is the total number of attempts including the first
	// call. Standard policy is 4 (1 + 3 retries).
	MaxAttempts int
	// PerAttemptTimeout caps a single ADT round-trip. 30s standard.
	PerAttemptTimeout time.Duration
	// InitialBackoff is the first sleep between retries. Doubles each
	// retry up to MaxBackoff.
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	// Sleep is injectable so tests can swap time.Sleep for a stub.
	Sleep func(d time.Duration)
}

// DefaultRetryPolicy returns the STATUS_M5.md B3 standard policy: 30s
// per attempt, up to 3 retries with exponential backoff (250ms →
// 500ms → 1s, capped at 5s).
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts:       4,
		PerAttemptTimeout: 30 * time.Second,
		InitialBackoff:    250 * time.Millisecond,
		MaxBackoff:        5 * time.Second,
		Sleep:             time.Sleep,
	}
}

// HTTPStatusCarrier is implemented by transport errors that expose the
// upstream HTTP status. Both the future SDK adapter and tests' fake
// errors satisfy it.
type HTTPStatusCarrier interface {
	HTTPStatus() int
}

// Run executes op per the retry policy. The returned error is one of:
//   - nil on success;
//   - ErrRateLimited on HTTP 429 (no retry);
//   - ErrLinkageMissing on HTTP 401 / 403 (no retry — see B6);
//   - *ClientError on any other HTTP 4xx (no retry);
//   - ErrUnavailable when retries on 5xx/timeout are exhausted;
//   - the raw op error for non-HTTP failures (e.g. ctx cancellation).
func (p RetryPolicy) Run(ctx context.Context, opName string, op func(attemptCtx context.Context) error) error {
	if p.MaxAttempts < 1 {
		p.MaxAttempts = 1
	}
	backoff := p.InitialBackoff
	var lastErr error
	for attempt := 1; attempt <= p.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		attemptCtx, cancel := context.WithTimeout(ctx, p.PerAttemptTimeout)
		err := op(attemptCtx)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err

		if !p.shouldRetry(err) {
			return classify(err, opName)
		}
		if attempt == p.MaxAttempts {
			break
		}
		if backoff > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
			if backoff *= 2; backoff > p.MaxBackoff {
				backoff = p.MaxBackoff
			}
		}
	}
	return classify(lastErr, opName)
}

// shouldRetry returns true for HTTP 5xx + context.DeadlineExceeded
// (timeouts) and false for everything else — including all 4xx.
func (p RetryPolicy) shouldRetry(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var carrier HTTPStatusCarrier
	if errors.As(err, &carrier) {
		s := carrier.HTTPStatus()
		return s >= 500 && s <= 599
	}
	return true
}

// classify converts a raw error into the package's sentinel surface.
// 401 / 403 collapse to ErrLinkageMissing because (per B6) the only
// path for a properly-signed AGS service JWT to be rejected is the
// linkage flag being absent on ADT's side.
func classify(err error, opName string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ErrUnavailable
	}
	var carrier HTTPStatusCarrier
	if errors.As(err, &carrier) {
		s := carrier.HTTPStatus()
		switch {
		case s == http.StatusTooManyRequests:
			return ErrRateLimited
		case s == http.StatusUnauthorized, s == http.StatusForbidden:
			return ErrLinkageMissing
		case s >= 400 && s <= 499:
			return &ClientError{StatusCode: s, Op: opName, Message: err.Error()}
		case s >= 500 && s <= 599:
			return ErrUnavailable
		}
	}
	return ErrUnavailable
}
