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

// ADTLinkage mirrors the adt_linkage table in migration 0006. Identity
// columns only — no credential payload exists (PRD §4.8.2 / schema.md
// §"adt_linkage table"). DeletedAt is nil on live rows; soft-deleted
// rows are preserved for audit chain integrity.
type ADTLinkage struct {
	ID              uuid.UUID
	StudioNamespace string
	ADTNamespace    string
	LinkedByUserID  uuid.UUID
	LinkedAt        time.Time
	DeletedAt       *time.Time
}

// ADTLinkPending mirrors the adt_link_pending nonce store (schema.md
// §"adt_link_pending table"). One row per in-flight StartADTLink;
// consumed by the matching CompleteADTLink or swept inline once
// expired.
type ADTLinkPending struct {
	State           string
	StudioNamespace string
	StartedByUserID uuid.UUID
	ExpiresAt       time.Time
}

// ADTLinkageStore is the data access surface for the ADT linkage
// tables. Service-layer handlers depend on the interface; tests at the
// service layer mock it.
type ADTLinkageStore interface {
	// InsertPending stores a fresh adt_link_pending row. Caller mints
	// the state nonce and computes ExpiresAt from
	// ADT_LINKAGE_PENDING_TTL_SECONDS.
	InsertPending(ctx context.Context, p *ADTLinkPending) error

	// ConsumePending atomically reads the pending row matching state,
	// deletes it, and sweeps every other row with expires_at < now.
	// Returns ErrNotFound when no row matches or the row has expired —
	// callers MUST NOT distinguish the two for the
	// docs/errors.md CompleteADTLink invalid-state contract.
	ConsumePending(ctx context.Context, state string, now time.Time) (*ADTLinkPending, error)

	// Insert creates a new adt_linkage row. UNIQUE constraint
	// violations surface as ErrUniqueViolation so the service can map
	// the conflict back to the existing live row.
	Insert(ctx context.Context, l *ADTLinkage) (*ADTLinkage, error)

	// GetLive returns the single live (not soft-deleted) linkage for
	// the given (studio, adt) pair. ErrNotFound when none.
	GetLive(ctx context.Context, studioNamespace, adtNamespace string) (*ADTLinkage, error)

	// ListLive returns every live linkage scoped to the caller's
	// studio namespace, ordered by linked_at DESC.
	ListLive(ctx context.Context, studioNamespace string) ([]*ADTLinkage, error)

	// GetByID returns the linkage row matching id within the caller's
	// studio namespace, regardless of soft-delete state. ErrNotFound
	// when the id is unknown OR the row belongs to another studio.
	GetByID(ctx context.Context, studioNamespace string, id uuid.UUID) (*ADTLinkage, error)

	// SoftDelete sets deleted_at on the row identified by id within
	// the caller's studio namespace. ErrNotFound when the id is
	// unknown OR the row belongs to another studio. Idempotent: a
	// second call against an already soft-deleted row returns nil and
	// leaves the existing deleted_at untouched.
	SoftDelete(ctx context.Context, studioNamespace string, id uuid.UUID) error
}

// PgADTLinkageStore is the Postgres-backed ADTLinkageStore.
type PgADTLinkageStore struct {
	pool *pgxpool.Pool
}

func NewPgADTLinkageStore(pool *pgxpool.Pool) *PgADTLinkageStore {
	return &PgADTLinkageStore{pool: pool}
}

const adtLinkageColumns = `id, studio_namespace, adt_namespace, linked_by_user_id, linked_at, deleted_at`

func (s *PgADTLinkageStore) InsertPending(ctx context.Context, p *ADTLinkPending) error {
	const sql = `
		INSERT INTO adt_link_pending (state, studio_namespace, started_by_user_id, expires_at)
		VALUES ($1, $2, $3, $4)`
	if _, err := s.pool.Exec(ctx, sql, p.State, p.StudioNamespace, p.StartedByUserID, p.ExpiresAt); err != nil {
		return fmt.Errorf("inserting adt_link_pending: %w", classifyPgError(err))
	}
	return nil
}

