package ags_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/anggorodewanto/playtesthub/pkg/ags"
)

// fakeHTTPErr satisfies ags.HTTPStatusCarrier so the retry classifier
// can treat it the same way it treats real SDK transport errors.
type fakeHTTPErr struct {
	code int
	msg  string
}

func (e fakeHTTPErr) Error() string   { return e.msg }
func (e fakeHTTPErr) HTTPStatus() int { return e.code }

func newPolicy(maxAttempts int) ags.RetryPolicy {
	return ags.RetryPolicy{
		MaxAttempts:       maxAttempts,
		PerAttemptTimeout: 50 * time.Millisecond,
		InitialBackoff:    1 * time.Millisecond,
		MaxBackoff:        5 * time.Millisecond,
		Sleep:             func(time.Duration) {}, // tests never block
	}
}

func TestRun_Success_NoRetry(t *testing.T) {
	calls := 0
	err := newPolicy(3).Run(context.Background(), "Op", func(ctx context.Context) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestRun_4xx_FailsImmediately(t *testing.T) {
	calls := 0
	err := newPolicy(3).Run(context.Background(), "CreateItem", func(ctx context.Context) error {
		calls++
		return fakeHTTPErr{code: http.StatusBadRequest, msg: "bad request"}
	})
	if calls != 1 {
		t.Fatalf("expected 1 call (no retry on 4xx), got %d", calls)
	}
	var ce *ags.ClientError
	if !errors.As(err, &ce) {
		t.Fatalf("expected *ags.ClientError, got %T: %v", err, err)
	}
	if ce.StatusCode != http.StatusBadRequest || ce.Op != "CreateItem" {
		t.Fatalf("unexpected client error: %+v", ce)
	}
}

func TestRun_429_MapsToRateLimited(t *testing.T) {
	calls := 0
	err := newPolicy(3).Run(context.Background(), "CreateCodes", func(ctx context.Context) error {
		calls++
		return fakeHTTPErr{code: http.StatusTooManyRequests, msg: "rate limited"}
	})
	if calls != 1 {
		t.Fatalf("429 must not retry, got %d calls", calls)
	}
	if !errors.Is(err, ags.ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
}

func TestRun_5xx_RetriesUpToMaxThenUnavailable(t *testing.T) {
	calls := 0
	err := newPolicy(3).Run(context.Background(), "CreateItem", func(ctx context.Context) error {
		calls++
		return fakeHTTPErr{code: 503, msg: "unavailable"}
	})
	if calls != 3 {
		t.Fatalf("expected MaxAttempts (3) calls, got %d", calls)
	}
	if !errors.Is(err, ags.ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
}

func TestRun_5xx_ThenSuccess(t *testing.T) {
	calls := 0
	err := newPolicy(3).Run(context.Background(), "CreateItem", func(ctx context.Context) error {
		calls++
		if calls < 2 {
			return fakeHTTPErr{code: 502, msg: "bad gateway"}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls (1 retry), got %d", calls)
	}
}

func TestRun_TimeoutPerAttempt_RetriesThenUnavailable(t *testing.T) {
	calls := 0
	err := newPolicy(3).Run(context.Background(), "CreateItem", func(ctx context.Context) error {
		calls++
		<-ctx.Done()
		return ctx.Err()
	})
	if calls != 3 {
		t.Fatalf("expected MaxAttempts (3) calls on timeout, got %d", calls)
	}
	if !errors.Is(err, ags.ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable on timeout exhaustion, got %v", err)
	}
}

func TestRun_ContextCanceled_StopsImmediately(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	err := newPolicy(3).Run(ctx, "CreateItem", func(ctx context.Context) error {
		calls++
		return nil
	})
	if calls != 0 {
		t.Fatalf("expected 0 calls when ctx pre-cancelled, got %d", calls)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestWithoutRetries_Singleshot(t *testing.T) {
	calls := 0
	err := ags.WithoutRetries().Run(context.Background(), "CreateCodes", func(ctx context.Context) error {
		calls++
		return fakeHTTPErr{code: 503, msg: "unavailable"}
	})
	if calls != 1 {
		t.Fatalf("WithoutRetries must not retry, got %d", calls)
	}
	if !errors.Is(err, ags.ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
}

func TestRun_NonHTTPError_TreatedAsTransientThenUnavailable(t *testing.T) {
	calls := 0
	err := newPolicy(3).Run(context.Background(), "DeleteItem", func(ctx context.Context) error {
		calls++
		return errors.New("dial tcp: connection reset")
	})
	if calls != 3 {
		t.Fatalf("expected MaxAttempts (3) calls for non-HTTP transient, got %d", calls)
	}
	if !errors.Is(err, ags.ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
}

func TestDefaultRetryPolicy_Shape(t *testing.T) {
	p := ags.DefaultRetryPolicy()
	if p.MaxAttempts != 4 {
		t.Errorf("MaxAttempts = %d, want 4 (1 + 3 retries)", p.MaxAttempts)
	}
	if p.PerAttemptTimeout != 30*time.Second {
		t.Errorf("PerAttemptTimeout = %v, want 30s", p.PerAttemptTimeout)
	}
}

func TestWithoutRetries_Shape(t *testing.T) {
	p := ags.WithoutRetries()
	if p.MaxAttempts != 1 {
		t.Errorf("MaxAttempts = %d, want 1", p.MaxAttempts)
	}
	if p.PerAttemptTimeout != 300*time.Second {
		t.Errorf("PerAttemptTimeout = %v, want 300s", p.PerAttemptTimeout)
	}
}
