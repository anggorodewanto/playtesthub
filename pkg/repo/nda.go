package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NDAAcceptance mirrors the nda_acceptance table from migration 0002.
// The composite PK (user_id, playtest_id, nda_version_hash) is the
// natural idempotency key per PRD §4.7 / §5.3 and docs/schema.md.
type NDAAcceptance struct {
	UserID         uuid.UUID
	PlaytestID     uuid.UUID
	NDAVersionHash string
	AcceptedAt     time.Time
}

// NDAAcceptanceStore is the data access surface for the append-only
// click-accept ledger.
type NDAAcceptanceStore interface {
	// AcceptIdempotent inserts a new acceptance row, or returns the
	// existing row when one already exists for the natural key
	// (userId, playtestId, ndaVersionHash). The boolean second return is
	// true when an existing row was returned (i.e. the call was a
	// re-accept, not a fresh write). PRD §4.7: a second AcceptNDA call
	// on the same key is success, not an error.
	AcceptIdempotent(ctx context.Context, a *NDAAcceptance) (*NDAAcceptance, bool, error)
	// Get returns the acceptance row for a natural key, or ErrNotFound.
	Get(ctx context.Context, userID, playtestID uuid.UUID, hash string) (*NDAAcceptance, error)
	// LatestForApplicant returns the most recent acceptance for a
	// (userID, playtestID) regardless of hash, or ErrNotFound if the
	// applicant has never accepted any NDA version on this playtest.
	// Powers the §5.3 NdaReacceptRequired derived state.
	LatestForApplicant(ctx context.Context, userID, playtestID uuid.UUID) (*NDAAcceptance, error)
}

type PgNDAAcceptanceStore struct {
	pool *pgxpool.Pool
}

func NewPgNDAAcceptanceStore(pool *pgxpool.Pool) *PgNDAAcceptanceStore {
	return &PgNDAAcceptanceStore{pool: pool}
}

const ndaAcceptanceColumns = `user_id, playtest_id, nda_version_hash, accepted_at`

// AcceptIdempotent uses INSERT ... ON CONFLICT DO NOTHING + a follow-up
// SELECT on the natural key. The conflict path means a re-accept
// returns the original `accepted_at`, not NOW() — that timestamp is the
// authoritative "first time this user accepted this exact NDA text"
// record (PRD §5.3).
func (s *PgNDAAcceptanceStore) AcceptIdempotent(ctx context.Context, a *NDAAcceptance) (*NDAAcceptance, bool, error) {
	const insertSQL = `
		INSERT INTO nda_acceptance (user_id, playtest_id, nda_version_hash)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, playtest_id, nda_version_hash) DO NOTHING
		RETURNING ` + ndaAcceptanceColumns

	row := s.pool.QueryRow(ctx, insertSQL, a.UserID, a.PlaytestID, a.NDAVersionHash)
	got, err := scanNDAAcceptance(row)
	if err == nil {
		return got, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, fmt.Errorf("inserting nda acceptance: %w", classifyPgError(err))
	}

	existing, getErr := s.Get(ctx, a.UserID, a.PlaytestID, a.NDAVersionHash)
	if getErr != nil {
		return nil, false, fmt.Errorf("fetching existing nda acceptance: %w", getErr)
	}
	return existing, true, nil
}

func (s *PgNDAAcceptanceStore) Get(ctx context.Context, userID, playtestID uuid.UUID, hash string) (*NDAAcceptance, error) {
	const sql = `SELECT ` + ndaAcceptanceColumns + `
	               FROM nda_acceptance
	              WHERE user_id = $1
	                AND playtest_id = $2
	                AND nda_version_hash = $3`
	row := s.pool.QueryRow(ctx, sql, userID, playtestID, hash)
	got, err := scanNDAAcceptance(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("fetching nda acceptance: %w", err)
	}
	return got, nil
}

func (s *PgNDAAcceptanceStore) LatestForApplicant(ctx context.Context, userID, playtestID uuid.UUID) (*NDAAcceptance, error) {
	const sql = `SELECT ` + ndaAcceptanceColumns + `
	               FROM nda_acceptance
	              WHERE user_id = $1
	                AND playtest_id = $2
	              ORDER BY accepted_at DESC
	              LIMIT 1`
	row := s.pool.QueryRow(ctx, sql, userID, playtestID)
	got, err := scanNDAAcceptance(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("fetching latest nda acceptance: %w", err)
	}
	return got, nil
}

func scanNDAAcceptance(row pgx.Row) (*NDAAcceptance, error) {
	var a NDAAcceptance
	if err := row.Scan(&a.UserID, &a.PlaytestID, &a.NDAVersionHash, &a.AcceptedAt); err != nil {
		return nil, err
	}
	return &a, nil
}
