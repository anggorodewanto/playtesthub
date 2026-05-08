package repo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditLog mirrors the audit_log table in migration 0001. The `Before`
// and `After` payloads are opaque to the repo — per-action shapes live
// in docs/schema.md. Redaction rules (no Code.value, no NDA text in
// non-nda.edit rows, etc.) are the service layer's responsibility;
// this struct carries whatever JSONB the caller hands it.
type AuditLog struct {
	ID          uuid.UUID
	Namespace   string
	PlaytestID  *uuid.UUID
	ActorUserID *uuid.UUID
	Action      string
	Before      json.RawMessage
	After       json.RawMessage
	CreatedAt   time.Time
}

// AuditLogPage carries one page of audit_log rows + the opaque cursor
// to the next page. NextPageToken is empty on the final page.
type AuditLogPage struct {
	Rows          []*AuditLog
	NextPageToken string
}

// AuditLogPageQuery is the filter + pagination input for List. Limit
// ≤ 0 picks the store-side default (50, matching PRD §6 Pagination).
//
// ActorFilter accepts three shapes:
//
//   - "" → no actor filter
//   - "system" → actor_user_id IS NULL (PRD §4.7 / §5.7 system rows)
//   - "<uuid>" → actor_user_id = <uuid>
//
// SystemActor is used by the in-process service layer when it wants to
// filter on a parsed UUID directly without round-tripping through the
// string form. When non-nil it overrides ActorFilter.
type AuditLogPageQuery struct {
	PlaytestID   uuid.UUID
	ActorFilter  string     // "" / "system" / "<uuid>"
	ActorUserID  *uuid.UUID // overrides ActorFilter when non-nil
	ActionFilter string     // "" → no action filter (exact match otherwise)
	PageToken    string     // "" → start of stream
	Limit        int        // ≤0 → 50
}

// AuditLogStore is the append-only audit trail. Rows are never updated
// or deleted after insert.
type AuditLogStore interface {
	Append(ctx context.Context, row *AuditLog) (*AuditLog, error)
	ListByPlaytest(ctx context.Context, playtestID uuid.UUID, limit int) ([]*AuditLog, error)
	// List returns one page of audit rows ordered by (created_at,
	// id) DESC, DESC. Powers the M3 ListAuditLog RPC and the admin
	// audit viewer (PRD §5.7 page 5). Returns
	// ErrInvalidAuditLogToken on a malformed token.
	List(ctx context.Context, q AuditLogPageQuery) (*AuditLogPage, error)
}

type PgAuditLogStore struct {
	pool *pgxpool.Pool
}

func NewPgAuditLogStore(pool *pgxpool.Pool) *PgAuditLogStore {
	return &PgAuditLogStore{pool: pool}
}

const auditLogColumns = `
	id, namespace, playtest_id, actor_user_id, action, before, after, created_at`

// Append writes a single audit row and returns the DB-assigned id +
// timestamp. Empty JSONB is stored as `{}` — the migration-level
// default. Both Before and After are optional; callers may pass nil
// for system-emitted events that only need metadata on one side.
func (s *PgAuditLogStore) Append(ctx context.Context, row *AuditLog) (*AuditLog, error) {
	const sql = `
		INSERT INTO audit_log (
			namespace, playtest_id, actor_user_id, action, before, after
		)
		VALUES ($1, $2, $3, $4, COALESCE($5, '{}'::jsonb), COALESCE($6, '{}'::jsonb))
		RETURNING ` + auditLogColumns

	var (
		before any
		after  any
	)
	if len(row.Before) > 0 {
		before = []byte(row.Before)
	}
	if len(row.After) > 0 {
		after = []byte(row.After)
	}

	pgRow := s.pool.QueryRow(ctx, sql,
		row.Namespace,
		uuidPtr(row.PlaytestID),
		uuidPtr(row.ActorUserID),
		row.Action,
		before,
		after,
	)
	got, err := scanAuditLog(pgRow)
	if err != nil {
		return nil, fmt.Errorf("appending audit log: %w", classifyPgError(err))
	}
	return got, nil
}

