// Package dmqueue is the bounded in-memory DM queue (PRD §5.4 + docs/
// dm-queue.md). It owns three concerns:
//
//  1. an in-memory FIFO of pending DM sends with overflow back-pressure
//     that surfaces as `lastDmStatus='failed'` on the applicant row;
//  2. a worker goroutine that drains the FIFO at a configurable rate,
//     calls Sender, and persists send success / failure including the
//     audit-row write path from PRD §4.1 step 6d;
//  3. a circuit breaker that pauses outbound DMs after 50 consecutive
//     failures within 60s for 5 minutes (auto-resume), surfacing as
//     `lastDmError='dm_circuit_open'` on every job consumed while
//     tripped.
//
// Restart loss is recovered via Sweep — exposed separately so main.go
// can run it once on boot before Run starts; tests drive it directly.
package dmqueue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// PRD §5.4 / dm-queue.md defaults. Exposed so config + tests can
// reference the same constants.
const (
	DefaultMaxDepth         = 10000
	DefaultDrainRatePerSec  = 5
	CircuitTripFailureCount = 50
	CircuitTripWindow       = 60 * time.Second
	CircuitOpenDuration     = 5 * time.Minute
)

// last_dm_error reasons emitted by this package. The "generic" reason
// is the truncated Discord error string itself; these constants name
// only the system-attributed reasons.
const (
	ReasonQueueOverflow = "dm_queue_overflow"
	ReasonCircuitOpen   = "dm_circuit_open"
	ReasonLostOnRestart = "lost_on_restart"
)

const (
	dmStatusFailed = "failed"
	dmStatusSent   = "sent"
)

// ErrQueueFull is returned by Enqueue when the buffer is at MaxDepth.
// The queue itself has already written the failed-status row + audit
// before returning this; the caller does not need to do anything except
// note that the DM did not enqueue. Returned for observability + test
// assertions.
var ErrQueueFull = errors.New("dmqueue: queue full")

// Sender abstracts the outbound Discord DM call so the worker can be
// driven by a fake in tests. Production wires a Discord bot client.
type Sender interface {
	SendDM(ctx context.Context, discordUserID, message string) error
}

// SenderFunc adapts a plain function to Sender. Used in tests.
type SenderFunc func(ctx context.Context, discordUserID, message string) error

func (f SenderFunc) SendDM(ctx context.Context, discordUserID, message string) error {
	return f(ctx, discordUserID, message)
}

// ApplicantUpdater is the slice of repo.ApplicantStore the queue needs.
// Narrowing to two methods keeps the unit-test fake small.
type ApplicantUpdater interface {
	UpdateDMStatus(ctx context.Context, applicantID uuid.UUID, status string, attemptAt time.Time, errMsg *string) (*repo.Applicant, error)
	ListLostDMOnRestart(ctx context.Context, namespace string) ([]*repo.Applicant, error)
}

// Job is the unit of work the queue carries. Manual=true denotes a
// RetryDM-driven send (PRD §5.4) — only manual sends emit the
// applicant.dm_sent audit row on success.
type Job struct {
	ApplicantID   uuid.UUID
	PlaytestID    uuid.UUID
	UserID        uuid.UUID
	DiscordUserID string
	Message       string
	Manual        bool
}

// Config holds the runtime knobs. Zero values are filled with the
// PRD-§5.9 defaults.
type Config struct {
	MaxDepth        int
	DrainRatePerSec int
	Namespace       string
}

func (c Config) withDefaults() Config {
	if c.MaxDepth <= 0 {
		c.MaxDepth = DefaultMaxDepth
	}
	if c.DrainRatePerSec <= 0 {
		c.DrainRatePerSec = DefaultDrainRatePerSec
	}
	return c
}

// Queue is the FIFO + worker. Construct with New, then call Run inside
// a goroutine; cancel its context to stop. Enqueue is safe to call from
// any goroutine.
type Queue struct {
	cfg       Config
	ch        chan Job
	sender    Sender
	applicant ApplicantUpdater
	audit     repo.AuditLogStore
	logger    *slog.Logger

	clock func() time.Time
	sleep func(time.Duration)

	mu             sync.Mutex
	failuresWindow []time.Time
	circuitOpenAt  time.Time
}

