package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// applicantLastDMErrorMaxBytes mirrors the DB CHECK on
// applicant.last_dm_error in migration 0001 and the dm-queue.md rule:
// the persisted message is byte-truncated to 500 bytes preserving valid
// UTF-8 codepoint boundaries.
const applicantLastDMErrorMaxBytes = 500

// Applicant status enum values — keep in sync with applicant_status_enum
// CHECK in migration 0001.
const (
	ApplicantStatusPending  = "PENDING"
	ApplicantStatusApproved = "APPROVED"
	ApplicantStatusRejected = "REJECTED"
)

// Applicant mirrors the applicant table in migration 0001. Admin vs.
// player visibility rules live in docs/schema.md; this struct carries
// every column — service-layer response builders are responsible for
// stripping fields before returning to the player (PRD §5.2 §5.4).
type Applicant struct {
	ID            uuid.UUID
	PlaytestID    uuid.UUID
	UserID        uuid.UUID
	DiscordHandle string
	// DiscordUserID is the Discord snowflake harvested from the IAM
	// `platform_user_id` claim at signup. NULL for rows persisted
	// before migration 0004; the DM queue treats NULL as
	// `lastDmError='missing_recipient'` per docs/dm-queue.md.
	DiscordUserID   *string
	Platforms       []string
	NDAVersionHash  *string
	Status          string
	GrantedCodeID   *uuid.UUID
	ApprovedAt      *time.Time
	RejectionReason *string
	LastDMStatus    *string
	LastDMAttemptAt *time.Time
	LastDMError     *string
	// AutoApproved is true when this applicant was promoted via the
	// auto-approve signup chain (PRD §5.4 / M5.A) rather than a manual
	// ApproveApplicant click. Used by the cap query in Signup to count
	// existing auto-approvals against playtest.auto_approve_limit.
	AutoApproved bool
	CreatedAt    time.Time
}

// ApplicantPage carries one page of applicant rows + the opaque cursor
// to the next page. NextPageToken is empty on the final page.
type ApplicantPage struct {
	Rows          []*Applicant
	NextPageToken string
}

// ApplicantPageQuery is the filter + pagination input shared by the
// ListPaged interface and its in-memory test fake. Limit ≤ 0 picks the
// store-side default (50, matching PRD §6 Pagination).
type ApplicantPageQuery struct {
	PlaytestID   uuid.UUID
	Status       string // "" → no filter
	DMFailedOnly bool   // true → last_dm_status='failed'
	PageToken    string // "" → start of stream
	Limit        int    // ≤0 → 50
}

