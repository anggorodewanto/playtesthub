package repo_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// Each typed writer marshals the schema.md payload and appends. These
// tests assert the action constant + the JSONB shape match the
// authoritative spec; the migration0002 test covers DB-level round-trip
// of arbitrary payloads, this file covers the Go-side helpers.

func loadOnly(t *testing.T, ctx context.Context, store repo.AuditLogStore, playtestID uuid.UUID) *repo.AuditLog {
	t.Helper()
	rows, err := store.ListByPlaytest(ctx, playtestID, 10)
	if err != nil {
		t.Fatalf("ListByPlaytest: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("row count = %d, want 1", len(rows))
	}
	return rows[0]
}

func decodeAfter(t *testing.T, row *repo.AuditLog) map[string]any {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(row.After, &got); err != nil {
		t.Fatalf("unmarshal after: %v", err)
	}
	return got
}

func TestAppendNDAAccept_PayloadAndActor(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "audit-nda-accept")
	store := repo.NewPgAuditLogStore(testPool)
	ctx := context.Background()

	actor := uuid.New()
	applicant := uuid.New()
	if err := repo.AppendNDAAccept(ctx, store, testNamespace, pt.ID, &actor, applicant, "v1hash"); err != nil {
		t.Fatalf("AppendNDAAccept: %v", err)
	}
	row := loadOnly(t, ctx, store, pt.ID)
	if row.Action != repo.ActionNDAAccept {
		t.Errorf("action = %q, want %q", row.Action, repo.ActionNDAAccept)
	}
	if row.ActorUserID == nil || *row.ActorUserID != actor {
		t.Errorf("actor = %v, want %v", row.ActorUserID, actor)
	}
	got := decodeAfter(t, row)
	if got["applicantId"] != applicant.String() || got["ndaVersionHash"] != "v1hash" {
		t.Errorf("payload = %+v", got)
	}
}

func TestAppendApplicantApprove_OmitsCodeValue(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "audit-approve")
	store := repo.NewPgAuditLogStore(testPool)
	ctx := context.Background()

	actor, applicant, code := uuid.New(), uuid.New(), uuid.New()
	if err := repo.AppendApplicantApprove(ctx, store, testNamespace, pt.ID, actor, applicant, code); err != nil {
		t.Fatalf("AppendApplicantApprove: %v", err)
	}
	row := loadOnly(t, ctx, store, pt.ID)
	if row.Action != repo.ActionApplicantApprove {
		t.Errorf("action = %q", row.Action)
	}
	got := decodeAfter(t, row)
	if got["grantedCodeId"] != code.String() {
		t.Errorf("grantedCodeId = %v, want %v", got["grantedCodeId"], code)
	}
	// schema.md L45: raw code value is forbidden.
	if _, hasValue := got["value"]; hasValue {
		t.Error("after payload leaked a raw code value")
	}
}

func TestAppendApplicantReject_PreservesReason(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "audit-reject")
	store := repo.NewPgAuditLogStore(testPool)
	ctx := context.Background()

	actor, applicant := uuid.New(), uuid.New()
	if err := repo.AppendApplicantReject(ctx, store, testNamespace, pt.ID, actor, applicant, "off-topic"); err != nil {
		t.Fatalf("AppendApplicantReject: %v", err)
	}
	got := decodeAfter(t, loadOnly(t, ctx, store, pt.ID))
	if got["rejectionReason"] != "off-topic" {
		t.Errorf("rejectionReason = %v", got["rejectionReason"])
	}
}

func TestAppendCodeUpload_RawValuesNeverPersisted(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "audit-code-upload")
	store := repo.NewPgAuditLogStore(testPool)
	ctx := context.Background()

	actor := uuid.New()
	if err := repo.AppendCodeUpload(ctx, store, testNamespace, pt.ID, actor, 42, "deadbeef", "keys.csv"); err != nil {
		t.Fatalf("AppendCodeUpload: %v", err)
	}
	got := decodeAfter(t, loadOnly(t, ctx, store, pt.ID))
	if got["count"].(float64) != 42 {
		t.Errorf("count = %v", got["count"])
	}
	if got["sha256"] != "deadbeef" || got["filename"] != "keys.csv" {
		t.Errorf("payload = %+v", got)
	}
	// Catch a future regression that adds a "values" array or similar.
	for k := range got {
		if k != "count" && k != "sha256" && k != "filename" {
			t.Errorf("unexpected payload field %q", k)
		}
	}
}

func TestAppendCodeUploadRejected_SystemEmitted(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "audit-upload-rej")
	store := repo.NewPgAuditLogStore(testPool)
	ctx := context.Background()

	if err := repo.AppendCodeUploadRejected(ctx, store, testNamespace, pt.ID, "bad.csv", "charset_violation", 17); err != nil {
		t.Fatalf("AppendCodeUploadRejected: %v", err)
	}
	row := loadOnly(t, ctx, store, pt.ID)
	if row.ActorUserID != nil {
		t.Errorf("actor = %v, want nil (system-emitted)", row.ActorUserID)
	}
	got := decodeAfter(t, row)
	if got["filename"] != "bad.csv" || got["reason"] != "charset_violation" || got["rowCount"].(float64) != 17 {
		t.Errorf("payload = %+v", got)
	}
}

