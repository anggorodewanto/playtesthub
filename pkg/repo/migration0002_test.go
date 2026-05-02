package repo_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// Migration 0002 ships nda_acceptance and extends the audit-log action
// vocabulary. The action column is free TEXT (no DB CHECK), so the only
// enforcement of the doc-level enum is here: every new action must
// round-trip its declared JSONB payload shape.

func TestMigration0002_NDAAcceptanceColumns(t *testing.T) {
	truncateAll(t)
	ctx := context.Background()

	const sql = `
		SELECT column_name, data_type, is_nullable
		  FROM information_schema.columns
		 WHERE table_schema = 'public'
		   AND table_name = 'nda_acceptance'
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
		{"accepted_at", "timestamp with time zone", "NO"},
		{"nda_version_hash", "text", "NO"},
		{"playtest_id", "uuid", "NO"},
		{"user_id", "uuid", "NO"},
	}
	sort.Slice(got, func(i, j int) bool { return got[i].name < got[j].name })
	if !reflect.DeepEqual(got, want) {
		t.Errorf("nda_acceptance columns mismatch:\n got  %+v\n want %+v", got, want)
	}
}

// PRD §5.3 / schema.md L192: composite PK is the natural key. A second
// accept on the same (user_id, playtest_id, nda_version_hash) must
// surface as a unique-violation so the service layer can resolve to the
// idempotent "return existing row" path.
func TestMigration0002_NDAAcceptanceCompositePK(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "nda-pk")
	ctx := context.Background()

	userID := uuid.New()
	hash := "abc123"

	const insertSQL = `
		INSERT INTO nda_acceptance (user_id, playtest_id, nda_version_hash)
		VALUES ($1, $2, $3)`

	if _, err := testPool.Exec(ctx, insertSQL, userID, pt.ID, hash); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	_, err := testPool.Exec(ctx, insertSQL, userID, pt.ID, hash)
	if err == nil {
		t.Fatalf("duplicate insert succeeded; want unique violation")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("got %v, want unique_violation (23505)", err)
	}

	// Distinct hash on the same (user, playtest) is allowed — re-accept
	// after the playtest's NDA text changes is a new row, not a
	// constraint violation.
	if _, err := testPool.Exec(ctx, insertSQL, userID, pt.ID, "def456"); err != nil {
		t.Fatalf("second hash insert: %v", err)
	}

	// Different user on the same (playtest, hash) is also allowed.
	if _, err := testPool.Exec(ctx, insertSQL, uuid.New(), pt.ID, hash); err != nil {
		t.Fatalf("different user insert: %v", err)
	}
}

// Every M2 audit action must round-trip its declared payload shape
// through JSONB. The action column has no DB enum — these tests are the
// only mechanical guard that schema.md §"AuditLog — `action` enum"
// stays in sync with what the code can actually persist.
func TestMigration0002_AuditActionRoundTrip(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "audit-m2")
	store := repo.NewPgAuditLogStore(testPool)
	ctx := context.Background()

	applicantID := uuid.New().String()
	codeID := uuid.New().String()
	userID := uuid.New().String()
	discordID := "discord:1234567890"
	admin := uuid.New()

	cases := []struct {
		action string
		actor  *uuid.UUID
		before map[string]any
		after  map[string]any
	}{
		{
			action: "nda.accept",
			actor:  &admin,
			after: map[string]any{
				"applicantId":    applicantID,
				"ndaVersionHash": "v1hash",
			},
		},
		{
			action: "applicant.approve",
			actor:  &admin,
			after: map[string]any{
				"applicantId":   applicantID,
				"grantedCodeId": codeID,
			},
		},
		{
			action: "applicant.reject",
			actor:  &admin,
			after: map[string]any{
				"applicantId":     applicantID,
				"rejectionReason": "off-topic submission",
			},
		},
		{
			action: "code.upload",
			actor:  &admin,
			after: map[string]any{
				"count":    float64(42),
				"sha256":   "deadbeef",
				"filename": "keys.csv",
			},
		},
		{
			action: "code.upload_rejected",
			after: map[string]any{
				"filename": "bad.csv",
				"reason":   "charset_violation",
				"rowCount": float64(17),
			},
		},
		{
			action: "code.grant_orphaned",
			after: map[string]any{
				"applicantId":        applicantID,
				"codeId":             codeID,
				"userId":             userID,
				"originalReservedAt": "2026-05-02T10:00:00Z",
			},
		},
		{
			action: "applicant.dm_sent",
			actor:  &admin,
			after: map[string]any{
				"applicantId":   applicantID,
				"discordUserId": discordID,
			},
		},
		{
			action: "applicant.dm_failed",
			after: map[string]any{
				"applicantId": applicantID,
				"error":       "discord 503: service unavailable",
				"attemptAt":   "2026-05-02T10:01:00Z",
			},
		},
		{
			action: "dm.circuit_opened",
			after: map[string]any{
				"trippedAt":          "2026-05-02T10:02:00Z",
				"recentFailureCount": float64(50),
			},
		},
		{
			action: "dm.circuit_closed",
			after: map[string]any{
				"closedAt": "2026-05-02T10:07:00Z",
			},
		},
		{
			action: "campaign.create",
			after: map[string]any{
				"agsItemId":           "item-uuid",
				"agsCampaignId":       "campaign-uuid",
				"itemName":            "playtest-keys",
				"initialCodeQuantity": float64(500),
			},
		},
		{
			action: "campaign.create_failed",
			after: map[string]any{
				"error":            "ags 502 bad gateway",
				"cleanupAttempted": true,
				"cleanupSuccess":   false,
			},
		},
		{
			action: "campaign.generate_codes",
			after: map[string]any{
				"agsCampaignId": "campaign-uuid",
				"quantity":      float64(1000),
				"totalPoolSize": float64(1500),
			},
		},
		{
			action: "campaign.generate_codes_failed",
			after: map[string]any{
				"agsCampaignId":     "campaign-uuid",
				"requestedQuantity": float64(1000),
				"error":             "ags timeout after 30s",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			afterBytes, err := json.Marshal(tc.after)
			if err != nil {
				t.Fatalf("marshal after: %v", err)
			}
			row := &repo.AuditLog{
				Namespace:   testNamespace,
				PlaytestID:  &pt.ID,
				ActorUserID: tc.actor,
				Action:      tc.action,
				After:       afterBytes,
			}
			if tc.before != nil {
				b, mErr := json.Marshal(tc.before)
				if mErr != nil {
					t.Fatalf("marshal before: %v", mErr)
				}
				row.Before = b
			}
			appended, err := store.Append(ctx, row)
			if err != nil {
				t.Fatalf("Append %s: %v", tc.action, err)
			}
			if appended.Action != tc.action {
				t.Errorf("action = %q, want %q", appended.Action, tc.action)
			}

			var gotAfter map[string]any
			if err := json.Unmarshal(appended.After, &gotAfter); err != nil {
				t.Fatalf("unmarshal after: %v", err)
			}
			if !reflect.DeepEqual(gotAfter, tc.after) {
				t.Errorf("after round-trip mismatch:\n got  %#v\n want %#v", gotAfter, tc.after)
			}

			// System-emitted rows must persist NULL actor (schema.md
			// L24 + per-action `**System-emitted**` annotations).
			wantNilActor := tc.actor == nil
			gotNilActor := appended.ActorUserID == nil
			if wantNilActor != gotNilActor {
				t.Errorf("actor nullability: got nil=%v, want nil=%v",
					gotNilActor, wantNilActor)
			}
		})
	}
}
