// Package reclaim runs the PRD §5.5 background reclaim job: a single
// elected leader periodically releases stale RESERVED Code rows back
// to UNUSED so a crashed approve flow does not strand pool inventory.
//
// Election uses the leader_lease table (docs/schema.md §"leader_lease
// table"). Every replica runs the same loop; only the lease holder
// performs the reclaim sweep on each tick. The leader heartbeats to
// keep the lease alive; on heartbeat failure the loop falls back to
// follower mode and races to re-acquire on the next tick. The lease
// holder log line + tick stats let operators see the elected
// instance and reclaimed-row counts in real time.
package reclaim

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// LeaseName is the leader_lease.name value used by the reclaim job.
// Single instance per AGS namespace per environment; the field is a
// constant rather than an env var because there is no scenario where
// running multiple reclaim leaders concurrently is correct.
const LeaseName = "reclaim-job"

// Config holds the runtime knobs for Run. Every field is required —
// callers populate from pkg/config (which already enforces the PRD
// §5.9 defaults).
type Config struct {
	// HolderID identifies this replica (e.g. POD_NAME). Two replicas
	// with the same HolderID will treat each other's lease as their
	// own (re-entrant acquisition); production wiring uses pod-unique
	// values to keep the election sharp.
	HolderID string

	// LeaseTTL bounds how long a crashed leader can starve the queue.
	// Stale leases are stealable after expiry. PRD §5.9 default 30s.
	LeaseTTL time.Duration

	// HeartbeatInterval is how often the active leader refreshes the
	// lease. Must be < LeaseTTL with safety margin. PRD §5.9 default
	// 10s.
	HeartbeatInterval time.Duration

	// ReclaimInterval is the cadence at which the active leader sweeps
	// the Code table for stale RESERVED rows. PRD §5.9 default 30s.
	ReclaimInterval time.Duration

	// ReservationTTL is the age threshold that flips a RESERVED row to
	// "stale" and reclaimable. PRD §5.9 default 60s.
	ReservationTTL time.Duration
}

// Reclaimer is the slice of repo.CodeStore the worker needs. Narrowing
// the dependency keeps the unit-test fake one method instead of the
// full CodeStore surface — and the production wire still hands in a
// *repo.PgCodeStore, which satisfies this interface for free.
type Reclaimer interface {
	Reclaim(ctx context.Context, ttl time.Duration) (int64, error)
}

// Worker is the long-running goroutine driving the reclaim loop.
// Construct with New, then call Run inside a goroutine; cancel the
// passed context to stop the loop. Worker is safe to construct (and
// Run) on every replica — only the elected leader actually sweeps.
type Worker struct {
	cfg     Config
	leases  repo.LeaderStore
	codes   Reclaimer
	logger  *slog.Logger
	clock   func() time.Time
	ticker  func(d time.Duration) (<-chan time.Time, func())
	leading bool
}

// New constructs a Worker. logger may be nil — the loop falls back to
// slog.Default(). clock and ticker are exported in tests so the test
// suite can drive virtual time; production wires them to the
// stdlib defaults via Run.
func New(cfg Config, leases repo.LeaderStore, codes Reclaimer, logger *slog.Logger) *Worker {
	if logger == nil {
		logger = slog.Default()
	}
	return &Worker{
		cfg:    cfg,
		leases: leases,
		codes:  codes,
		logger: logger,
		clock:  time.Now,
		ticker: defaultTicker,
	}
}

// Run drives the reclaim loop until ctx is cancelled. It returns
// nil on a clean shutdown. Errors during reclaim or heartbeat are
// logged but never abort the loop — the next tick retries.
func (w *Worker) Run(ctx context.Context) error {
	tickCh, stopTick := w.ticker(tickPeriod(w.cfg))
	defer stopTick()

	// Try to acquire on boot so the first tick can do useful work
	// immediately. A miss is not fatal — the next tick re-tries.
	w.tryAcquire(ctx)

	for {
		select {
		case <-ctx.Done():
			w.releaseIfLeading(context.Background())
			return nil
		case <-tickCh:
			w.tick(ctx)
		}
	}
}

