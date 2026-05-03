package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Code-state enum values — keep in sync with the code_state_enum CHECK
// in migration 0001.
const (
	CodeStateUnused   = "UNUSED"
	CodeStateReserved = "RESERVED"
	CodeStateGranted  = "GRANTED"
)

// Code mirrors the code table in migration 0001. The value column
// carries the raw Steam key (STEAM_KEYS) or AGS-generated alphanumeric
// (AGS_CAMPAIGN); it is forbidden from logs and audit-log payloads
// (PRD §6 Observability; docs/schema.md redaction rule).
type Code struct {
	ID         uuid.UUID
	PlaytestID uuid.UUID
	Value      string
	State      string
	ReservedBy *uuid.UUID
	ReservedAt *time.Time
	GrantedAt  *time.Time
	CreatedAt  time.Time
}

// CodeStore is the data access surface for pool inventory. The
// reserve / finalize / reclaim trio lands in M2 phase 3 to support
// the approve flow (PRD §4.1 step 6 + docs/schema.md §"Approve flow")
// and the reclaim worker (PRD §5.5).
type CodeStore interface {
	BulkInsert(ctx context.Context, playtestID uuid.UUID, values []string) (int, error)
	BulkInsertCSV(ctx context.Context, playtestID uuid.UUID, values []string) (int, error)
	BulkInsertGenerated(ctx context.Context, playtestID uuid.UUID, values []string) (int, error)
	CountByState(ctx context.Context, playtestID uuid.UUID) (map[string]int, error)
	GetByID(ctx context.Context, id uuid.UUID) (*Code, error)
	// ListByPlaytest returns every Code row for a playtest ordered by
	// created_at ASC, id ASC. Powers GetCodePool — admin surfaces are
	// exempt from the §6 log-redaction rule (PRD §5.7), so callers may
	// surface raw values in API responses (never logs).
	ListByPlaytest(ctx context.Context, playtestID uuid.UUID) ([]*Code, error)
	// UploadAtomic ingests <values> for the playtest under the PRD §4.3
	// concurrency discipline: opens a tx, takes the per-playtest
	// pg_advisory_xact_lock, dedups against existing code rows, inserts
	// via CopyFrom, and commits. When at least one duplicate-against-DB
	// is found, no rows are inserted and the offending values are
	// returned for the caller to map back to CSV line numbers (whole-
	// file reject per PRD §4.3). On success returns (inserted=N, dups=
	// nil); on dedup failure returns (inserted=0, dups=...); the empty
	// values slice short-circuits to (0, nil, nil).
	UploadAtomic(ctx context.Context, playtestID uuid.UUID, values []string) (inserted int, dupsAgainstDB []string, err error)
	// Reserve picks a single UNUSED row from the playtest's pool, marks
	// it RESERVED with reserved_by=userID and reserved_at=NOW(), and
	// returns the row. Concurrent callers do not block on each other —
	// FOR UPDATE SKIP LOCKED hands each one a distinct row. Returns
	// ErrPoolEmpty when no UNUSED rows are available.
	Reserve(ctx context.Context, q Querier, playtestID, userID uuid.UUID) (*Code, error)
	// FencedFinalize runs the canonical fenced UPDATE from schema.md
	// §"Approve flow". Returns the number of rows affected: 1 on
	// success, 0 when the reservation was reclaimed/stolen between
	// reserve and finalize. The service layer turns the 0-row case
	// into the code.grant_orphaned audit + Aborted RPC error
	// (errors.md row 10).
	FencedFinalize(ctx context.Context, q Querier, codeID, userID uuid.UUID, originalReservedAt time.Time) (int64, error)
	// Reclaim flips RESERVED rows whose reservation is older than ttl
	// back to UNUSED and clears reserved_by / reserved_at. Powers the
	// reclaim worker (PRD §5.5). Returns the number of rows released.
	Reclaim(ctx context.Context, ttl time.Duration) (int64, error)
}

type PgCodeStore struct {
	pool *pgxpool.Pool
}

func NewPgCodeStore(pool *pgxpool.Pool) *PgCodeStore {
	return &PgCodeStore{pool: pool}
}

// BulkInsert inserts a batch of code values for a playtest. Returns
// the number of rows inserted. A duplicate (playtest_id, value) pair
// produces ErrUniqueViolation for the whole batch (pgx CopyFrom aborts
// on first conflict). Callers that need per-row validation must do it
// upstream.
func (s *PgCodeStore) BulkInsert(ctx context.Context, playtestID uuid.UUID, values []string) (int, error) {
	if len(values) == 0 {
		return 0, nil
	}
	rows := make([][]any, len(values))
	for i, v := range values {
		rows[i] = []any{playtestID, v}
	}
	copied, err := s.pool.CopyFrom(ctx,
		pgx.Identifier{"code"},
		[]string{"playtest_id", "value"},
		pgx.CopyFromRows(rows),
	)
	if err != nil {
		return 0, fmt.Errorf("bulk inserting codes: %w", classifyPgError(err))
	}
	return int(copied), nil
}

