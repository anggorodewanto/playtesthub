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

// Playtest mirrors the playtest table in migration 0001. Field-level
// semantics live in docs/PRD.md §5.1 and docs/schema.md; this struct
// carries the wire shape only. Pointer-typed fields are nullable in
// the DB; non-pointer fields are NOT NULL (or have a DB default).
type Playtest struct {
	ID                    uuid.UUID
	Namespace             string
	Slug                  string
	Title                 string
	Description           string
	BannerImageURL        string
	Platforms             []string
	StartsAt              *time.Time
	EndsAt                *time.Time
	Status                string
	NDARequired           bool
	NDAText               string
	CurrentNDAVersionHash string
	SurveyID              *uuid.UUID
	DistributionModel     string
	AGSItemID             *string
	AGSCampaignID         *string
	InitialCodeQuantity   *int32
	CreatedAt             time.Time
	UpdatedAt             time.Time
	DeletedAt             *time.Time
}

// PlaytestStore is the data access surface the service layer depends on.
// Mocks for unit tests (phase 6) are generated against this interface;
// PgPlaytestStore is the only production implementation.
type PlaytestStore interface {
	Create(ctx context.Context, p *Playtest) (*Playtest, error)
	GetByID(ctx context.Context, namespace string, id uuid.UUID) (*Playtest, error)
	GetBySlug(ctx context.Context, namespace, slug string) (*Playtest, error)
	List(ctx context.Context, namespace string, includeDeleted bool) ([]*Playtest, error)
	Update(ctx context.Context, p *Playtest) (*Playtest, error)
	SoftDelete(ctx context.Context, namespace string, id uuid.UUID) error
	TransitionStatus(ctx context.Context, namespace string, id uuid.UUID, from, to string) (*Playtest, error)
}

// PgPlaytestStore is the Postgres-backed PlaytestStore.
type PgPlaytestStore struct {
	pool *pgxpool.Pool
}

func NewPgPlaytestStore(pool *pgxpool.Pool) *PgPlaytestStore {
	return &PgPlaytestStore{pool: pool}
}

const playtestColumns = `
	id, namespace, slug, title, description, banner_image_url, platforms,
	starts_at, ends_at, status, nda_required, nda_text,
	current_nda_version_hash, survey_id, distribution_model,
	ags_item_id, ags_campaign_id, initial_code_quantity,
	created_at, updated_at, deleted_at`

// Create inserts a new playtest row. The caller supplies business
// fields; id, created_at, and updated_at are assigned by Postgres. The
// fully populated row (including DB-assigned values) is returned.
func (s *PgPlaytestStore) Create(ctx context.Context, p *Playtest) (*Playtest, error) {
	const sql = `
		INSERT INTO playtest (
			namespace, slug, title, description, banner_image_url, platforms,
			starts_at, ends_at, status, nda_required, nda_text,
			current_nda_version_hash, survey_id, distribution_model,
			ags_item_id, ags_campaign_id, initial_code_quantity
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		RETURNING ` + playtestColumns

	row := s.pool.QueryRow(ctx, sql,
		p.Namespace,
		p.Slug,
		p.Title,
		p.Description,
		p.BannerImageURL,
		p.Platforms,
		timePtr(p.StartsAt),
		timePtr(p.EndsAt),
		nonEmptyOr(p.Status, "DRAFT"),
		p.NDARequired,
		p.NDAText,
		p.CurrentNDAVersionHash,
		uuidPtr(p.SurveyID),
		p.DistributionModel,
		stringPtr(p.AGSItemID),
		stringPtr(p.AGSCampaignID),
		int32Ptr(p.InitialCodeQuantity),
	)
	got, err := scanPlaytest(row)
	if err != nil {
		return nil, fmt.Errorf("creating playtest: %w", classifyPgError(err))
	}
	return got, nil
}

func (s *PgPlaytestStore) GetByID(ctx context.Context, namespace string, id uuid.UUID) (*Playtest, error) {
	const sql = `SELECT ` + playtestColumns + ` FROM playtest WHERE namespace = $1 AND id = $2`
	row := s.pool.QueryRow(ctx, sql, namespace, id)
	got, err := scanPlaytest(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("fetching playtest by id: %w", err)
	}
	return got, nil
}

func (s *PgPlaytestStore) GetBySlug(ctx context.Context, namespace, slug string) (*Playtest, error) {
	const sql = `SELECT ` + playtestColumns + ` FROM playtest WHERE namespace = $1 AND slug = $2`
	row := s.pool.QueryRow(ctx, sql, namespace, slug)
	got, err := scanPlaytest(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("fetching playtest by slug: %w", err)
	}
	return got, nil
}

// List returns every playtest in the namespace, newest first. When
// includeDeleted is false, soft-deleted rows are omitted — the default
// for admin list pages (PRD §5.1).
func (s *PgPlaytestStore) List(ctx context.Context, namespace string, includeDeleted bool) ([]*Playtest, error) {
	sql := `SELECT ` + playtestColumns + ` FROM playtest WHERE namespace = $1`
	if !includeDeleted {
		sql += ` AND deleted_at IS NULL`
	}
	sql += ` ORDER BY created_at DESC, id ASC`

	rows, err := s.pool.Query(ctx, sql, namespace)
	if err != nil {
		return nil, fmt.Errorf("listing playtests: %w", err)
	}
	defer rows.Close()

	out := make([]*Playtest, 0)
	for rows.Next() {
		p, scanErr := scanPlaytest(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scanning playtest row: %w", scanErr)
		}
		out = append(out, p)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterating playtest rows: %w", rowsErr)
	}
	return out, nil
}

