package repo_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestMigration0007_ApplicantADTTelemetryColumns pins the four nullable
// ADT telemetry cache columns added by migration 0007 to applicant. They
// ship dormant in M5.C; M6 lights them up. See docs/schema.md §Applicant
// entity and docs/STATUS_M5.md C2.
func TestMigration0007_ApplicantADTTelemetryColumns(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	const sql = `
		SELECT column_name, data_type, is_nullable, column_default
		  FROM information_schema.columns
		 WHERE table_schema = 'public'
		   AND table_name   = 'applicant'
		   AND column_name IN ('adt_download_at', 'adt_total_playtime_seconds', 'adt_hardware_specs', 'adt_crash_count')
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
	if len(got) != 4 {
		t.Fatalf("got %d rows, want 4: %+v", len(got), got)
	}

	wantNullable := map[string]string{
		"adt_crash_count":            "NO",
		"adt_download_at":            "YES",
		"adt_hardware_specs":         "YES",
		"adt_total_playtime_seconds": "YES",
	}
	wantType := map[string]string{
		"adt_crash_count":            "integer",
		"adt_download_at":            "timestamp with time zone",
		"adt_hardware_specs":         "jsonb",
		"adt_total_playtime_seconds": "integer",
	}
	for _, c := range got {
		if wantNullable[c.name] != c.isNullable {
			t.Errorf("%s.is_nullable = %q, want %q", c.name, c.isNullable, wantNullable[c.name])
		}
		if wantType[c.name] != c.dataType {
			t.Errorf("%s.data_type = %q, want %q", c.name, c.dataType, wantType[c.name])
		}
	}

	// crash_count must default 0 so the SQL aggregate logic that surfaces
	// "Crash Reports (N)" in M6's modal sees a non-NULL count even before
	// the telemetry worker writes anything.
	for _, c := range got {
		if c.name == "adt_crash_count" {
			if c.defaultExpr == nil || *c.defaultExpr != "0" {
				t.Errorf("adt_crash_count.column_default = %v, want \"0\"", c.defaultExpr)
			}
		}
	}
}

// TestMigration0007_AnnouncementTableShape pins the announcement table
// shape per docs/schema.md §"announcement + announcement_recipient tables".
func TestMigration0007_AnnouncementTableShape(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	const sql = `
		SELECT column_name, data_type, is_nullable
		  FROM information_schema.columns
		 WHERE table_schema = 'public'
		   AND table_name   = 'announcement'
		 ORDER BY column_name`

	rows, err := testPool.Query(ctx, sql)
	if err != nil {
		t.Fatalf("query columns: %v", err)
	}
	defer rows.Close()

	type col struct{ name, dataType, isNullable string }
	var got []col
	for rows.Next() {
		var c col
		if scanErr := rows.Scan(&c.name, &c.dataType, &c.isNullable); scanErr != nil {
			t.Fatalf("scan: %v", scanErr)
		}
		got = append(got, c)
	}
	want := []col{
		{"created_at", "timestamp with time zone", "NO"},
		{"created_by_user_id", "uuid", "NO"},
		{"id", "uuid", "NO"},
		{"message", "text", "NO"},
		{"playtest_id", "uuid", "NO"},
		{"recipients_sent", "integer", "NO"},
		{"recipients_total", "integer", "NO"},
		{"send_to_filter", "text", "NO"},
		{"status", "text", "NO"},
		{"subject", "text", "NO"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d columns, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("col[%d] = %+v, want %+v", i, got[i], w)
		}
	}
}

// TestMigration0007_AnnouncementCheckRejects pins the four CHECK
// constraints on announcement: send_to_filter enum, status enum, subject
// length, message length.
func TestMigration0007_AnnouncementCheckRejects(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	// Seed one playtest so FK is satisfied.
	var playtestID string
	err := testPool.QueryRow(ctx,
		`INSERT INTO playtest (namespace, slug, title, distribution_model)
		 VALUES ('ns', 'pt-c2', 'title', 'STEAM_KEYS') RETURNING id`).Scan(&playtestID)
	if err != nil {
		t.Fatalf("seed playtest: %v", err)
	}

	cases := []struct {
		name       string
		subject    string
		message    string
		filter     string
		status     string
		wantConstr string
	}{
		{"empty_subject", "", "msg", "ALL", "SENDING", "announcement_subject_len"},
		{"empty_message", "subj", "", "ALL", "SENDING", "announcement_message_len"},
		{"subject_over_200", longString(201), "msg", "ALL", "SENDING", "announcement_subject_len"},
		{"message_over_4000", "subj", longString(4001), "ALL", "SENDING", "announcement_message_len"},
		{"bad_filter", "subj", "msg", "EVERYBODY", "SENDING", "announcement_send_to_filter_enum"},
		{"bad_status", "subj", "msg", "ALL", "BROADCASTING", "announcement_status_enum"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := testPool.Exec(ctx,
				`INSERT INTO announcement (playtest_id, send_to_filter, subject, message, status, recipients_total, created_by_user_id)
				 VALUES ($1, $2, $3, $4, $5, 0, gen_random_uuid())`,
				playtestID, tc.filter, tc.subject, tc.message, tc.status)
			if err == nil {
				t.Fatalf("insert succeeded, want CHECK rejection on %s", tc.wantConstr)
			}
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) {
				t.Fatalf("err is %T, want *pgconn.PgError: %v", err, err)
			}
			if pgErr.ConstraintName != tc.wantConstr {
				t.Errorf("constraint = %q, want %q", pgErr.ConstraintName, tc.wantConstr)
			}
		})
	}
}

// TestMigration0007_AnnouncementRecipientShape pins the announcement_recipient
// join table shape including the dm_status enum CHECK.
func TestMigration0007_AnnouncementRecipientShape(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	// Pin PRIMARY KEY (announcement_id, applicant_id).
	const pkSQL = `
		SELECT array_agg(a.attname ORDER BY array_position(c.conkey, a.attnum))
		  FROM pg_constraint c
		  JOIN pg_class t ON t.oid = c.conrelid
		  JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = ANY (c.conkey)
		 WHERE t.relname = 'announcement_recipient' AND c.contype = 'p'`
	var pkCols []string
	if err := testPool.QueryRow(ctx, pkSQL).Scan(&pkCols); err != nil {
		t.Fatalf("query pk: %v", err)
	}
	if len(pkCols) != 2 || pkCols[0] != "announcement_id" || pkCols[1] != "applicant_id" {
		t.Errorf("pk = %v, want [announcement_id applicant_id]", pkCols)
	}

	// Reject bogus dm_status to pin the enum CHECK.
	// First seed a playtest, applicant, and announcement so the FKs satisfy.
	var playtestID, applicantID, announcementID string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO playtest (namespace, slug, title, distribution_model)
		 VALUES ('ns', 'pt-c2b', 'title', 'STEAM_KEYS') RETURNING id`).Scan(&playtestID); err != nil {
		t.Fatalf("seed playtest: %v", err)
	}
	if err := testPool.QueryRow(ctx,
		`INSERT INTO applicant (playtest_id, user_id, discord_handle, platforms)
		 VALUES ($1, gen_random_uuid(), 'h', '{}') RETURNING id`, playtestID).Scan(&applicantID); err != nil {
		t.Fatalf("seed applicant: %v", err)
	}
	if err := testPool.QueryRow(ctx,
		`INSERT INTO announcement (playtest_id, send_to_filter, subject, message, status, recipients_total, created_by_user_id)
		 VALUES ($1, 'ALL', 'subj', 'msg', 'SENDING', 1, gen_random_uuid()) RETURNING id`,
		playtestID).Scan(&announcementID); err != nil {
		t.Fatalf("seed announcement: %v", err)
	}

	_, err := testPool.Exec(ctx,
		`INSERT INTO announcement_recipient (announcement_id, applicant_id, dm_status)
		 VALUES ($1, $2, 'STREAMING')`, announcementID, applicantID)
	if err == nil {
		t.Fatalf("insert succeeded, want CHECK rejection on dm_status enum")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("err is %T, want *pgconn.PgError: %v", err, err)
	}
	if pgErr.ConstraintName != "announcement_recipient_dm_status_enum" {
		t.Errorf("constraint = %q, want announcement_recipient_dm_status_enum", pgErr.ConstraintName)
	}
}

// TestMigration0007_StaleTelemetryIndex pins the dormant
// applicant_adt_telemetry_stale_idx predicate.
func TestMigration0007_StaleTelemetryIndex(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	const sql = `
		SELECT indexdef
		  FROM pg_indexes
		 WHERE schemaname = 'public'
		   AND tablename  = 'applicant'
		   AND indexname  = 'applicant_adt_telemetry_stale_idx'`

	var indexDef string
	if err := testPool.QueryRow(ctx, sql).Scan(&indexDef); err != nil {
		t.Fatalf("query index: %v", err)
	}
	if !contains(indexDef, "(playtest_id, status)") {
		t.Errorf("indexdef %q missing (playtest_id, status)", indexDef)
	}
	if !contains(indexDef, "adt_download_at IS NULL") {
		t.Errorf("indexdef %q missing adt_download_at IS NULL", indexDef)
	}
	if !contains(indexDef, "(status = 'APPROVED'") && !contains(indexDef, "status = 'APPROVED'") {
		t.Errorf("indexdef %q missing status = 'APPROVED'", indexDef)
	}
}

func longString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a'
	}
	return string(b)
}
