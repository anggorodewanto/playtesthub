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
