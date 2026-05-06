package repo_test

import (
	"context"
	"reflect"
	"sort"
	"testing"
)

// Migration 0003 ships survey + survey_response. The action column on
// audit_log gains M3 entries (`survey.create`, `survey.edit`,
// `applicant.dm_failed_bulk`) — those round-trip through the same
// JSONB Append path covered in migration0002_test.go and survey_test.go.

func TestMigration0003_SurveyColumns(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	const sql = `
		SELECT column_name, data_type, is_nullable
		  FROM information_schema.columns
		 WHERE table_schema = 'public'
		   AND table_name = 'survey'
		 ORDER BY column_name`

	rows, err := testPool.Query(ctx, sql)
	if err != nil {
		t.Fatalf("query columns: %v", err)
	}
	defer rows.Close()

	type col struct{ name, typ, nullable string }
	var got []col
	for rows.Next() {
		var c col
		if err := rows.Scan(&c.name, &c.typ, &c.nullable); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}

	want := []col{
		{"created_at", "timestamp with time zone", "NO"},
		{"id", "uuid", "NO"},
		{"playtest_id", "uuid", "NO"},
		{"questions", "jsonb", "NO"},
		{"version", "integer", "NO"},
	}
	sort.Slice(got, func(i, j int) bool { return got[i].name < got[j].name })
	if !reflect.DeepEqual(got, want) {
		t.Errorf("survey columns mismatch:\n got  %+v\n want %+v", got, want)
	}
}

func TestMigration0003_SurveyResponseColumns(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	const sql = `
		SELECT column_name, data_type, is_nullable
		  FROM information_schema.columns
		 WHERE table_schema = 'public'
		   AND table_name = 'survey_response'
		 ORDER BY column_name`

	rows, err := testPool.Query(ctx, sql)
	if err != nil {
		t.Fatalf("query columns: %v", err)
	}
	defer rows.Close()

	type col struct{ name, typ, nullable string }
	var got []col
	for rows.Next() {
		var c col
		if err := rows.Scan(&c.name, &c.typ, &c.nullable); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}

	want := []col{
		{"answers", "jsonb", "NO"},
		{"id", "uuid", "NO"},
		{"playtest_id", "uuid", "NO"},
		{"submitted_at", "timestamp with time zone", "NO"},
		{"survey_id", "uuid", "NO"},
		{"user_id", "uuid", "NO"},
	}
	sort.Slice(got, func(i, j int) bool { return got[i].name < got[j].name })
	if !reflect.DeepEqual(got, want) {
		t.Errorf("survey_response columns mismatch:\n got  %+v\n want %+v", got, want)
	}
}