// CountByState returns row counts per state for a given playtest,
// powering the admin key-pool stats tile (PRD §5.7 page 2, lands in
// M2). Missing states are omitted from the map; callers should treat
// missing keys as zero.
func (s *PgCodeStore) CountByState(ctx context.Context, playtestID uuid.UUID) (map[string]int, error) {
	const sql = `SELECT state, COUNT(*) FROM code WHERE playtest_id = $1 GROUP BY state`
	rows, err := s.pool.Query(ctx, sql, playtestID)
	if err != nil {
		return nil, fmt.Errorf("counting codes: %w", err)
	}
	defer rows.Close()

	out := make(map[string]int)
	for rows.Next() {
		var state string
		var count int
		if scanErr := rows.Scan(&state, &count); scanErr != nil {
			return nil, fmt.Errorf("scanning code count row: %w", scanErr)
		}
		out[state] = count
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterating code count rows: %w", rowsErr)
	}
	return out, nil
}

func (s *PgCodeStore) GetByID(ctx context.Context, id uuid.UUID) (*Code, error) {
	const sql = `
		SELECT id, playtest_id, value, state, reserved_by, reserved_at, granted_at, created_at
		  FROM code
		 WHERE id = $1`
	row := s.pool.QueryRow(ctx, sql, id)
	got, err := scanCode(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("fetching code by id: %w", err)
	}
	return got, nil
}

// BulkInsertCSV is the STEAM_KEYS upload path. Same SQL behavior as
// BulkInsert (CopyFrom is atomic by default — duplicate values abort
// the whole batch via the UNIQUE (playtest_id, value) constraint per
// PRD §4.3 whole-file-reject rule). Named for the call site so the
// upload-tx + advisory-lock concerns the service layer wraps around it
// stay legible.
func (s *PgCodeStore) BulkInsertCSV(ctx context.Context, playtestID uuid.UUID, values []string) (int, error) {
	return s.BulkInsert(ctx, playtestID, values)
}

// BulkInsertGenerated is the AGS_CAMPAIGN insert path: writes one
// generated batch with the same atomic-batch semantics as BulkInsert.
// The non-idempotent / no-tx wrapper rule for TopUpCodes (PRD §4.6)
// lives at the service layer.
func (s *PgCodeStore) BulkInsertGenerated(ctx context.Context, playtestID uuid.UUID, values []string) (int, error) {
	return s.BulkInsert(ctx, playtestID, values)
}

const codeColumns = `id, playtest_id, value, state, reserved_by, reserved_at, granted_at, created_at`

// Reserve atomically picks one UNUSED row and flips it to RESERVED.
// The inner SELECT uses FOR UPDATE SKIP LOCKED so concurrent reservers
// take different rows; ORDER BY created_at ASC keeps consumption FIFO
// (oldest codes redeemed first).
func (s *PgCodeStore) Reserve(ctx context.Context, q Querier, playtestID, userID uuid.UUID) (*Code, error) {
	const sql = `
		UPDATE code
		   SET state = 'RESERVED',
		       reserved_by = $2,
		       reserved_at = NOW()
		 WHERE id = (
		     SELECT id
		       FROM code
		      WHERE playtest_id = $1
		        AND state = 'UNUSED'
		      ORDER BY created_at ASC
		      LIMIT 1
		      FOR UPDATE SKIP LOCKED
		 )
		RETURNING ` + codeColumns

	row := q.QueryRow(ctx, sql, playtestID, userID)
	got, err := scanCode(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPoolEmpty
	}
	if err != nil {
		return nil, fmt.Errorf("reserving code: %w", classifyPgError(err))
	}
	return got, nil
}

// FencedFinalize runs the canonical fenced UPDATE. Returns the rows
// affected (0 = reclaim-and-steal happened mid-approve; 1 = success).
func (s *PgCodeStore) FencedFinalize(ctx context.Context, q Querier, codeID, userID uuid.UUID, originalReservedAt time.Time) (int64, error) {
	const sql = `
		UPDATE code
		   SET state = 'GRANTED',
		       granted_at = NOW()
		 WHERE id = $1
		   AND state = 'RESERVED'
		   AND reserved_by = $2
		   AND reserved_at = $3`

	tag, err := q.Exec(ctx, sql, codeID, userID, originalReservedAt)
	if err != nil {
		return 0, fmt.Errorf("finalizing code grant: %w", classifyPgError(err))
	}
	return tag.RowsAffected(), nil
}