// ApplicantStore is the data access surface for applicant rows.
//
// data-access surface; splitting it just to satisfy the 10-method cap
// would scatter related methods across multiple interfaces and force
// every caller to compose them.
//
//nolint:interfacebloat // single source of truth for the applicant
type ApplicantStore interface {
	Insert(ctx context.Context, a *Applicant) (*Applicant, error)
	GetByID(ctx context.Context, id uuid.UUID) (*Applicant, error)
	GetByPlaytestUser(ctx context.Context, playtestID, userID uuid.UUID) (*Applicant, error)
	ListByPlaytest(ctx context.Context, playtestID uuid.UUID, status string) ([]*Applicant, error)
	// ListPaged powers the admin applicants queue (PRD §5.4 / errors
	// .md ListApplicants). Cursor pagination on (created_at DESC, id
	// DESC) — stable across inserts because id is the secondary key.
	// Returns ErrInvalidPageToken on a malformed token.
	ListPaged(ctx context.Context, q ApplicantPageQuery) (*ApplicantPage, error)
	UpdateStatus(ctx context.Context, a *Applicant) (*Applicant, error)
	// ApproveCAS performs the PENDING → APPROVED transition with grant
	// attribution (docs/schema.md §"Approve flow"). The Querier argument
	// is the transaction the caller has opened around the fenced code
	// finalize; the applicant update must run inside that same tx so
	// either both rows commit or neither does. Returns
	// ErrStatusCASMismatch when the row is no longer PENDING (the
	// "applicant already approved" race per errors.md row 11).
	// autoApproved sets applicant.auto_approved=true when the approve
	// flows through the M5.A signup-chain path; false leaves the column
	// at its DEFAULT (false) for manual ApproveApplicant clicks.
	ApproveCAS(ctx context.Context, q Querier, applicantID, codeID uuid.UUID, approvedAt time.Time, autoApproved bool) (*Applicant, error)
	// CountAutoApprovedByPlaytest returns the count of applicants where
	// auto_approved=true for the playtest. Runs inside the caller's
	// transaction so the playtest-scoped advisory lock the signup tx
	// holds serialises concurrent auto-approve attempts (PRD §5.4 /
	// M5.A). Backed by applicant_auto_approved_count_idx (migration
	// 0005).
	CountAutoApprovedByPlaytest(ctx context.Context, q Querier, playtestID uuid.UUID) (int, error)
	// RejectCAS is the terminal PENDING → REJECTED transition (PRD
	// §5.4). The reason is the admin-supplied free-text per errors.md;
	// nil rejects without a reason.
	RejectCAS(ctx context.Context, q Querier, applicantID uuid.UUID, reason *string) (*Applicant, error)
	// UpdateDMStatus stamps the DM attribution fields (PRD §5.4 / docs
	// /dm-queue.md). status is "sent" or "failed"; errMsg is preserved
	// verbatim for "sent" (typically nil) and byte-truncated to 500
	// UTF-8-safe bytes for "failed".
	UpdateDMStatus(ctx context.Context, applicantID uuid.UUID, status string, attemptAt time.Time, errMsg *string) (*Applicant, error)
	// SetNDAVersionHash overwrites Applicant.nda_version_hash on every
	// accept (idempotent re-accept on the same hash is a no-op write,
	// re-accept after an NDA edit advances the stored hash). Powers the
	// PRD §5.3 NdaReacceptRequired derived state.
	SetNDAVersionHash(ctx context.Context, applicantID uuid.UUID, hash string) (*Applicant, error)
	// ListLostDMOnRestart returns every APPROVED applicant in the
	// namespace whose last_dm_status is NULL or 'pending' — the
	// dm-queue.md "Restart behavior" idempotency guard. Rows already at
	// 'failed' are deliberately excluded so the original error reason
	// (e.g. dm_queue_overflow) is preserved across restarts.
	ListLostDMOnRestart(ctx context.Context, namespace string) ([]*Applicant, error)
	// ListDMFailedByPlaytest returns every APPROVED applicant for the
	// playtest whose last_dm_status is 'failed'. Used by RetryFailedDms
	// (PRD §5.5) — the bulk variant of RetryDM. Non-paginated: the
	// failed cohort is bounded by the queue cap (10k) which is also the
	// upper bound on what can be re-enqueued in one call.
	ListDMFailedByPlaytest(ctx context.Context, playtestID uuid.UUID) ([]*Applicant, error)
}

type PgApplicantStore struct {
	pool *pgxpool.Pool
}

func NewPgApplicantStore(pool *pgxpool.Pool) *PgApplicantStore {
	return &PgApplicantStore{pool: pool}
}

const applicantColumns = `
	id, playtest_id, user_id, discord_handle, discord_user_id, platforms,
	nda_version_hash, status, granted_code_id, approved_at,
	rejection_reason, last_dm_status, last_dm_attempt_at,
	last_dm_error, auto_approved, created_at`