func (s *PgADTLinkageStore) ConsumePending(ctx context.Context, state string, now time.Time) (*ADTLinkPending, error) {
	// One round-trip: sweep every expired row and delete-by-state in a
	// CTE so the consumer + sweep share a snapshot. RETURNING surfaces
	// the consumed row when the state matched a non-expired row.
	const sql = `
		WITH swept AS (
			DELETE FROM adt_link_pending WHERE expires_at < $2
		)
		DELETE FROM adt_link_pending
		WHERE state = $1 AND expires_at >= $2
		RETURNING state, studio_namespace, started_by_user_id, expires_at`
	row := s.pool.QueryRow(ctx, sql, state, now)
	out := &ADTLinkPending{}
	if err := row.Scan(&out.State, &out.StudioNamespace, &out.StartedByUserID, &out.ExpiresAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("consuming adt_link_pending: %w", err)
	}
	return out, nil
}

func (s *PgADTLinkageStore) Insert(ctx context.Context, l *ADTLinkage) (*ADTLinkage, error) {
	const sql = `
		INSERT INTO adt_linkage (studio_namespace, adt_namespace, linked_by_user_id)
		VALUES ($1, $2, $3)
		RETURNING ` + adtLinkageColumns
	row := s.pool.QueryRow(ctx, sql, l.StudioNamespace, l.ADTNamespace, l.LinkedByUserID)
	got, err := scanADTLinkage(row)
	if err != nil {
		return nil, fmt.Errorf("inserting adt_linkage: %w", classifyPgError(err))
	}
	return got, nil
}

func (s *PgADTLinkageStore) GetLive(ctx context.Context, studioNamespace, adtNamespace string) (*ADTLinkage, error) {
	const sql = `
		SELECT ` + adtLinkageColumns + `
		  FROM adt_linkage
		 WHERE studio_namespace = $1
		   AND adt_namespace = $2
		   AND deleted_at IS NULL`
	row := s.pool.QueryRow(ctx, sql, studioNamespace, adtNamespace)
	got, err := scanADTLinkage(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("fetching adt_linkage: %w", err)
	}
	return got, nil
}

func (s *PgADTLinkageStore) ListLive(ctx context.Context, studioNamespace string) ([]*ADTLinkage, error) {
	const sql = `
		SELECT ` + adtLinkageColumns + `
		  FROM adt_linkage
		 WHERE studio_namespace = $1
		   AND deleted_at IS NULL
		 ORDER BY linked_at DESC, id ASC`
	rows, err := s.pool.Query(ctx, sql, studioNamespace)
	if err != nil {
		return nil, fmt.Errorf("listing adt_linkages: %w", err)
	}
	defer rows.Close()

	out := make([]*ADTLinkage, 0)
	for rows.Next() {
		l, scanErr := scanADTLinkage(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scanning adt_linkage row: %w", scanErr)
		}
		out = append(out, l)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterating adt_linkage rows: %w", rowsErr)
	}
	return out, nil
}

func (s *PgADTLinkageStore) GetByID(ctx context.Context, studioNamespace string, id uuid.UUID) (*ADTLinkage, error) {
	const sql = `
		SELECT ` + adtLinkageColumns + `
		  FROM adt_linkage
		 WHERE id = $1 AND studio_namespace = $2`
	row := s.pool.QueryRow(ctx, sql, id, studioNamespace)
	got, err := scanADTLinkage(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("fetching adt_linkage by id: %w", err)
	}
	return got, nil
}

func (s *PgADTLinkageStore) SoftDelete(ctx context.Context, studioNamespace string, id uuid.UUID) error {
	const sql = `
		UPDATE adt_linkage
		   SET deleted_at = COALESCE(deleted_at, NOW())
		 WHERE id = $1 AND studio_namespace = $2`
	tag, err := s.pool.Exec(ctx, sql, id, studioNamespace)
	if err != nil {
		return fmt.Errorf("soft-deleting adt_linkage: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanADTLinkage(row pgx.Row) (*ADTLinkage, error) {
	out := &ADTLinkage{}
	var deletedAt *time.Time
	if err := row.Scan(&out.ID, &out.StudioNamespace, &out.ADTNamespace, &out.LinkedByUserID, &out.LinkedAt, &deletedAt); err != nil {
		return nil, err
	}
	out.DeletedAt = deletedAt
	return out, nil
}
