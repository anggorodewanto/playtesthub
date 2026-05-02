// Package repo is the Postgres data access layer. One file per table
// (playtest, applicant, code, audit_log, leader_lease). Each exports a
// domain struct, a store interface, and a *pgxpool.Pool-backed
// implementation. Service-layer handlers depend on the interface; tests
// at the service layer mock it, integration tests here exercise real
// SQL against testcontainers-postgres. See docs/engineering.md §2 / §3.
package repo

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Querier is the subset of pgx surface satisfied by both *pgxpool.Pool
// and pgx.Tx. Methods that must compose into a caller-controlled
// transaction (the approve flow's fenced finalize + applicant CAS, per
// docs/schema.md §"Approve flow") accept a Querier so the same code
// path serves both the pool-direct case and the in-tx case.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Sentinel errors callers may switch on with errors.Is.
var (
	// ErrNotFound is returned when a lookup matches zero rows or an
	// update affects zero rows for a reason other than a CAS mismatch.
	ErrNotFound = errors.New("repo: not found")

	// ErrUniqueViolation wraps a Postgres unique-constraint violation
	// (SQLSTATE 23505). Callers decide whether to treat the conflict as
	// user error (slug collision, duplicate code value) or idempotent
	// success (signup re-post).
	ErrUniqueViolation = errors.New("repo: unique constraint violation")

	// ErrCheckViolation wraps a Postgres CHECK-constraint violation
	// (SQLSTATE 23514) — e.g. a status enum value the DB rejects.
	ErrCheckViolation = errors.New("repo: check constraint violation")

	// ErrStatusCASMismatch is returned by status-transition helpers
	// when the current row state no longer matches the expected state
	// the caller passed in (another writer won the race).
	ErrStatusCASMismatch = errors.New("repo: status CAS mismatch")

	// ErrPoolEmpty is returned by Code.Reserve when there are no UNUSED
	// rows left in a playtest's pool. The service layer maps this to
	// the model-specific ResourceExhausted message in errors.md (rows
	// 12 / 13).
	ErrPoolEmpty = errors.New("repo: code pool empty")
)

// classifyPgError maps low-level pgconn.PgError SQLSTATE codes to the
// repo sentinels above. Returns the original error (wrapped) for codes
// we don't special-case, and nil for a nil input.
func classifyPgError(err error) error {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case "23505":
		return errors.Join(ErrUniqueViolation, err)
	case "23514":
		return errors.Join(ErrCheckViolation, err)
	}
	return err
}