// Insert creates an applicant row. Hits the UNIQUE (playtest_id,
// user_id) index on re-signup; the service layer (phase 7) is expected
// to catch ErrUniqueViolation and resolve via GetByPlaytestUser for
// idempotency (PRD §5.2).
func (s *PgApplicantStore) Insert(ctx context.Context, a *Applicant) (*Applicant, error) {
	const sql = `
		INSERT INTO applicant (
			playtest_id, user_id, discord_handle, discord_user_id, platforms, nda_version_hash
		)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING ` + applicantColumns

	row := s.pool.QueryRow(ctx, sql,
		a.PlaytestID,
		a.UserID,
		a.DiscordHandle,
		stringPtr(a.DiscordUserID),
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

// ListPagedDefaultLimit is the page size when the caller passes ≤0.
// Matches PRD §6 Pagination.
const ListPagedDefaultLimit = 50

// ListPagedMaxLimit caps the per-page count callers can request, so a
// hostile or buggy client cannot slurp the whole table in one call.
const ListPagedMaxLimit = 200

// ErrInvalidPageToken is surfaced by ListPaged when the opaque
// page_token does not decode into a (createdAt, id) pair.
var ErrInvalidPageToken = errors.New("repo: invalid page token")

// ListPaged returns one page of applicants ordered by (created_at,
// id) DESC, DESC. The opaque cursor encodes the last row's
// (created_at, id) tuple; the next call's WHERE clause is a tuple
// comparison against that cursor, which Postgres can satisfy from the
// composite index (created_at DESC, id DESC) without a sort.
func (s *PgApplicantStore) ListPaged(ctx context.Context, q ApplicantPageQuery) (*ApplicantPage, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = ListPagedDefaultLimit
	}
	if limit > ListPagedMaxLimit {
		limit = ListPagedMaxLimit
	}

	cursor, err := decodeApplicantPageToken(q.PageToken)
	if err != nil {
		return nil, err
	}

	sql := `SELECT ` + applicantColumns + ` FROM applicant WHERE playtest_id = $1`
	args := []any{q.PlaytestID}
	idx := 2
	if q.Status != "" {
		sql += fmt.Sprintf(` AND status = $%d`, idx)
		args = append(args, q.Status)
		idx++
	}
	if q.DMFailedOnly {
		sql += ` AND last_dm_status = 'failed'`
	}
	if cursor != nil {
		sql += fmt.Sprintf(` AND (created_at, id) < ($%d, $%d)`, idx, idx+1)
		args = append(args, cursor.CreatedAt, cursor.ID)
		idx += 2
	}
	sql += fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d`, idx)
	args = append(args, limit+1)

	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("listing applicants paged: %w", err)
	}
	defer rows.Close()

	out := make([]*Applicant, 0, limit)
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

	page := &ApplicantPage{}
	if len(out) > limit {
		last := out[limit-1]
		page.Rows = out[:limit]
		page.NextPageToken = encodeApplicantPageToken(applicantCursor{CreatedAt: last.CreatedAt, ID: last.ID})
		return page, nil
	}
	page.Rows = out
	return page, nil
}

// applicantCursor is the JSON-serialised opaque cursor exchanged with
// clients via page_token. Field names are short to keep the
// base64-encoded blob compact.
type applicantCursor struct {
	CreatedAt time.Time `json:"c"`
	ID        uuid.UUID `json:"i"`
}

func encodeApplicantPageToken(c applicantCursor) string {
	return encodePageCursor(c)
}

func decodeApplicantPageToken(token string) (*applicantCursor, error) {
	return decodePageCursor(token, func(c *applicantCursor) uuid.UUID { return c.ID }, ErrInvalidPageToken)
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

// ApproveCAS runs inside the caller's transaction. The CAS is the
// status='PENDING' guard — when two admins click Approve on the same
// row, only one UPDATE returns a row; the other gets pgx.ErrNoRows
// surfaced as ErrStatusCASMismatch. autoApproved is propagated to the
// applicant.auto_approved column so M5.A's signup-chain promotions are
// distinguishable from manual ApproveApplicant clicks (the cap query
// counts only auto_approved=true rows).
func (s *PgApplicantStore) ApproveCAS(ctx context.Context, q Querier, applicantID, codeID uuid.UUID, approvedAt time.Time, autoApproved bool) (*Applicant, error) {
	const sql = `
		UPDATE applicant
		   SET status = 'APPROVED',
		       granted_code_id = $2,
		       approved_at = $3,
		       auto_approved = $4
		 WHERE id = $1
		   AND status = 'PENDING'
		RETURNING ` + applicantColumns

	row := q.QueryRow(ctx, sql, applicantID, codeID, approvedAt, autoApproved)
	got, err := scanApplicant(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrStatusCASMismatch
	}
	if err != nil {
		return nil, fmt.Errorf("approving applicant: %w", classifyPgError(err))
	}
	return got, nil
}

// CountAutoApprovedByPlaytest returns the number of applicants for the
// playtest whose auto_approved column is true. Runs inside the
// caller's transaction so the M5.A signup advisory lock serialises
// concurrent cap checks. Backed by applicant_auto_approved_count_idx.
func (s *PgApplicantStore) CountAutoApprovedByPlaytest(ctx context.Context, q Querier, playtestID uuid.UUID) (int, error) {
	const sql = `SELECT COUNT(*) FROM applicant WHERE playtest_id = $1 AND auto_approved = true`
	var n int
	if err := q.QueryRow(ctx, sql, playtestID).Scan(&n); err != nil {
		return 0, fmt.Errorf("counting auto-approved applicants: %w", err)
	}
	return n, nil
}

// RejectCAS is the terminal PENDING → REJECTED transition. Same CAS
// discipline as ApproveCAS — losing the race surfaces as
// ErrStatusCASMismatch.
func (s *PgApplicantStore) RejectCAS(ctx context.Context, q Querier, applicantID uuid.UUID, reason *string) (*Applicant, error) {
	const sql = `
		UPDATE applicant
		   SET status = 'REJECTED',
		       rejection_reason = $2
		 WHERE id = $1
		   AND status = 'PENDING'
		RETURNING ` + applicantColumns

	row := q.QueryRow(ctx, sql, applicantID, stringPtr(reason))
	got, err := scanApplicant(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrStatusCASMismatch
	}
	if err != nil {
		return nil, fmt.Errorf("rejecting applicant: %w", classifyPgError(err))
	}
	return got, nil
}

// UpdateDMStatus is latest-write-wins — by design (PRD §5.4): a manual
// Retry DM that succeeds after a prior failure must overwrite the
// recorded status. errMsg is byte-truncated to 500 bytes preserving
// UTF-8 codepoint boundaries (docs/dm-queue.md).
func (s *PgApplicantStore) UpdateDMStatus(ctx context.Context, applicantID uuid.UUID, status string, attemptAt time.Time, errMsg *string) (*Applicant, error) {
	const sql = `
		UPDATE applicant
		   SET last_dm_status = $2,
		       last_dm_attempt_at = $3,
		       last_dm_error = $4
		 WHERE id = $1
		RETURNING ` + applicantColumns

	var truncated *string
	if errMsg != nil {
		t := truncateUTF8(*errMsg, applicantLastDMErrorMaxBytes)
		truncated = &t
	}
	row := s.pool.QueryRow(ctx, sql, applicantID, status, attemptAt, stringPtr(truncated))
	got, err := scanApplicant(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("updating applicant dm status: %w", classifyPgError(err))
	}
	return got, nil
}

// ListLostDMOnRestart returns every APPROVED applicant in the
// namespace whose last_dm_status is NULL or 'pending'. The JOIN onto
// playtest scopes the sweep to one AGS namespace so a shared DB does
// not bleed sweeps across instances. Soft-deleted playtests are
// excluded — their applicants stay at whatever state they were in,
// since the playtest is no longer admin-visible.
func (s *PgApplicantStore) ListLostDMOnRestart(ctx context.Context, namespace string) ([]*Applicant, error) {
	sql := `
		SELECT ` + applicantColumnsPrefixed("a") + `
		  FROM applicant a
		  JOIN playtest p ON p.id = a.playtest_id
		 WHERE p.namespace = $1
		   AND p.deleted_at IS NULL
		   AND a.status = 'APPROVED'
		   AND (a.last_dm_status IS NULL OR a.last_dm_status = 'pending')`

	rows, err := s.pool.Query(ctx, sql, namespace)
	if err != nil {
		return nil, fmt.Errorf("listing lost-on-restart applicants: %w", err)
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

// ListDMFailedByPlaytest returns every APPROVED applicant whose
// last_dm_status is 'failed' for the playtest. Backs RetryFailedDms
// (PRD §5.5 / dm-queue.md "Bulk retry RPC"). Soft-deleted playtest
// rows have already been gated by the service layer; this query does
// not filter on it.
func (s *PgApplicantStore) ListDMFailedByPlaytest(ctx context.Context, playtestID uuid.UUID) ([]*Applicant, error) {
	sql := `
		SELECT ` + applicantColumns + `
		  FROM applicant
		 WHERE playtest_id = $1
		   AND status = 'APPROVED'
		   AND last_dm_status = 'failed'
		 ORDER BY created_at DESC, id DESC`

	rows, err := s.pool.Query(ctx, sql, playtestID)
	if err != nil {
		return nil, fmt.Errorf("listing dm-failed applicants: %w", err)
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

// applicantColumnsPrefixed renders the applicant column list aliased
// to a table abbreviation so JOIN queries can disambiguate `id` /
// `playtest_id` against the joined table. The constant applicantColumns
// is unprefixed because every other applicant query is single-table.
func applicantColumnsPrefixed(alias string) string {
	cols := []string{
		"id", "playtest_id", "user_id", "discord_handle", "discord_user_id", "platforms",
		"nda_version_hash", "status", "granted_code_id", "approved_at",
		"rejection_reason", "last_dm_status", "last_dm_attempt_at",
		"last_dm_error", "auto_approved", "created_at",
	}
	var b strings.Builder
	for i, c := range cols {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(alias)
		b.WriteByte('.')
		b.WriteString(c)
	}
	return b.String()
}

// SetNDAVersionHash updates only the nda_version_hash column. Returns
// ErrNotFound when the row is gone (caller should resolve that as a
// transient race rather than a user-visible error).
func (s *PgApplicantStore) SetNDAVersionHash(ctx context.Context, applicantID uuid.UUID, hash string) (*Applicant, error) {
	const sql = `
		UPDATE applicant
		   SET nda_version_hash = $2
		 WHERE id = $1
		RETURNING ` + applicantColumns

	row := s.pool.QueryRow(ctx, sql, applicantID, hash)
	got, err := scanApplicant(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("setting applicant nda_version_hash: %w", classifyPgError(err))
	}
	return got, nil
}

// truncateUTF8 returns a string whose byte length is ≤ maxBytes and
// whose final byte still ends on a valid UTF-8 codepoint boundary.
// Used to honour the DB CHECK on applicant.last_dm_error and the
// dm-queue.md 500-byte rule.
func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

func scanApplicant(row pgx.Row) (*Applicant, error) {
	var (
		a             Applicant
		discordUserID pgtype.Text
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
		&discordUserID,
		&a.Platforms,
		&ndaHash,
		&a.Status,
		&grantedCode,
		&approvedAt,
		&rejReason,
		&lastDMStatus,
		&lastDMAttempt,
		&lastDMError,
		&a.AutoApproved,
		&a.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	a.DiscordUserID = stringPtrFromPg(discordUserID)
	a.NDAVersionHash = stringPtrFromPg(ndaHash)
	a.GrantedCodeID = uuidPtrFromPg(grantedCode)
	a.ApprovedAt = timePtrFromPg(approvedAt)
	a.RejectionReason = stringPtrFromPg(rejReason)
	a.LastDMStatus = stringPtrFromPg(lastDMStatus)
	a.LastDMAttemptAt = timePtrFromPg(lastDMAttempt)
	a.LastDMError = stringPtrFromPg(lastDMError)
	return &a, nil
}
