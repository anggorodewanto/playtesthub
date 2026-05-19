package repo_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestMigration0005_PlaytestAutoApproveColumns pins the shape of the
// auto-approve fields added by migration 0005 to playtest:
//   - auto_approve       BOOLEAN NOT NULL DEFAULT FALSE
//   - auto_approve_limit INTEGER NULL
//
// See docs/PRD.md §5.1 / §5.4 "Auto-approve" and docs/STATUS_M5.md A2.
func TestMigration0005_PlaytestAutoApproveColumns(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	const sql = `
		SELECT column_name, data_type, is_nullable, column_default
		  FROM information_schema.columns
		 WHERE table_schema = 'public'
		   AND table_name   = 'playtest'
		   AND column_name IN ('auto_approve', 'auto_approve_limit')
		 ORDER BY column_name`

	rows, err := testPool.Query(ctx, sql)
	if err != nil {
		t.Fatalf("query columns: %v", err)
	}
	defer rows.Close()

	type col struct {
		name, dataType, isNullable string
		defaultExpr                *string
	}
	var got []col
	for rows.Next() {
		var c col
		if scanErr := rows.Scan(&c.name, &c.dataType, &c.isNullable, &c.defaultExpr); scanErr != nil {
			t.Fatalf("scan: %v", scanErr)
		}
		got = append(got, c)
	}
	if rows.Err() != nil {
		t.Fatalf("rows.Err: %v", rows.Err())
	}

	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2 (auto_approve + auto_approve_limit): %+v", len(got), got)
	}

	// auto_approve: BOOLEAN NOT NULL DEFAULT false.
	if got[0].name != "auto_approve" {
		t.Errorf("got[0].name = %q, want auto_approve", got[0].name)
	}
	if got[0].dataType != "boolean" {
		t.Errorf("auto_approve.data_type = %q, want boolean", got[0].dataType)
	}
	if got[0].isNullable != "NO" {
		t.Errorf("auto_approve.is_nullable = %q, want NO", got[0].isNullable)
	}
	if got[0].defaultExpr == nil || *got[0].defaultExpr != "false" {
		t.Errorf("auto_approve.column_default = %v, want \"false\"", got[0].defaultExpr)
	}

	// auto_approve_limit: INTEGER NULL.
	if got[1].name != "auto_approve_limit" {
		t.Errorf("got[1].name = %q, want auto_approve_limit", got[1].name)
	}
	if got[1].dataType != "integer" {
		t.Errorf("auto_approve_limit.data_type = %q, want integer", got[1].dataType)
	}
	if got[1].isNullable != "YES" {
		t.Errorf("auto_approve_limit.is_nullable = %q, want YES", got[1].isNullable)
	}
}

// TestMigration0005_ApplicantAutoApprovedColumn pins applicant.auto_approved:
// BOOLEAN NOT NULL DEFAULT FALSE.
func TestMigration0005_ApplicantAutoApprovedColumn(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	const sql = `
		SELECT data_type, is_nullable, column_default
		  FROM information_schema.columns
		 WHERE table_schema = 'public'
		   AND table_name   = 'applicant'
		   AND column_name  = 'auto_approved'`

	var dataType, isNullable string
	var defaultExpr *string
	if err := testPool.QueryRow(ctx, sql).Scan(&dataType, &isNullable, &defaultExpr); err != nil {
		t.Fatalf("query column: %v", err)
	}
	if dataType != "boolean" {
		t.Errorf("data_type = %q, want boolean", dataType)
	}
	if isNullable != "NO" {
		t.Errorf("is_nullable = %q, want NO", isNullable)
	}
	if defaultExpr == nil || *defaultExpr != "false" {
		t.Errorf("column_default = %v, want \"false\"", defaultExpr)
	}
}

