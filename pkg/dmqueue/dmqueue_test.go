package dmqueue

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

const testNamespace = "ns"

// fakeApplicantUpdater is a minimal in-memory ApplicantUpdater. It
// records every UpdateDMStatus call so tests can assert the right
// reason was persisted; ListLostDMOnRestart returns whatever the test
// staged via lostRows.
type fakeApplicantUpdater struct {
	mu        sync.Mutex
	calls     []dmStatusCall
	lostRows  []*repo.Applicant
	updateErr error
}

type dmStatusCall struct {
	ApplicantID uuid.UUID
	Status      string
	AttemptAt   time.Time
	ErrMsg      *string
}

func (f *fakeApplicantUpdater) UpdateDMStatus(_ context.Context, applicantID uuid.UUID, status string, attemptAt time.Time, errMsg *string) (*repo.Applicant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	var copyMsg *string
	if errMsg != nil {
		v := *errMsg
		copyMsg = &v
	}
	f.calls = append(f.calls, dmStatusCall{ApplicantID: applicantID, Status: status, AttemptAt: attemptAt, ErrMsg: copyMsg})
	return &repo.Applicant{ID: applicantID, Status: "APPROVED"}, nil
}

func (f *fakeApplicantUpdater) ListLostDMOnRestart(_ context.Context, _ string) ([]*repo.Applicant, error) {
	return f.lostRows, nil
}

func (f *fakeApplicantUpdater) snapshot() []dmStatusCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]dmStatusCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// fakeAuditStore captures every Append call so tests can assert action
// + payload shape. Not goroutine-safe by accident: callers serialise via
// the queue worker.
type fakeAuditStore struct {
	mu   sync.Mutex
	rows []*repo.AuditLog
}

func (f *fakeAuditStore) Append(_ context.Context, row *repo.AuditLog) (*repo.AuditLog, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	clone := *row
	f.rows = append(f.rows, &clone)
	return &clone, nil
}

func (f *fakeAuditStore) ListByPlaytest(context.Context, uuid.UUID, int) ([]*repo.AuditLog, error) {
	return nil, nil
}

func (f *fakeAuditStore) snapshot() []*repo.AuditLog {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]*repo.AuditLog, len(f.rows))
	copy(out, f.rows)
	return out
}

func newQueueForTest(sender Sender) (*Queue, *fakeApplicantUpdater, *fakeAuditStore) {
	app := &fakeApplicantUpdater{}
	aud := &fakeAuditStore{}
	q := New(Config{MaxDepth: 4, DrainRatePerSec: 100, Namespace: testNamespace}, sender, app, aud, nil)
	q.sleep = func(time.Duration) {}
	return q, app, aud
}

func mustJob(manual bool) Job {
	return Job{
		ApplicantID:   uuid.New(),
		PlaytestID:    uuid.New(),
		UserID:        uuid.New(),
		DiscordUserID: "discord:user",
		Message:       "you are approved",
		Manual:        manual,
	}
}

