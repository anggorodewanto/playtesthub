package repo

import (
	"context"
	"encoding/json"
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

// AuditLogStore is the append-only audit trail. Rows are never updated
// or deleted after insert.
type AuditLogStore interface {
	Append(ctx context.Context, row *AuditLog) (*AuditLog, error)
	ListByPlaytest(ctx context.Context, playtestID uuid.UUID, limit int) ([]*AuditLog, error)
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
	if playtestID.Valid {
		id := uuid.UUID(playtestID.Bytes)
		a.PlaytestID = &id
	}
	if actorUserID.Valid {
		id := uuid.UUID(actorUserID.Bytes)
		a.ActorUserID = &id
	}
	a.Before = append(json.RawMessage{}, before...)
	a.After = append(json.RawMessage{}, after...)
	return &a, nil
}
