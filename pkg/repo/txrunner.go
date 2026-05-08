package repo

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TxRunner is the service-facing entry point for "do these queries
// inside a single tx". Approve flow uses it to chain Reserve →
// FencedFinalize → ApproveCAS atomically per docs/schema.md
// §"Approve flow"; tests substitute a fake that runs fn against a fake
// Querier without touching Postgres.
type TxRunner interface {
	InTx(ctx context.Context, fn func(q Querier) error) error
}

// PgTxRunner is the production *pgxpool.Pool-backed implementation. fn
// runs inside a real pgx.Tx; commit is implicit on a nil error from fn,
// and any non-nil error rolls back. Returning an error from fn that
// matches a sentinel (e.g. ErrPoolEmpty) is the caller's job — InTx
// itself just shuttles the error back.
type PgTxRunner struct {
	pool *pgxpool.Pool
}

// NewPgTxRunner wires a TxRunner around the shared connection pool.
func NewPgTxRunner(pool *pgxpool.Pool) *PgTxRunner {
	return &PgTxRunner{pool: pool}
}

func (r *PgTxRunner) InTx(ctx context.Context, fn func(q Querier) error) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("beginning tx: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing tx: %w", err)
	}
	return nil
}

// withAdvisoryLockTx opens a tx, takes a transaction-scoped advisory
// lock keyed on hashtext(lockKey), runs fn against the tx, and commits.
// label is interpolated into the wrap messages so failures stay
// readable (e.g. "beginning <label> tx", "acquiring <label> advisory
// lock"). fn is responsible for its own error contexts; this helper
// just owns the tx skeleton.
func withAdvisoryLockTx(ctx context.Context, pool *pgxpool.Pool, lockKey, label string, fn func(pgx.Tx) error) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("beginning %s tx: %w", label, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, lockKey); err != nil {
		return fmt.Errorf("acquiring %s advisory lock: %w", label, err)
	}
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing %s tx: %w", label, err)
	}
	return nil
}