// TestEnqueue_Drains_MarksSent_NoAuditOnAutoSend verifies the happy
// path: a job pushed to the queue runs through Sender → markSent and
// the auto-send path writes no applicant.dm_sent audit row (PRD §5.4
// reserves that audit for manual retries only).
func TestEnqueue_Drains_MarksSent_NoAuditOnAutoSend(t *testing.T) {
	var sent atomic.Int32
	q, app, aud := newQueueForTest(SenderFunc(func(_ context.Context, _, _ string) error {
		sent.Add(1)
		return nil
	}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go q.Run(ctx)

	if err := q.Enqueue(ctx, mustJob(false)); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	waitFor(t, "sender called", func() bool { return sent.Load() == 1 })
	waitFor(t, "applicant marked sent", func() bool {
		for _, c := range app.snapshot() {
			if c.Status == dmStatusSent {
				return true
			}
		}
		return false
	})
	for _, r := range aud.snapshot() {
		if r.Action == repo.ActionApplicantDMSent {
			t.Fatalf("auto-send must not write applicant.dm_sent audit; got %+v", r)
		}
	}
}

// TestEnqueue_Manual_WritesDMSentAudit covers the symmetric: a manual
// Retry DM that succeeds emits the applicant.dm_sent audit row.
func TestEnqueue_Manual_WritesDMSentAudit(t *testing.T) {
	q, _, aud := newQueueForTest(SenderFunc(func(_ context.Context, _, _ string) error { return nil }))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go q.Run(ctx)

	j := mustJob(true)
	if err := q.Enqueue(ctx, j); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	waitFor(t, "audit appended", func() bool {
		for _, r := range aud.snapshot() {
			if r.Action == repo.ActionApplicantDMSent {
				return true
			}
		}
		return false
	})
}

// TestEnqueue_Overflow_MarksFailedSync verifies the bounded-FIFO
// contract: a full buffer surfaces ErrQueueFull and the applicant is
// immediately marked failed with reason=dm_queue_overflow + an audit
// row.
func TestEnqueue_Overflow_MarksFailedSync(t *testing.T) {
	// Worker is not started — the channel will fill on the second push.
	q, app, aud := newQueueForTest(SenderFunc(func(_ context.Context, _, _ string) error { return nil }))
	q.cfg.MaxDepth = 1
	q.ch = make(chan Job, 1)

	ctx := context.Background()
	if err := q.Enqueue(ctx, mustJob(false)); err != nil {
		t.Fatalf("first enqueue: %v", err)
	}
	overflow := mustJob(false)
	err := q.Enqueue(ctx, overflow)
	if !errors.Is(err, ErrQueueFull) {
		t.Fatalf("want ErrQueueFull on overflow, got %v", err)
	}

	calls := app.snapshot()
	if len(calls) != 1 {
		t.Fatalf("want 1 sync mark, got %d: %+v", len(calls), calls)
	}
	c := calls[0]
	if c.ApplicantID != overflow.ApplicantID || c.Status != dmStatusFailed || c.ErrMsg == nil || *c.ErrMsg != ReasonQueueOverflow {
		t.Fatalf("want overflow mark on second job, got %+v (errMsg=%v)", c, c.ErrMsg)
	}

	rows := aud.snapshot()
	if len(rows) != 1 || rows[0].Action != repo.ActionApplicantDMFailed {
		t.Fatalf("want one applicant.dm_failed audit, got %+v", rows)
	}
}

// TestSendFailure_MarksFailedAndUsesErrAsReason: when Sender returns an
// error, the queue stamps applicant.last_dm_error with the error
// string verbatim (the repo handles UTF-8-safe truncation).
func TestSendFailure_MarksFailedAndUsesErrAsReason(t *testing.T) {
	q, app, _ := newQueueForTest(SenderFunc(func(_ context.Context, _, _ string) error {
		return errors.New("discord: 500 Internal Server Error")
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go q.Run(ctx)

	if err := q.Enqueue(ctx, mustJob(false)); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	waitFor(t, "applicant marked failed", func() bool {
		for _, c := range app.snapshot() {
			if c.Status != dmStatusFailed || c.ErrMsg == nil {
				continue
			}
			if *c.ErrMsg == "discord: 500 Internal Server Error" {
				return true
			}
		}
		return false
	})
}

// TestCircuitBreaker_TripsAfter50Failures_PausesSends covers the
// 50-failures-in-60s trip + auto-resume contract. Subsequent jobs are
// marked failed with reason=dm_circuit_open without invoking Sender;
// dm.circuit_opened audit is emitted exactly once on the trip.
func TestCircuitBreaker_TripsAfter50Failures_PausesSends(t *testing.T) {
	var senderCalls atomic.Int32
	q, app, aud := newQueueForTest(SenderFunc(func(_ context.Context, _, _ string) error {
		senderCalls.Add(1)
		return errors.New("boom")
	}))
	q.cfg.MaxDepth = CircuitTripFailureCount + 5
	q.ch = make(chan Job, q.cfg.MaxDepth)

	now := time.Unix(1700000000, 0)
	mu := sync.Mutex{}
	q.clock = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go q.Run(ctx)

	for i := range CircuitTripFailureCount {
		if err := q.Enqueue(ctx, mustJob(false)); err != nil {
			t.Fatalf("Enqueue %d: %v", i, err)
		}
	}
	waitFor(t, "sender called 50x", func() bool { return senderCalls.Load() >= int32(CircuitTripFailureCount) })

	openAudit := 0
	for _, r := range aud.snapshot() {
		if r.Action == repo.ActionDMCircuitOpened {
			openAudit++
		}
	}
	if openAudit != 1 {
		t.Fatalf("want exactly 1 dm.circuit_opened, got %d", openAudit)
	}

	// Next job should be short-circuited: marked failed with reason=
	// dm_circuit_open and Sender NOT called.
	preCalls := senderCalls.Load()
	short := mustJob(false)
	if err := q.Enqueue(ctx, short); err != nil {
		t.Fatalf("post-trip Enqueue: %v", err)
	}
	waitFor(t, "short-circuit job marked failed", func() bool {
		for _, c := range app.snapshot() {
			if c.ApplicantID == short.ApplicantID && c.ErrMsg != nil && *c.ErrMsg == ReasonCircuitOpen {
				return true
			}
		}
		return false
	})
	if got := senderCalls.Load(); got != preCalls {
		t.Fatalf("post-trip Sender invoked: pre=%d post=%d", preCalls, got)
	}

	// Advance clock past the open window — next job recovers and Sender
	// is invoked again. (Sender still errors so the new attempt fails,
	// but the circuit is closed so the failure goes through Sender.)
	mu.Lock()
	now = now.Add(CircuitOpenDuration + time.Second)
	mu.Unlock()
	postOpen := mustJob(false)
	if err := q.Enqueue(ctx, postOpen); err != nil {
		t.Fatalf("post-recover Enqueue: %v", err)
	}
	waitFor(t, "sender called after recover", func() bool { return senderCalls.Load() > preCalls })

	closeAudit := 0
	for _, r := range aud.snapshot() {
		if r.Action == repo.ActionDMCircuitClosed {
			closeAudit++
		}
	}
	if closeAudit != 1 {
		t.Fatalf("want exactly 1 dm.circuit_closed after recover, got %d", closeAudit)
	}
}

// TestSweep_MarksApprovedRowsLostOnRestart verifies the boot-time
// idempotency guard from dm-queue.md.
func TestSweep_MarksApprovedRowsLostOnRestart(t *testing.T) {
	q, app, aud := newQueueForTest(SenderFunc(func(_ context.Context, _, _ string) error { return nil }))
	id1 := uuid.New()
	id2 := uuid.New()
	ptID := uuid.New()
	app.lostRows = []*repo.Applicant{
		{ID: id1, PlaytestID: ptID, UserID: uuid.New(), Status: "APPROVED"},
		{ID: id2, PlaytestID: ptID, UserID: uuid.New(), Status: "APPROVED"},
	}

	n, err := q.Sweep(context.Background())
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if n != 2 {
		t.Fatalf("want 2 swept, got %d", n)
	}

	calls := app.snapshot()
	if len(calls) != 2 {
		t.Fatalf("want 2 mark-failed calls, got %d", len(calls))
	}
	for _, c := range calls {
		if c.Status != dmStatusFailed || c.ErrMsg == nil || *c.ErrMsg != ReasonLostOnRestart {
			t.Fatalf("want lost_on_restart mark, got %+v (errMsg=%v)", c, c.ErrMsg)
		}
	}

	failed := 0
	for _, r := range aud.snapshot() {
		if r.Action == repo.ActionApplicantDMFailed {
			failed++
		}
	}
	if failed != 2 {
		t.Fatalf("want 2 applicant.dm_failed audit rows, got %d", failed)
	}
}

// waitFor polls a predicate up to ~1s. Failure dumps a marker so the
// reader knows which assertion timed out.
func waitFor(t *testing.T, what string, pred func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if pred() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("waitFor timed out: %s", what)
}
