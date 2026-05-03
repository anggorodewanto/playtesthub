package ags

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// RetryPolicy carries the knobs for the standard AGS retry loop
// (docs/ags-failure-modes.md "Retry policy"). The initial-create code
// path bypasses this entirely with WithoutRetries.
type RetryPolicy struct {
	// MaxAttempts is the total number of attempts, including the
	// first call. Standard policy is 4 (1 + 3 retries).
	MaxAttempts int
	// PerAttemptTimeout caps a single AGS round-trip. 30s standard.
	PerAttemptTimeout time.Duration
	// InitialBackoff is the first sleep between retries. Doubles each
	// retry up to MaxBackoff.
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	// Sleep is injectable so tests can swap time.Sleep for a stub
	// that records call counts and never actually sleeps.
	Sleep func(d time.Duration)
}

// DefaultRetryPolicy returns the PRD §4.6 standard policy: 30s per
// attempt, up to 3 retries with exponential backoff (250ms → 500ms →
// 1s, capped at 5s).
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts:       4,
		PerAttemptTimeout: 30 * time.Second,
		InitialBackoff:    250 * time.Millisecond,
		MaxBackoff:        5 * time.Second,
		Sleep:             time.Sleep,
	}
}

// WithoutRetries returns the initial-create exception policy: 300s per
// attempt, no retries, all-or-nothing (PRD §4.6 / docs/ags-failure
// -modes.md "Exception").
func WithoutRetries() RetryPolicy {
	return RetryPolicy{
		MaxAttempts:       1,
		PerAttemptTimeout: 300 * time.Second,
		InitialBackoff:    0,
		MaxBackoff:        0,
		Sleep:             time.Sleep,
	}
}

// HTTPStatusCarrier is implemented by transport errors that expose
// the upstream HTTP status. Both the SDK adapter and tests' fake
// errors satisfy it.
type HTTPStatusCarrier interface {
	HTTPStatus() int
}

// Run executes op per the retry policy. The returned error is one of:
//   - nil on success;
//   - ErrRateLimited when op surfaced an HTTP 429 (no retry);
//   - *ClientError for any other HTTP 4xx (no retry);
//   - ErrUnavailable when retries on 5xx/timeout were exhausted;
//   - the raw op error for non-HTTP failures (e.g. ctx cancellation).
//
// op should call back into the supplied attemptCtx so the per-attempt
// timeout takes effect.
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

		// Sleep with cancel-awareness: if ctx is cancelled mid-backoff
		// we surface the cancellation rather than waste the budget.
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

// shouldRetry returns true for HTTP 5xx / context.DeadlineExceeded
// (timeouts) and false for everything else — including 4xx (PRD §4.6:
// "HTTP 4xx including 429 fail immediately").
func (p RetryPolicy) shouldRetry(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var carrier HTTPStatusCarrier
	if errors.As(err, &carrier) {
		s := carrier.HTTPStatus()
		return s >= 500 && s <= 599
	}
	// Non-HTTP transport errors (DNS, TCP reset) are treated as
	// transient — the SDK surfaces them as plain error values.
	return true
}

// classify converts a raw error into the package's sentinel surface so
// service-layer mappers can use errors.Is / errors.As without caring
// about the underlying SDK type.
func classify(err error, opName string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		// DeadlineExceeded after retry exhaustion is the "AGS timeout
		// after retries" path — collapse to ErrUnavailable so callers
		// surface gRPC Unavailable per errors.md.
		return ErrUnavailable
	}
	var carrier HTTPStatusCarrier
	if errors.As(err, &carrier) {
		s := carrier.HTTPStatus()
		if s == http.StatusTooManyRequests {
			return ErrRateLimited
		}
		if s >= 400 && s <= 499 {
			return &ClientError{StatusCode: s, Op: opName, Message: err.Error()}
		}
		if s >= 500 && s <= 599 {
			return ErrUnavailable
		}
	}
	// Non-HTTP transport errors that survived retries are operationally
	// indistinguishable from upstream unavailability.
	return ErrUnavailable
}
