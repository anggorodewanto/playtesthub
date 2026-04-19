package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LeaderLease mirrors the leader_lease table in migration 0001. Used
// by the reclaim-job leader election described in PRD §5.5; the full
// consumer lands in M2, but the table + repo ship in M1 per the
// migration scope (docs/STATUS.md phase 3).
type LeaderLease struct {
	Name       string
	Holder     string
	AcquiredAt time.Time
	ExpiresAt  time.Time
}

// LeaderStore is the election primitive. Callers compete for a named
// lease; only one holder at a time is visible as the active leader
// until either Release is called or the lease expires.
type LeaderStore interface {
	TryAcquire(ctx context.Context, name, holder string, ttl time.Duration) (*LeaderLease, error)
	Refresh(ctx context.Context, name, holder string, ttl time.Duration) (*LeaderLease, error)
	Release(ctx context.Context, name, holder string) error
	Get(ctx context.Context, name string) (*LeaderLease, error)
}

// ErrLeaseHeld is returned when another holder currently owns a valid
// (unexpired) lease on the given name.
var ErrLeaseHeld = errors.New("repo: leader lease held by another holder")

type PgLeaderStore struct {
	pool *pgxpool.Pool
}

func NewPgLeaderStore(pool *pgxpool.Pool) *PgLeaderStore {
	return &PgLeaderStore{pool: pool}
}

// TryAcquire claims a named lease for the given holder for ttl. The
// claim succeeds when no row exists, the existing row is expired, or
// the caller is already the holder (treated as re-entrant). Otherwise
// ErrLeaseHeld is returned.
//
// The implementation uses an upsert keyed on the single name PK. The
// conflict branch updates only if the current row is expired or if
// the same holder is re-claiming — this keeps the "steal expired
// lease" path atomic.
func (s *PgLeaderStore) TryAcquire(ctx context.Context, name, holder string, ttl time.Duration) (*LeaderLease, error) {
	const sql = `
		INSERT INTO leader_lease (name, holder, acquired_at, expires_at)
		VALUES ($1, $2, NOW(), NOW() + $3::interval)
		ON CONFLICT (name) DO UPDATE
		   SET holder = EXCLUDED.holder,
		       acquired_at = EXCLUDED.acquired_at,
		       expires_at = EXCLUDED.expires_at
		 WHERE leader_lease.expires_at <= NOW()
		    OR leader_lease.holder = EXCLUDED.holder
		RETURNING name, holder, acquired_at, expires_at`

	row := s.pool.QueryRow(ctx, sql, name, holder, ttl.String())
	got, err := scanLeaderLease(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLeaseHeld
	}
	if err != nil {
		return nil, fmt.Errorf("acquiring leader lease: %w", err)
	}
	return got, nil
}

// Refresh extends the lease's expiry iff the caller is the current
// holder and the lease has not yet expired. On any other state the
// caller has lost the lease and must re-acquire.
func (s *PgLeaderStore) Refresh(ctx context.Context, name, holder string, ttl time.Duration) (*LeaderLease, error) {
	const sql = `
		UPDATE leader_lease
		   SET expires_at = NOW() + $3::interval
		 WHERE name = $1
		   AND holder = $2
		   AND expires_at > NOW()
		RETURNING name, holder, acquired_at, expires_at`

	row := s.pool.QueryRow(ctx, sql, name, holder, ttl.String())
	got, err := scanLeaderLease(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLeaseHeld
	}
	if err != nil {
		return nil, fmt.Errorf("refreshing leader lease: %w", err)
	}
	return got, nil
}

// Release clears the lease if the caller is the current holder. A
// no-op when the lease has expired or has already been taken by
// another holder.
func (s *PgLeaderStore) Release(ctx context.Context, name, holder string) error {
	const sql = `DELETE FROM leader_lease WHERE name = $1 AND holder = $2`
	_, err := s.pool.Exec(ctx, sql, name, holder)
	if err != nil {
		return fmt.Errorf("releasing leader lease: %w", err)
	}
	return nil
}

func (s *PgLeaderStore) Get(ctx context.Context, name string) (*LeaderLease, error) {
	const sql = `SELECT name, holder, acquired_at, expires_at FROM leader_lease WHERE name = $1`
	row := s.pool.QueryRow(ctx, sql, name)
	got, err := scanLeaderLease(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("fetching leader lease: %w", err)
	}
	return got, nil
}

func scanLeaderLease(row pgx.Row) (*LeaderLease, error) {
	var l LeaderLease
	if err := row.Scan(&l.Name, &l.Holder, &l.AcquiredAt, &l.ExpiresAt); err != nil {
		return nil, err
	}
	return &l, nil
}