// tick performs one iteration: leaders heartbeat + sweep, followers
// race for the lease.
func (w *Worker) tick(ctx context.Context) {
	if !w.leading {
		w.tryAcquire(ctx)
		if !w.leading {
			return
		}
	}
	if !w.heartbeat(ctx) {
		// Lost lease; do nothing this tick. Next tick re-tries.
		return
	}
	released, err := w.codes.Reclaim(ctx, w.cfg.ReservationTTL)
	if err != nil {
		w.logger.LogAttrs(ctx, slog.LevelWarn, "reclaim sweep failed",
			slog.String("event", "reclaim_tick"),
			slog.String("leaseHolder", w.cfg.HolderID),
			slog.String("error", err.Error()),
		)
		return
	}
	w.logger.LogAttrs(ctx, slog.LevelInfo, "reclaim tick",
		slog.String("event", "reclaim_tick"),
		slog.String("leaseHolder", w.cfg.HolderID),
		slog.Int64("released", released),
	)
}

// tryAcquire attempts to grab the lease. ErrLeaseHeld is the
// expected non-leader outcome; anything else is logged and treated as
// a transient miss.
func (w *Worker) tryAcquire(ctx context.Context) {
	_, err := w.leases.TryAcquire(ctx, LeaseName, w.cfg.HolderID, w.cfg.LeaseTTL)
	if err == nil {
		w.leading = true
		w.logger.LogAttrs(ctx, slog.LevelInfo, "reclaim worker acquired leader lease",
			slog.String("event", "reclaim_lease_acquired"),
			slog.String("leaseHolder", w.cfg.HolderID),
		)
		return
	}
	if !errors.Is(err, repo.ErrLeaseHeld) {
		w.logger.LogAttrs(ctx, slog.LevelWarn, "reclaim lease acquire failed",
			slog.String("event", "reclaim_lease_acquire_failed"),
			slog.String("leaseHolder", w.cfg.HolderID),
			slog.String("error", err.Error()),
		)
	}
	w.leading = false
}

// heartbeat refreshes the lease. Returns false if the leader has lost
// it (e.g. clock drift caused expiry); callers stop the current tick.
func (w *Worker) heartbeat(ctx context.Context) bool {
	_, err := w.leases.Refresh(ctx, LeaseName, w.cfg.HolderID, w.cfg.LeaseTTL)
	if err == nil {
		return true
	}
	w.leading = false
	if !errors.Is(err, repo.ErrLeaseHeld) {
		w.logger.LogAttrs(ctx, slog.LevelWarn, "reclaim heartbeat failed",
			slog.String("event", "reclaim_heartbeat_failed"),
			slog.String("leaseHolder", w.cfg.HolderID),
			slog.String("error", err.Error()),
		)
		return false
	}
	w.logger.LogAttrs(ctx, slog.LevelInfo, "reclaim worker lost leader lease",
		slog.String("event", "reclaim_lease_lost"),
		slog.String("leaseHolder", w.cfg.HolderID),
	)
	return false
}

func (w *Worker) releaseIfLeading(ctx context.Context) {
	if !w.leading {
		return
	}
	if err := w.leases.Release(ctx, LeaseName, w.cfg.HolderID); err != nil {
		w.logger.LogAttrs(ctx, slog.LevelWarn, "reclaim worker release failed",
			slog.String("event", "reclaim_lease_release_failed"),
			slog.String("error", err.Error()),
		)
	}
	w.leading = false
}

// tickPeriod selects the ticker interval. ReclaimInterval drives the
// sweep cadence; HeartbeatInterval is enforced inside tick() because
// the heartbeat piggybacks on the same tick (PRD §5.9 ratios make the
// two compatible — heartbeat 10s, reclaim 30s, lease 30s).
//
// To honour both cadences with a single ticker the worker fires at
// the GCD of the two; for the PRD defaults (10s / 30s) that is 10s,
// so heartbeats happen every tick and the sweep runs on every third.
// Currently we collapse the two by ticking at HeartbeatInterval and
// running the sweep every tick — Reclaim is a single UPDATE, so a
// 3× higher cadence is fine. A future enhancement could split.
func tickPeriod(cfg Config) time.Duration {
	if cfg.HeartbeatInterval <= 0 {
		return cfg.ReclaimInterval
	}
	return cfg.HeartbeatInterval
}

func defaultTicker(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTicker(d)
	return t.C, t.Stop
}