// ListByPlaytest returns the most recent `limit` rows for the given
// playtest, newest first. Uses the playtestId + createdAt DESC index
// (migration 0001).
func (s *PgAuditLogStore) ListByPlaytest(ctx context.Context, playtestID uuid.UUID, limit int) ([]*AuditLog, error) {
	if limit <= 0 {
		limit = 50
	}
	const sql = `
		SELECT ` + auditLogColumns + `
		  FROM audit_log
		 WHERE playtest_id = $1
		 ORDER BY created_at DESC, id ASC
		 LIMIT $2`

	rows, err := s.pool.Query(ctx, sql, playtestID, limit)
	if err != nil {
		return nil, fmt.Errorf("listing audit log: %w", err)
	}
	defer rows.Close()

	out := make([]*AuditLog, 0)
	for rows.Next() {
		a, scanErr := scanAuditLog(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scanning audit_log row: %w", scanErr)
		}
		out = append(out, a)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterating audit_log rows: %w", rowsErr)
	}
	return out, nil
}

// ErrInvalidAuditLogToken is surfaced by List when the opaque
// page_token does not decode into a (createdAt, id) pair.
var ErrInvalidAuditLogToken = errors.New("repo: invalid audit log page token")

// List walks audit_log under (created_at, id) DESC, DESC pagination.
// Filters compose: actor + action + cursor narrow the stream;
// ordering is constant. Cursor comparison is a tuple `<` so Postgres
// can satisfy the WHERE clause from the playtest+created_at index
// without a sort.
func (s *PgAuditLogStore) List(ctx context.Context, q AuditLogPageQuery) (*AuditLogPage, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = ListPagedDefaultLimit
	}
	if limit > ListPagedMaxLimit {
		limit = ListPagedMaxLimit
	}

	cursor, err := decodeAuditLogToken(q.PageToken)
	if err != nil {
		return nil, err
	}

	sql := `SELECT ` + auditLogColumns + ` FROM audit_log WHERE playtest_id = $1`
	args := []any{q.PlaytestID}
	idx := 2

	switch {
	case q.ActorUserID != nil:
		sql += fmt.Sprintf(` AND actor_user_id = $%d`, idx)
		args = append(args, *q.ActorUserID)
		idx++
	case q.ActorFilter == "system":
		sql += ` AND actor_user_id IS NULL`
	case q.ActorFilter != "":
		parsed, parseErr := uuid.Parse(q.ActorFilter)
		if parseErr != nil {
			return nil, fmt.Errorf("audit log actor filter %q: %w", q.ActorFilter, parseErr)
		}
		sql += fmt.Sprintf(` AND actor_user_id = $%d`, idx)
		args = append(args, parsed)
		idx++
	}

	if q.ActionFilter != "" {
		sql += fmt.Sprintf(` AND action = $%d`, idx)
		args = append(args, q.ActionFilter)
		idx++
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
		return nil, fmt.Errorf("listing audit log paged: %w", err)
	}
	defer rows.Close()

	out := make([]*AuditLog, 0, limit)
	for rows.Next() {
		a, scanErr := scanAuditLog(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scanning audit_log row: %w", scanErr)
		}
		out = append(out, a)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterating audit_log rows: %w", rowsErr)
	}

	page := &AuditLogPage{}
	if len(out) > limit {
		last := out[limit-1]
		page.Rows = out[:limit]
		page.NextPageToken = encodeAuditLogToken(auditLogCursor{CreatedAt: last.CreatedAt, ID: last.ID})
		return page, nil
	}
	page.Rows = out
	return page, nil
}

type auditLogCursor struct {
	CreatedAt time.Time `json:"c"`
	ID        uuid.UUID `json:"i"`
}

func encodeAuditLogToken(c auditLogCursor) string {
	return encodePageCursor(c)
}

func decodeAuditLogToken(token string) (*auditLogCursor, error) {
	return decodePageCursor(token, func(c *auditLogCursor) uuid.UUID { return c.ID }, ErrInvalidAuditLogToken)
}

func scanAuditLog(row pgx.Row) (*AuditLog, error) {
	var (
		a           AuditLog
		playtestID  pgtype.UUID
		actorUserID pgtype.UUID
		before      []byte
		after       []byte
	)
	err := row.Scan(
		&a.ID,
		&a.Namespace,
		&playtestID,
		&actorUserID,
		&a.Action,
		&before,
		&after,
		&a.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	a.PlaytestID = uuidPtrFromPg(playtestID)
	a.ActorUserID = uuidPtrFromPg(actorUserID)
	a.Before = append(json.RawMessage{}, before...)
	a.After = append(json.RawMessage{}, after...)
	return &a, nil
}