// New constructs a Queue.
func New(cfg Config, sender Sender, applicant ApplicantUpdater, audit repo.AuditLogStore, logger *slog.Logger) *Queue {
	cfg = cfg.withDefaults()
	if logger == nil {
		logger = slog.Default()
	}
	return &Queue{
		cfg:       cfg,
		ch:        make(chan Job, cfg.MaxDepth),
		sender:    sender,
		applicant: applicant,
		audit:     audit,
		logger:    logger,
		clock:     time.Now,
		sleep:     time.Sleep,
	}
}

// Enqueue adds a Job to the FIFO. On overflow the call is non-blocking:
// the queue immediately writes `lastDmStatus='failed'` with
// `lastDmError='dm_queue_overflow'` plus an applicant.dm_failed audit
// row, and returns ErrQueueFull. The caller treats both outcomes as
// "the DM is now in the system" — no retry on its end.
func (q *Queue) Enqueue(ctx context.Context, j Job) error {
	select {
	case q.ch <- j:
		return nil
	default:
		q.markFailed(ctx, j, ReasonQueueOverflow)
		return ErrQueueFull
	}
}

// Depth returns the current FIFO depth. Used by smoke + tests.
func (q *Queue) Depth() int {
	return len(q.ch)
}

// Run drains the FIFO until ctx is cancelled. Blocks; callers
// typically invoke from a goroutine.
func (q *Queue) Run(ctx context.Context) {
	throttle := time.Second / time.Duration(q.cfg.DrainRatePerSec)
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-q.ch:
			q.process(ctx, j)
			q.sleep(throttle)
		}
	}
}

// Sweep is the boot-time idempotency guard from dm-queue.md "Restart
// behavior". Scans every APPROVED applicant whose last_dm_status is
// NULL or 'pending' (skipping rows already at 'failed' so the original
// reason is preserved), marks them failed with `lost_on_restart`, and
// emits applicant.dm_failed audit rows. Safe to call exactly once at
// boot before Run.
func (q *Queue) Sweep(ctx context.Context) (int, error) {
	rows, err := q.applicant.ListLostDMOnRestart(ctx, q.cfg.Namespace)
	if err != nil {
		return 0, fmt.Errorf("listing lost-on-restart applicants: %w", err)
	}
	for _, r := range rows {
		j := Job{ApplicantID: r.ID, PlaytestID: r.PlaytestID, UserID: r.UserID}
		q.markFailed(ctx, j, ReasonLostOnRestart)
	}
	q.logger.LogAttrs(ctx, slog.LevelInfo, "dm queue restart sweep",
		slog.String("event", "dm_restart_sweep"),
		slog.Int("affected", len(rows)),
	)
	return len(rows), nil
}

// process is the per-job pipeline. Public-method-private-helper
// pattern keeps the loop body small and the test surface focused.
func (q *Queue) process(ctx context.Context, j Job) {
	if q.checkCircuit(ctx) {
		q.markFailed(ctx, j, ReasonCircuitOpen)
		return
	}
	if err := q.sender.SendDM(ctx, j.DiscordUserID, j.Message); err != nil {
		q.markFailed(ctx, j, err.Error())
		q.recordFailure(ctx)
		return
	}
	q.markSent(ctx, j)
	q.resetFailures()
}

// checkCircuit returns true if the breaker is currently open. As a
// side-effect it auto-resets the breaker once the open window has
// elapsed and writes the dm.circuit_closed audit row.
func (q *Queue) checkCircuit(ctx context.Context) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.circuitOpenAt.IsZero() {
		return false
	}
	if q.clock().Sub(q.circuitOpenAt) < CircuitOpenDuration {
		return true
	}
	closedAt := q.clock()
	q.circuitOpenAt = time.Time{}
	q.failuresWindow = nil
	if q.audit != nil {
		if err := repo.AppendDMCircuitClosed(ctx, q.audit, q.cfg.Namespace, closedAt); err != nil {
			q.logger.LogAttrs(ctx, slog.LevelWarn, "appending dm.circuit_closed audit",
				slog.String("event", "dm_circuit_audit_failed"),
				slog.String("error", err.Error()),
			)
		}
	}
	q.logger.LogAttrs(ctx, slog.LevelInfo, "dm circuit closed",
		slog.String("event", "dm_circuit_closed"),
	)
	return false
}