func TestAppendCodeGrantOrphaned_SystemEmittedNoActor(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "audit-grant-orph")
	store := repo.NewPgAuditLogStore(testPool)
	ctx := context.Background()

	applicant, code, user := uuid.New(), uuid.New(), uuid.New()
	now := time.Now().UTC().Truncate(time.Second)
	if err := repo.AppendCodeGrantOrphaned(ctx, store, testNamespace, pt.ID, applicant, code, user, now); err != nil {
		t.Fatalf("AppendCodeGrantOrphaned: %v", err)
	}
	row := loadOnly(t, ctx, store, pt.ID)
	if row.ActorUserID != nil {
		t.Errorf("actor = %v, want nil", row.ActorUserID)
	}
	got := decodeAfter(t, row)
	if got["applicantId"] != applicant.String() || got["codeId"] != code.String() || got["userId"] != user.String() {
		t.Errorf("payload = %+v", got)
	}
	// Timestamp round-trips as RFC3339Nano.
	parsed, err := time.Parse(time.RFC3339Nano, got["originalReservedAt"].(string))
	if err != nil {
		t.Fatalf("parse originalReservedAt: %v", err)
	}
	if !parsed.Equal(now) {
		t.Errorf("originalReservedAt = %v, want %v", parsed, now)
	}
}

func TestAppendApplicantDMSent_AdminAttributed(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "audit-dm-sent")
	store := repo.NewPgAuditLogStore(testPool)
	ctx := context.Background()

	admin, applicant := uuid.New(), uuid.New()
	if err := repo.AppendApplicantDMSent(ctx, store, testNamespace, pt.ID, admin, applicant, "discord:42"); err != nil {
		t.Fatalf("AppendApplicantDMSent: %v", err)
	}
	row := loadOnly(t, ctx, store, pt.ID)
	if row.ActorUserID == nil || *row.ActorUserID != admin {
		t.Errorf("actor = %v, want %v", row.ActorUserID, admin)
	}
}

// schema.md L48 / dm-queue.md: the persisted `error` field is byte-
// truncated to 500 bytes preserving valid UTF-8 codepoints.
func TestAppendApplicantDMFailed_ErrorTruncatedAt500Bytes(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "audit-dm-failed-trunc")
	store := repo.NewPgAuditLogStore(testPool)
	ctx := context.Background()

	long := strings.Repeat("é", 251) // 502 bytes; cut at 500 lands mid-codepoint.
	if err := repo.AppendApplicantDMFailed(ctx, store, testNamespace, pt.ID, uuid.New(), long, time.Now()); err != nil {
		t.Fatalf("AppendApplicantDMFailed: %v", err)
	}
	got := decodeAfter(t, loadOnly(t, ctx, store, pt.ID))
	persisted := got["error"].(string)
	if len(persisted) > 500 {
		t.Errorf("len(error) = %d, want ≤500", len(persisted))
	}
	if !strings.HasSuffix(persisted, "é") {
		t.Errorf("trailing char not 'é': %q", persisted[len(persisted)-2:])
	}
}

func TestAppendDMCircuit_NamespaceScopedNoPlaytest(t *testing.T) {
	truncateAll(t)
	store := repo.NewPgAuditLogStore(testPool)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	if err := repo.AppendDMCircuitOpened(ctx, store, testNamespace, now, 50); err != nil {
		t.Fatalf("AppendDMCircuitOpened: %v", err)
	}
	if err := repo.AppendDMCircuitClosed(ctx, store, testNamespace, now.Add(5*time.Minute)); err != nil {
		t.Fatalf("AppendDMCircuitClosed: %v", err)
	}
	// Both rows have nil playtest_id (namespace-scoped per schema.md).
	rows, err := testPool.Query(ctx,
		`SELECT action, playtest_id, actor_user_id FROM audit_log WHERE namespace=$1 ORDER BY created_at`, testNamespace)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var action string
		var playtestID, actor *uuid.UUID
		if err := rows.Scan(&action, &playtestID, &actor); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if playtestID != nil {
			t.Errorf("%s: playtest_id = %v, want nil", action, playtestID)
		}
		if actor != nil {
			t.Errorf("%s: actor = %v, want nil (system-emitted)", action, actor)
		}
		count++
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestAppendCampaignActions_SystemEmitted(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "audit-campaign")
	store := repo.NewPgAuditLogStore(testPool)
	ctx := context.Background()

	if err := repo.AppendCampaignCreate(ctx, store, testNamespace, pt.ID, "item-1", "camp-1", "playtest-keys", 500); err != nil {
		t.Fatalf("AppendCampaignCreate: %v", err)
	}
	if err := repo.AppendCampaignCreateFailed(ctx, store, testNamespace, pt.ID, "ags 502", true, false); err != nil {
		t.Fatalf("AppendCampaignCreateFailed: %v", err)
	}
	if err := repo.AppendCampaignGenerateCodes(ctx, store, testNamespace, pt.ID, "camp-1", 1000, 1500); err != nil {
		t.Fatalf("AppendCampaignGenerateCodes: %v", err)
	}
	if err := repo.AppendCampaignGenerateCodesFailed(ctx, store, testNamespace, pt.ID, "camp-1", 1000, "ags timeout"); err != nil {
		t.Fatalf("AppendCampaignGenerateCodesFailed: %v", err)
	}

	rows, err := store.ListByPlaytest(ctx, pt.ID, 10)
	if err != nil {
		t.Fatalf("ListByPlaytest: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("count = %d, want 4", len(rows))
	}
	for _, row := range rows {
		if row.ActorUserID != nil {
			t.Errorf("%s: actor = %v, want nil (system-emitted)", row.Action, row.ActorUserID)
		}
	}
}
