package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Survey mirrors the survey table from migration 0003. Questions is
// opaque JSONB at the repo layer — typed marshalling lives in the
// service layer (per docs/schema.md §"Survey entity spec"; same
// discipline as AuditLog.{Before,After}). The repo stores whatever
// JSONB the caller hands it.
type Survey struct {
	ID         uuid.UUID
	PlaytestID uuid.UUID
	Version    int
	Questions  json.RawMessage
	CreatedAt  time.Time
}

// SurveyResponse mirrors the survey_response table. UNIQUE (playtest_id,
// user_id) on the table enforces PRD §5.6's one-shot rule across every
// survey version — a player who answered v1 cannot resubmit against v2.
type SurveyResponse struct {
	ID          uuid.UUID
	PlaytestID  uuid.UUID
	UserID      uuid.UUID
	SurveyID    uuid.UUID
	Answers     json.RawMessage
	SubmittedAt time.Time
}

// SurveyResponsePage carries one page of survey_response rows + the
// opaque cursor to the next page. NextPageToken is empty on the final
// page.
type SurveyResponsePage struct {
	Rows          []*SurveyResponse
	NextPageToken string
}

// SurveyResponsePageQuery is the filter + pagination input shared by
// the ListResponses interface and its in-memory test fake. Limit ≤ 0
// picks the store-side default (50, matching PRD §6 Pagination).
type SurveyResponsePageQuery struct {
	PlaytestID uuid.UUID
	SurveyID   uuid.UUID // uuid.Nil → no filter (every version)
	PageToken  string    // "" → start of stream
	Limit      int       // ≤0 → 50
}

// SurveyStore is the data access surface for the per-version survey
// rows. EditAsNewVersion is responsible for the version bump — callers
// never write `version` directly.
type SurveyStore interface {
	// Create inserts version=1 for the playtest. Returns
	// ErrUniqueViolation when a survey already exists for the
	// playtest (the second-Create-call case from PRD §4.7); the
	// service layer maps that to AlreadyExists.
	Create(ctx context.Context, playtestID uuid.UUID, questions json.RawMessage) (*Survey, error)
	// EditAsNewVersion writes a new survey row with version =
	// previous + 1. Atomic: opens a tx, takes the per-playtest
	// pg_advisory_xact_lock so two concurrent edits cannot both
	// observe the same `previous` and write conflicting rows. Returns
	// ErrNotFound when no previous version exists.
	EditAsNewVersion(ctx context.Context, playtestID uuid.UUID, questions json.RawMessage) (*Survey, error)
	// GetCurrent returns the latest version for a playtest, or
	// ErrNotFound when no survey has been created.
	GetCurrent(ctx context.Context, playtestID uuid.UUID) (*Survey, error)
	// GetByID returns a single survey row by primary key, or
	// ErrNotFound. Powers the SubmitSurveyResponse path where the
	// service must validate the client-submitted surveyId belongs to
	// the playtest before recording the response.
	GetByID(ctx context.Context, surveyID uuid.UUID) (*Survey, error)
}

type PgSurveyStore struct {
	pool *pgxpool.Pool
}

func NewPgSurveyStore(pool *pgxpool.Pool) *PgSurveyStore {
	return &PgSurveyStore{pool: pool}
}

const surveyColumns = `id, playtest_id, version, questions, created_at`

func (s *PgSurveyStore) Create(ctx context.Context, playtestID uuid.UUID, questions json.RawMessage) (*Survey, error) {
	const sql = `
		INSERT INTO survey (playtest_id, version, questions)
		VALUES ($1, 1, COALESCE($2, '[]'::jsonb))
		RETURNING ` + surveyColumns

	row := s.pool.QueryRow(ctx, sql, playtestID, questionsArg(questions))
	got, err := scanSurvey(row)
	if err != nil {
		return nil, fmt.Errorf("creating survey: %w", classifyPgError(err))
	}
	return got, nil
}