// ListByPlaytest returns every code row for a playtest, oldest first.
// Powers the admin GetCodePool view (PRD §5.7 page 4). FIFO ordering
// matches the Reserve() consumption order so the admin sees the same
// list the approve flow walks.
func (s *PgCodeStore) ListByPlaytest(ctx context.Context, playtestID uuid.UUID) ([]*Code, error) {
	const sql = `SELECT ` + codeColumns + `
		           FROM code
		          WHERE playtest_id = $1
		          ORDER BY created_at ASC, id ASC`

	rows, err := s.pool.Query(ctx, sql, playtestID)
	if err != nil {
		return nil, fmt.Errorf("listing codes: %w", err)
	}
	defer rows.Close()

	out := make([]*Code, 0)
	for rows.Next() {
		c, scanErr := scanCode(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scanning code row: %w", scanErr)
		}
		out = append(out, c)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterating code rows: %w", rowsErr)
	}
	return out, nil
}

// UploadAtomic implements PRD §4.3's whole-file-reject rule under one
// tx with the per-playtest advisory lock. The lock serialises with
// other UploadCodes / TopUpCodes calls for the same playtest; approves
// are *not* serialised (they take row-level locks on Code rows
// instead, per §4.1 step 6 — uploads and approves interleave freely).
//
// The dedup-vs-DB SELECT runs *inside* the locked tx: another writer
// cannot insert a colliding row between the SELECT and the COPY, so
// the whole-file-reject decision is observed-state-correct. The COPY
// itself is also covered by the UNIQUE (playtest_id, value) index, so
// even without the dedup query a colliding write would surface as
// 23505 — the dedup query exists to give the service per-value reject
// feedback rather than a single batch-aborted error.
func (s *PgCodeStore) UploadAtomic(ctx context.Context, playtestID uuid.UUID, values []string) (int, []string, error) {
	if len(values) == 0 {
		return 0, nil, nil
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, nil, fmt.Errorf("beginning upload tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// hashtext(int4) coerces a TEXT to a deterministic int4 — the lock
	// key is the playtest id stringified, which keeps the lock space
	// per-playtest.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, playtestID.String()); err != nil {
		return 0, nil, fmt.Errorf("acquiring upload advisory lock: %w", err)
	}

	dupRows, err := tx.Query(ctx,
		`SELECT value FROM code WHERE playtest_id = $1 AND value = ANY($2)`,
		playtestID, values)
	if err != nil {
		return 0, nil, fmt.Errorf("dedup query: %w", err)
	}
	dups := make([]string, 0)
	for dupRows.Next() {
		var v string
		if scanErr := dupRows.Scan(&v); scanErr != nil {
			dupRows.Close()
			return 0, nil, fmt.Errorf("scanning dedup row: %w", scanErr)
		}
		dups = append(dups, v)
	}
	dupRows.Close()
	if rowsErr := dupRows.Err(); rowsErr != nil {
		return 0, nil, fmt.Errorf("iterating dedup rows: %w", rowsErr)
	}
	if len(dups) > 0 {
		// PRD §4.3: whole-file reject — release the lock without
		// inserting anything. Tx rollback covers both.
		return 0, dups, nil
	}

	rowsCopy := make([][]any, len(values))
	for i, v := range values {
		rowsCopy[i] = []any{playtestID, v}
	}
	copied, err := tx.CopyFrom(ctx,
		pgx.Identifier{"code"},
		[]string{"playtest_id", "value"},
		pgx.CopyFromRows(rowsCopy),
	)
	if err != nil {
		return 0, nil, fmt.Errorf("copying upload rows: %w", classifyPgError(err))
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, nil, fmt.Errorf("committing upload tx: %w", err)
	}
	return int(copied), nil, nil
}

// Reclaim releases RESERVED rows whose reservation is older than ttl.
// Pool-wide (no playtest filter) so a single tick cleans the whole
// instance. PRD §5.5 reclaim worker.
func (s *PgCodeStore) Reclaim(ctx context.Context, ttl time.Duration) (int64, error) {
	const sql = `
		UPDATE code
		   SET state = 'UNUSED',
		       reserved_by = NULL,
		       reserved_at = NULL
		 WHERE state = 'RESERVED'
		   AND reserved_at < NOW() - $1::interval`

	tag, err := s.pool.Exec(ctx, sql, ttl.String())
	if err != nil {
		return 0, fmt.Errorf("reclaiming codes: %w", err)
	}
	return tag.RowsAffected(), nil
}

func scanCode(row pgx.Row) (*Code, error) {
	var (
		c          Code
		reservedBy pgtype.UUID
		reservedAt pgtype.Timestamptz
		grantedAt  pgtype.Timestamptz
	)
	err := row.Scan(
		&c.ID,
		&c.PlaytestID,
		&c.Value,
		&c.State,
		&reservedBy,
		&reservedAt,
		&grantedAt,
		&c.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	if reservedBy.Valid {
		id := uuid.UUID(reservedBy.Bytes)
		c.ReservedBy = &id
	}
	if reservedAt.Valid {
		t := reservedAt.Time
		c.ReservedAt = &t
	}
	if grantedAt.Valid {
		t := grantedAt.Time
		c.GrantedAt = &t
	}
	return &c, nil
}
