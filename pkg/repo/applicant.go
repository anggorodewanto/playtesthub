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

// Applicant mirrors the applicant table in migration 0001. Admin vs.
// player visibility rules live in docs/schema.md; this struct carries
// every column — service-layer response builders are responsible for
// stripping fields before returning to the player (PRD §5.2 §5.4).
type Applicant struct {
	ID              uuid.UUID
	PlaytestID      uuid.UUID
	UserID          uuid.UUID
	DiscordHandle   string
	Platforms       []string
	NDAVersionHash  *string
	Status          string
	GrantedCodeID   *uuid.UUID
	ApprovedAt      *time.Time
	RejectionReason *string
	LastDMStatus    *string
	LastDMAttemptAt *time.Time
	LastDMError     *string
	CreatedAt       time.Time
}

// ApplicantStore is the data access surface for applicant rows.
type ApplicantStore interface {
	Insert(ctx context.Context, a *Applicant) (*Applicant, error)
	GetByID(ctx context.Context, id uuid.UUID) (*Applicant, error)
	GetByPlaytestUser(ctx context.Context, playtestID, userID uuid.UUID) (*Applicant, error)
	ListByPlaytest(ctx context.Context, playtestID uuid.UUID, status string) ([]*Applicant, error)
	UpdateStatus(ctx context.Context, a *Applicant) (*Applicant, error)
}

type PgApplicantStore struct {
	pool *pgxpool.Pool
}

func NewPgApplicantStore(pool *pgxpool.Pool) *PgApplicantStore {
	return &PgApplicantStore{pool: pool}
}

const applicantColumns = `
	id, playtest_id, user_id, discord_handle, platforms,
	nda_version_hash, status, granted_code_id, approved_at,
	rejection_reason, last_dm_status, last_dm_attempt_at,
	last_dm_error, created_at`

// Insert creates an applicant row. Hits the UNIQUE (playtest_id,
// user_id) index on re-signup; the service layer (phase 7) is expected
// to catch ErrUniqueViolation and resolve via GetByPlaytestUser for
// idempotency (PRD §5.2).
func (s *PgApplicantStore) Insert(ctx context.Context, a *Applicant) (*Applicant, error) {
	const sql = `
		INSERT INTO applicant (
			playtest_id, user_id, discord_handle, platforms, nda_version_hash
		)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING ` + applicantColumns

	row := s.pool.QueryRow(ctx, sql,
		a.PlaytestID,
		a.UserID,
		a.DiscordHandle,
		a.Platforms,
		stringPtr(a.NDAVersionHash),
	)
	got, err := scanApplicant(row)
	if err != nil {
		return nil, fmt.Errorf("inserting applicant: %w", classifyPgError(err))
	}
	return got, nil
}

func (s *PgApplicantStore) GetByID(ctx context.Context, id uuid.UUID) (*Applicant, error) {
	const sql = `SELECT ` + applicantColumns + ` FROM applicant WHERE id = $1`
	row := s.pool.QueryRow(ctx, sql, id)
	got, err := scanApplicant(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("fetching applicant by id: %w", err)
	}
	return got, nil
}

func (s *PgApplicantStore) GetByPlaytestUser(ctx context.Context, playtestID, userID uuid.UUID) (*Applicant, error) {
	const sql = `SELECT ` + applicantColumns + `
	               FROM applicant
	              WHERE playtest_id = $1 AND user_id = $2`
	row := s.pool.QueryRow(ctx, sql, playtestID, userID)
	got, err := scanApplicant(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("fetching applicant by (playtest,user): %w", err)
	}
	return got, nil
}

// ListByPlaytest powers the admin applicants queue (PRD §5.4). An
// empty status argument returns all rows; the applicant_queue_idx
// supports filtering + the DESC ordering.
func (s *PgApplicantStore) ListByPlaytest(ctx context.Context, playtestID uuid.UUID, status string) ([]*Applicant, error) {
	sql := `SELECT ` + applicantColumns + ` FROM applicant WHERE playtest_id = $1`
	args := []any{playtestID}
	if status != "" {
		sql += ` AND status = $2`
		args = append(args, status)
	}
	sql += ` ORDER BY created_at DESC, id ASC`

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("listing applicants: %w", err)
	}
	defer rows.Close()

	out := make([]*Applicant, 0)
	for rows.Next() {
		a, scanErr := scanApplicant(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scanning applicant row: %w", scanErr)
		}
		out = append(out, a)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterating applicant rows: %w", rowsErr)
	}
	return out, nil
}

// UpdateStatus rewrites status + the grant/rejection/DM attribution
// fields for an applicant row. The DB-level CHECK constraints enforce
// the enum values; state-machine legality is the service layer's
// concern (PRD §5.4 — APPROVED and REJECTED are terminal).
func (s *PgApplicantStore) UpdateStatus(ctx context.Context, a *Applicant) (*Applicant, error) {
	const sql = `
		UPDATE applicant
		   SET status = $2,
		       granted_code_id = $3,
		       approved_at = $4,
		       rejection_reason = $5,
		       last_dm_status = $6,
		       last_dm_attempt_at = $7,
		       last_dm_error = $8
		 WHERE id = $1
		RETURNING ` + applicantColumns

	row := s.pool.QueryRow(ctx, sql,
		a.ID,
		a.Status,
		uuidPtr(a.GrantedCodeID),
		timePtr(a.ApprovedAt),
		stringPtr(a.RejectionReason),
		stringPtr(a.LastDMStatus),
		timePtr(a.LastDMAttemptAt),
		stringPtr(a.LastDMError),
	)
	got, err := scanApplicant(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("updating applicant status: %w", classifyPgError(err))
	}
	return got, nil
}

func scanApplicant(row pgx.Row) (*Applicant, error) {
	var (
		a             Applicant
		ndaHash       pgtype.Text
		grantedCode   pgtype.UUID
		approvedAt    pgtype.Timestamptz
		rejReason     pgtype.Text
		lastDMStatus  pgtype.Text
		lastDMAttempt pgtype.Timestamptz
		lastDMError   pgtype.Text
	)
	err := row.Scan(
		&a.ID,
		&a.PlaytestID,
		&a.UserID,
		&a.DiscordHandle,
		&a.Platforms,
		&ndaHash,
		&a.Status,
		&grantedCode,
		&approvedAt,
		&rejReason,
		&lastDMStatus,
		&lastDMAttempt,
		&lastDMError,
		&a.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	if ndaHash.Valid {
		v := ndaHash.String
		a.NDAVersionHash = &v
	}
	if grantedCode.Valid {
		id := uuid.UUID(grantedCode.Bytes)
		a.GrantedCodeID = &id
	}
	if approvedAt.Valid {
		t := approvedAt.Time
		a.ApprovedAt = &t
	}
	if rejReason.Valid {
		v := rejReason.String
		a.RejectionReason = &v
	}
	if lastDMStatus.Valid {
		v := lastDMStatus.String
		a.LastDMStatus = &v
	}
	if lastDMAttempt.Valid {
		t := lastDMAttempt.Time
		a.LastDMAttemptAt = &t
	}
	if lastDMError.Valid {
		v := lastDMError.String
		a.LastDMError = &v
	}
	return &a, nil
}