// EditAsNewVersion bumps the version under a per-playtest advisory
// lock. The lock serialises with concurrent edits; the
// `version_uniq` index is the second-line guard if the lock is ever
// bypassed.
func (s *PgSurveyStore) EditAsNewVersion(ctx context.Context, playtestID uuid.UUID, questions json.RawMessage) (*Survey, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("beginning survey edit tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "survey:"+playtestID.String()); err != nil {
		return nil, fmt.Errorf("acquiring survey edit advisory lock: %w", err)
	}

	var prev int
	err = tx.QueryRow(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM survey WHERE playtest_id = $1`,
		playtestID,
	).Scan(&prev)
	if err != nil {
		return nil, fmt.Errorf("reading previous survey version: %w", err)
	}
	if prev == 0 {
		return nil, ErrNotFound
	}

	const insertSQL = `
		INSERT INTO survey (playtest_id, version, questions)
		VALUES ($1, $2, COALESCE($3, '[]'::jsonb))
		RETURNING ` + surveyColumns

	row := tx.QueryRow(ctx, insertSQL, playtestID, prev+1, questionsArg(questions))
	got, scanErr := scanSurvey(row)
	if scanErr != nil {
		return nil, fmt.Errorf("inserting next survey version: %w", classifyPgError(scanErr))
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("committing survey edit tx: %w", err)
	}
	return got, nil
}

func (s *PgSurveyStore) GetCurrent(ctx context.Context, playtestID uuid.UUID) (*Survey, error) {
	const sql = `SELECT ` + surveyColumns + `
	               FROM survey
	              WHERE playtest_id = $1
	              ORDER BY version DESC
	              LIMIT 1`
	row := s.pool.QueryRow(ctx, sql, playtestID)
	got, err := scanSurvey(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("fetching current survey: %w", err)
	}
	return got, nil
}

func (s *PgSurveyStore) GetByID(ctx context.Context, surveyID uuid.UUID) (*Survey, error) {
	const sql = `SELECT ` + surveyColumns + ` FROM survey WHERE id = $1`
	row := s.pool.QueryRow(ctx, sql, surveyID)
	got, err := scanSurvey(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("fetching survey by id: %w", err)
	}
	return got, nil
}

func scanSurvey(row pgx.Row) (*Survey, error) {
	var (
		s         Survey
		questions []byte
	)
	if err := row.Scan(&s.ID, &s.PlaytestID, &s.Version, &questions, &s.CreatedAt); err != nil {
		return nil, err
	}
	s.Questions = append(json.RawMessage{}, questions...)
	return &s, nil
}

// questionsArg lets the SQL COALESCE the JSONB default when the caller
// hands in an empty payload. pgx maps a typed-nil []byte to NULL,
// which COALESCE then resolves to '[]'::jsonb.
func questionsArg(q json.RawMessage) any {
	if len(q) == 0 {
		return nil
	}
	return []byte(q)
}

// SurveyResponseStore is the append-only one-shot ledger for survey
// submissions. Rows are never updated or deleted (PRD §5.6).
type SurveyResponseStore interface {
	// SubmitOnce inserts a single response. Returns the existing row
	// + true when (playtest_id, user_id) already has a submission;
	// the service layer maps that to AlreadyExists per errors.md
	// row 31. The returned row's surveyId is the *original*
	// submitted version, not the one the caller passed — callers
	// rendering the read-only "thanks, response recorded" view
	// should still surface an empty body per PRD §5.6.
	SubmitOnce(ctx context.Context, r *SurveyResponse) (*SurveyResponse, bool, error)
	// GetByPlaytestUser fetches the (playtestId, userId) submission,
	// or ErrNotFound. Powers the "did this player submit yet" check
	// for the player's gating UI.
	GetByPlaytestUser(ctx context.Context, playtestID, userID uuid.UUID) (*SurveyResponse, error)
	// ListResponses returns one page of responses ordered by
	// (submitted_at, id) DESC, DESC — distinct from the audit-log
	// (created_at, id) shape. Optional surveyId filter for the
	// per-version aggregate split (PRD §5.6).
	ListResponses(ctx context.Context, q SurveyResponsePageQuery) (*SurveyResponsePage, error)
}

type PgSurveyResponseStore struct {
	pool *pgxpool.Pool
}

func NewPgSurveyResponseStore(pool *pgxpool.Pool) *PgSurveyResponseStore {
	return &PgSurveyResponseStore{pool: pool}
}

const surveyResponseColumns = `id, playtest_id, user_id, survey_id, answers, submitted_at`

// SubmitOnce uses INSERT ... ON CONFLICT DO NOTHING + a follow-up
// SELECT on the natural key. The conflict path returns the original
// row unchanged — re-submits never overwrite the original answers.
func (s *PgSurveyResponseStore) SubmitOnce(ctx context.Context, r *SurveyResponse) (*SurveyResponse, bool, error) {
	const insertSQL = `
		INSERT INTO survey_response (playtest_id, user_id, survey_id, answers)
		VALUES ($1, $2, $3, COALESCE($4, '{}'::jsonb))
		ON CONFLICT (playtest_id, user_id) DO NOTHING
		RETURNING ` + surveyResponseColumns

	row := s.pool.QueryRow(ctx, insertSQL, r.PlaytestID, r.UserID, r.SurveyID, answersArg(r.Answers))
	got, err := scanSurveyResponse(row)
	if err == nil {
		return got, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, false, fmt.Errorf("inserting survey response: %w", classifyPgError(err))
	}

	existing, getErr := s.GetByPlaytestUser(ctx, r.PlaytestID, r.UserID)
	if getErr != nil {
		return nil, false, fmt.Errorf("fetching existing survey response: %w", getErr)
	}
	return existing, true, nil
}

func (s *PgSurveyResponseStore) GetByPlaytestUser(ctx context.Context, playtestID, userID uuid.UUID) (*SurveyResponse, error) {
	const sql = `SELECT ` + surveyResponseColumns + `
	               FROM survey_response
	              WHERE playtest_id = $1 AND user_id = $2`
	row := s.pool.QueryRow(ctx, sql, playtestID, userID)
	got, err := scanSurveyResponse(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("fetching survey response: %w", err)
	}
	return got, nil
}

// ErrInvalidSurveyResponseToken is surfaced when the opaque page_token
// does not decode into a (submittedAt, id) pair.
var ErrInvalidSurveyResponseToken = errors.New("repo: invalid survey response page token")

func (s *PgSurveyResponseStore) ListResponses(ctx context.Context, q SurveyResponsePageQuery) (*SurveyResponsePage, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = ListPagedDefaultLimit
	}
	if limit > ListPagedMaxLimit {
		limit = ListPagedMaxLimit
	}

	cursor, err := decodeSurveyResponseToken(q.PageToken)
	if err != nil {
		return nil, err
	}

	sql := `SELECT ` + surveyResponseColumns + ` FROM survey_response WHERE playtest_id = $1`
	args := []any{q.PlaytestID}
	idx := 2
	if q.SurveyID != uuid.Nil {
		sql += fmt.Sprintf(` AND survey_id = $%d`, idx)
		args = append(args, q.SurveyID)
		idx++
	}
	if cursor != nil {
		sql += fmt.Sprintf(` AND (submitted_at, id) < ($%d, $%d)`, idx, idx+1)
		args = append(args, cursor.SubmittedAt, cursor.ID)
		idx += 2
	}
	sql += fmt.Sprintf(` ORDER BY submitted_at DESC, id DESC LIMIT $%d`, idx)
	args = append(args, limit+1)

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("listing survey responses: %w", err)
	}
	defer rows.Close()

	out := make([]*SurveyResponse, 0, limit)
	for rows.Next() {
		r, scanErr := scanSurveyResponse(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scanning survey response row: %w", scanErr)
		}
		out = append(out, r)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterating survey response rows: %w", rowsErr)
	}

	page := &SurveyResponsePage{}
	if len(out) > limit {
		last := out[limit-1]
		page.Rows = out[:limit]
		page.NextPageToken = encodeSurveyResponseToken(surveyResponseCursor{SubmittedAt: last.SubmittedAt, ID: last.ID})
		return page, nil
	}
	page.Rows = out
	return page, nil
}

type surveyResponseCursor struct {
	SubmittedAt time.Time `json:"s"`
	ID          uuid.UUID `json:"i"`
}

func encodeSurveyResponseToken(c surveyResponseCursor) string {
	return encodePageCursor(c)
}

func decodeSurveyResponseToken(token string) (*surveyResponseCursor, error) {
	return decodePageCursor(token, func(c *surveyResponseCursor) uuid.UUID { return c.ID }, ErrInvalidSurveyResponseToken)
}

func scanSurveyResponse(row pgx.Row) (*SurveyResponse, error) {
	var (
		r       SurveyResponse
		answers []byte
	)
	if err := row.Scan(&r.ID, &r.PlaytestID, &r.UserID, &r.SurveyID, &answers, &r.SubmittedAt); err != nil {
		return nil, err
	}
	r.Answers = append(json.RawMessage{}, answers...)
	return &r, nil
}

func answersArg(a json.RawMessage) any {
	if len(a) == 0 {
		return nil
	}
	return []byte(a)
}