// Update rewrites the mutable field set on an existing playtest. The
// service layer is responsible for enforcing the immutable-field
// whitelist (PRD §5.1; docs/errors.md EditPlaytest row); Update here
// unconditionally sets every column in its WHERE clause. It refuses to
// modify soft-deleted rows (returns ErrNotFound).
func (s *PgPlaytestStore) Update(ctx context.Context, p *Playtest) (*Playtest, error) {
	const sql = `
		UPDATE playtest
		   SET title = $3,
		       description = $4,
		       banner_image_url = $5,
		       platforms = $6,
		       starts_at = $7,
		       ends_at = $8,
		       nda_required = $9,
		       nda_text = $10,
		       current_nda_version_hash = $11,
		       survey_id = $12,
		       updated_at = NOW()
		 WHERE namespace = $1
		   AND id = $2
		   AND deleted_at IS NULL
		RETURNING ` + playtestColumns

	row := s.pool.QueryRow(ctx, sql,
		p.Namespace,
		p.ID,
		p.Title,
		p.Description,
		p.BannerImageURL,
		p.Platforms,
		timePtr(p.StartsAt),
		timePtr(p.EndsAt),
		p.NDARequired,
		p.NDAText,
		p.CurrentNDAVersionHash,
		uuidPtr(p.SurveyID),
	)
	got, err := scanPlaytest(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("updating playtest: %w", classifyPgError(err))
	}
	return got, nil
}

// SoftDelete marks the row deleted. Subsequent Create calls that reuse
// the slug fail with ErrUniqueViolation (PRD §5.1 slug uniqueness
// spans live and soft-deleted rows).
func (s *PgPlaytestStore) SoftDelete(ctx context.Context, namespace string, id uuid.UUID) error {
	const sql = `
		UPDATE playtest
		   SET deleted_at = NOW(),
		       updated_at = NOW()
		 WHERE namespace = $1
		   AND id = $2
		   AND deleted_at IS NULL
		RETURNING id`

	var got uuid.UUID
	err := s.pool.QueryRow(ctx, sql, namespace, id).Scan(&got)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("soft-deleting playtest: %w", err)
	}
	return nil
}

// TransitionStatus performs a compare-and-swap on the status column.
// It refuses to operate on soft-deleted rows. A zero-row update (the
// current status no longer matches `from`, or the row is deleted)
// returns ErrStatusCASMismatch so the service layer can distinguish
// race losses from missing rows.
func (s *PgPlaytestStore) TransitionStatus(ctx context.Context, namespace string, id uuid.UUID, from, to string) (*Playtest, error) {
	const sql = `
		UPDATE playtest
		   SET status = $4,
		       updated_at = NOW()
		 WHERE namespace = $1
		   AND id = $2
		   AND status = $3
		   AND deleted_at IS NULL
		RETURNING ` + playtestColumns

	row := s.pool.QueryRow(ctx, sql, namespace, id, from, to)
	got, err := scanPlaytest(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrStatusCASMismatch
	}
	if err != nil {
		return nil, fmt.Errorf("transitioning playtest status: %w", classifyPgError(err))
	}
	return got, nil
}

// scanPlaytest scans a single row (pgx.Row or a live pgx.Rows cursor)
// into a Playtest. Nullable columns are read through pgtype temporaries
// because *uuid.UUID / *string pointer-scan would need custom codecs.
func scanPlaytest(row pgx.Row) (*Playtest, error) {
	var (
		p          Playtest
		startsAt   pgtype.Timestamptz
		endsAt     pgtype.Timestamptz
		surveyID   pgtype.UUID
		agsItemID  pgtype.Text
		agsCampID  pgtype.Text
		initialQty pgtype.Int4
		deletedAt  pgtype.Timestamptz
	)
	err := row.Scan(
		&p.ID,
		&p.Namespace,
		&p.Slug,
		&p.Title,
		&p.Description,
		&p.BannerImageURL,
		&p.Platforms,
		&startsAt,
		&endsAt,
		&p.Status,
		&p.NDARequired,
		&p.NDAText,
		&p.CurrentNDAVersionHash,
		&surveyID,
		&p.DistributionModel,
		&agsItemID,
		&agsCampID,
		&initialQty,
		&p.CreatedAt,
		&p.UpdatedAt,
		&deletedAt,
	)
	if err != nil {
		return nil, err
	}
	if startsAt.Valid {
		t := startsAt.Time
		p.StartsAt = &t
	}
	if endsAt.Valid {
		t := endsAt.Time
		p.EndsAt = &t
	}
	if surveyID.Valid {
		id := uuid.UUID(surveyID.Bytes)
		p.SurveyID = &id
	}
	if agsItemID.Valid {
		v := agsItemID.String
		p.AGSItemID = &v
	}
	if agsCampID.Valid {
		v := agsCampID.String
		p.AGSCampaignID = &v
	}
	if initialQty.Valid {
		v := initialQty.Int32
		p.InitialCodeQuantity = &v
	}
	if deletedAt.Valid {
		t := deletedAt.Time
		p.DeletedAt = &t
	}
	return &p, nil
}

// ---- tiny pointer-to-nullable helpers ---------------------------------

func timePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

func stringPtr(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func int32Ptr(i *int32) any {
	if i == nil {
		return nil
	}
	return *i
}

func uuidPtr(u *uuid.UUID) any {
	if u == nil {
		return nil
	}
	return *u
}

func nonEmptyOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