// recordFailure appends a failure timestamp and trips the circuit if
// the trailing-window count crosses the threshold. The window is
// pruned in place to keep the slice bounded.
func (q *Queue) recordFailure(ctx context.Context) {
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.clock()
	cutoff := now.Add(-CircuitTripWindow)
	pruned := q.failuresWindow[:0]
	for _, t := range q.failuresWindow {
		if t.After(cutoff) {
			pruned = append(pruned, t)
		}
	}
	q.failuresWindow = append(pruned, now)
	if len(q.failuresWindow) < CircuitTripFailureCount {
		return
	}
	if !q.circuitOpenAt.IsZero() {
		return
	}
	q.circuitOpenAt = now
	if q.audit != nil {
		if err := repo.AppendDMCircuitOpened(ctx, q.audit, q.cfg.Namespace, now, len(q.failuresWindow)); err != nil {
			q.logger.LogAttrs(ctx, slog.LevelWarn, "appending dm.circuit_opened audit",
				slog.String("event", "dm_circuit_audit_failed"),
				slog.String("error", err.Error()),
			)
		}
	}
	q.logger.LogAttrs(ctx, slog.LevelWarn, "dm circuit opened",
		slog.String("event", "dm_circuit_opened"),
		slog.Int("recentFailureCount", len(q.failuresWindow)),
	)
}

// resetFailures clears the trailing-window counter. Called on every
// successful send so the breaker only trips on *consecutive*-ish
// failures within a minute.
func (q *Queue) resetFailures() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.failuresWindow = nil
}

// markSent stamps the applicant row + (manual only) emits the
// applicant.dm_sent audit row.
func (q *Queue) markSent(ctx context.Context, j Job) {
	now := q.clock()
	if _, err := q.applicant.UpdateDMStatus(ctx, j.ApplicantID, dmStatusSent, now, nil); err != nil {
		q.logger.LogAttrs(ctx, slog.LevelWarn, "updating applicant dm status to sent failed",
			slog.String("event", "dm_apply_status_failed"),
			slog.String("applicantId", j.ApplicantID.String()),
			slog.String("error", err.Error()),
		)
		return
	}
	if !j.Manual || q.audit == nil {
		return
	}
	if err := repo.AppendApplicantDMSent(ctx, q.audit, q.cfg.Namespace, j.PlaytestID, j.UserID, j.ApplicantID, j.DiscordUserID); err != nil {
		q.logger.LogAttrs(ctx, slog.LevelWarn, "appending applicant.dm_sent audit",
			slog.String("event", "dm_audit_failed"),
			slog.String("applicantId", j.ApplicantID.String()),
			slog.String("error", err.Error()),
		)
	}
}

// markFailed stamps applicant + writes the applicant.dm_failed audit.
// reason is persisted verbatim into last_dm_error (truncated by the
// repo). The audit row uses the same string so the admin UI's "DM
// failed" filter and the audit-log viewer agree on the cause.
func (q *Queue) markFailed(ctx context.Context, j Job, reason string) {
	now := q.clock()
	r := reason
	if _, err := q.applicant.UpdateDMStatus(ctx, j.ApplicantID, dmStatusFailed, now, &r); err != nil {
		q.logger.LogAttrs(ctx, slog.LevelWarn, "marking applicant dm status failed errored",
			slog.String("event", "dm_apply_status_failed"),
			slog.String("applicantId", j.ApplicantID.String()),
			slog.String("reason", reason),
			slog.String("error", err.Error()),
		)
		return
	}
	if q.audit == nil {
		return
	}
	if err := repo.AppendApplicantDMFailed(ctx, q.audit, q.cfg.Namespace, j.PlaytestID, j.ApplicantID, reason, now); err != nil {
		q.logger.LogAttrs(ctx, slog.LevelWarn, "appending applicant.dm_failed audit",
			slog.String("event", "dm_audit_failed"),
			slog.String("applicantId", j.ApplicantID.String()),
			slog.String("error", err.Error()),
		)
	}
}
