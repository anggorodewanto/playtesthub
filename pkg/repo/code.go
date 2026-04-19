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
// reserve/finalize pair lands in M2; M1 only needs seeding + counting
// so the CSV-upload path (phase 8 of M2) has a target to land against.
type CodeStore interface {
	BulkInsert(ctx context.Context, playtestID uuid.UUID, values []string) (int, error)
	CountByState(ctx context.Context, playtestID uuid.UUID) (map[string]int, error)
	GetByID(ctx context.Context, id uuid.UUID) (*Code, error)
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