// TestMigration0005_AutoApprovedPartialIndex pins the partial index that
// keeps the cap-count predicate index-only.
func TestMigration0005_AutoApprovedPartialIndex(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	const sql = `
		SELECT indexdef
		  FROM pg_indexes
		 WHERE schemaname = 'public'
		   AND tablename  = 'applicant'
		   AND indexname  = 'applicant_auto_approved_count_idx'`

	var indexDef string
	if err := testPool.QueryRow(ctx, sql).Scan(&indexDef); err != nil {
		t.Fatalf("query index: %v", err)
	}
	// pg formats the predicate canonically; assert both the column scope
	// and the partial predicate are present.
	if !contains(indexDef, "(playtest_id)") {
		t.Errorf("indexdef %q missing (playtest_id)", indexDef)
	}
	if !contains(indexDef, "auto_approved = true") {
		t.Errorf("indexdef %q missing WHERE auto_approved = true", indexDef)
	}
}

// TestMigration0005_AutoApproveCheckRejects pins the
// playtest_auto_approve_limit_bounds CHECK constraint.
func TestMigration0005_AutoApproveCheckRejects(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	// 1. auto_approve=true + auto_approve_limit=NULL → rejected.
	cases := []struct {
		name  string
		sql   string
		args  []any
		label string
	}{
		{
			name: "auto_approve_true_limit_null",
			sql: `INSERT INTO playtest (namespace, slug, title, distribution_model, auto_approve, auto_approve_limit)
			      VALUES ($1, $2, $3, 'STEAM_KEYS', true, NULL)`,
			args:  []any{"ns", "auto-null", "title"},
			label: "auto_approve=true + limit=NULL",
		},
		{
			name: "auto_approve_true_limit_zero",
			sql: `INSERT INTO playtest (namespace, slug, title, distribution_model, auto_approve, auto_approve_limit)
			      VALUES ($1, $2, $3, 'STEAM_KEYS', true, 0)`,
			args:  []any{"ns", "auto-zero", "title"},
			label: "auto_approve=true + limit=0",
		},
		{
			name: "auto_approve_true_limit_over_cap",
			sql: `INSERT INTO playtest (namespace, slug, title, distribution_model, auto_approve, auto_approve_limit)
			      VALUES ($1, $2, $3, 'STEAM_KEYS', true, 100001)`,
			args:  []any{"ns", "auto-over", "title"},
			label: "auto_approve=true + limit=100001",
		},
		{
			name: "auto_approve_false_limit_set",
			sql: `INSERT INTO playtest (namespace, slug, title, distribution_model, auto_approve, auto_approve_limit)
			      VALUES ($1, $2, $3, 'STEAM_KEYS', false, 10)`,
			args:  []any{"ns", "auto-off-set", "title"},
			label: "auto_approve=false + limit=10",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := testPool.Exec(ctx, tc.sql, tc.args...)
			if err == nil {
				t.Fatalf("%s: insert succeeded, want CHECK rejection", tc.label)
			}
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) {
				t.Fatalf("%s: err is %T, want *pgconn.PgError: %v", tc.label, err, err)
			}
			if pgErr.ConstraintName != "playtest_auto_approve_limit_bounds" {
				t.Errorf("%s: constraint = %q, want playtest_auto_approve_limit_bounds", tc.label, pgErr.ConstraintName)
			}
		})
	}
}

// TestMigration0005_AutoApproveCheckAccepts confirms the valid combinations
// the CHECK constraint allows.
func TestMigration0005_AutoApproveCheckAccepts(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	cases := []struct {
		slug   string
		toggle bool
		limit  any // int or nil
	}{
		{"auto-off-null", false, nil},
		{"auto-on-min", true, 1},
		{"auto-on-max", true, 100000},
	}
	for _, tc := range cases {
		_, err := testPool.Exec(ctx,
			`INSERT INTO playtest (namespace, slug, title, distribution_model, auto_approve, auto_approve_limit)
			 VALUES ($1, $2, $3, 'STEAM_KEYS', $4, $5)`,
			"ns", tc.slug, "title", tc.toggle, tc.limit)
		if err != nil {
			t.Errorf("insert slug=%s toggle=%v limit=%v: %v", tc.slug, tc.toggle, tc.limit, err)
		}
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
