package repo_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// Key M1-phase-4 invariant (STATUS.md): audit-log JSONB round-trip.
// A non-trivial payload (nested object, array, numbers, strings, nulls,
// unicode) must survive store → fetch unchanged when decoded.
func TestAuditLogAppend_JSONBRoundTrip(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "audit-roundtrip")
	store := repo.NewPgAuditLogStore(testPool)

	beforePayload := map[string]any{
		"title":        "old title",
		"platforms":    []any{"STEAM", "EPIC"},
		"ndaRequired":  false,
		"surveyId":     nil,
		"nested":       map[string]any{"count": float64(3), "label": "hëllo"},
		"emptyStrings": []any{"", " "},
	}
	afterPayload := map[string]any{
		"title":       "new title",
		"platforms":   []any{"STEAM"},
		"ndaRequired": true,
		"surveyId":    "9ab70d98-0000-4000-8000-000000000001",
		"nested":      map[string]any{"count": float64(4), "label": "hëllo"},
	}
	beforeBytes, err := json.Marshal(beforePayload)
	if err != nil {
		t.Fatalf("marshal before: %v", err)
	}
	afterBytes, err := json.Marshal(afterPayload)
	if err != nil {
		t.Fatalf("marshal after: %v", err)
	}

	actor := uuid.New()
	appended, err := store.Append(context.Background(), &repo.AuditLog{
		Namespace:   testNamespace,
		PlaytestID:  &pt.ID,
		ActorUserID: &actor,
		Action:      "playtest.edit",
		Before:      beforeBytes,
		After:       afterBytes,
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if appended.ID == uuid.Nil || appended.CreatedAt.IsZero() {
		t.Error("Append did not populate id/created_at")
	}

	rows, err := store.ListByPlaytest(context.Background(), pt.ID, 10)
	if err != nil {
		t.Fatalf("ListByPlaytest: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("row count = %d, want 1", len(rows))
	}
	got := rows[0]

	var gotBefore, gotAfter map[string]any
	if err := json.Unmarshal(got.Before, &gotBefore); err != nil {
		t.Fatalf("unmarshal before: %v", err)
	}
	if err := json.Unmarshal(got.After, &gotAfter); err != nil {
		t.Fatalf("unmarshal after: %v", err)
	}
	if !reflect.DeepEqual(gotBefore, beforePayload) {
		t.Errorf("before round-trip mismatch:\n got  %#v\n want %#v", gotBefore, beforePayload)
	}
	if !reflect.DeepEqual(gotAfter, afterPayload) {
		t.Errorf("after round-trip mismatch:\n got  %#v\n want %#v", gotAfter, afterPayload)
	}
	if got.Action != "playtest.edit" {
		t.Errorf("action = %q, want playtest.edit", got.Action)
	}
	if got.ActorUserID == nil || *got.ActorUserID != actor {
		t.Errorf("actor_user_id round-trip broke: got %v, want %v", got.ActorUserID, actor)
	}
}

// Empty Before/After must store as `{}` (the migration default), not
// as Postgres NULL.
func TestAuditLogAppend_EmptyPayloadDefaults(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "audit-empty")
	store := repo.NewPgAuditLogStore(testPool)

	got, err := store.Append(context.Background(), &repo.AuditLog{
		Namespace:  testNamespace,
		PlaytestID: &pt.ID,
		Action:     "playtest.status_transition",
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if string(got.Before) != "{}" {
		t.Errorf("empty Before stored as %q, want {}", got.Before)
	}
	if string(got.After) != "{}" {
		t.Errorf("empty After stored as %q, want {}", got.After)
	}
}

// System-emitted rows carry nil ActorUserID (e.g. dm.circuit_opened).
func TestAuditLogAppend_NullActor(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "audit-sysactor")
	store := repo.NewPgAuditLogStore(testPool)

	got, err := store.Append(context.Background(), &repo.AuditLog{
		Namespace:  testNamespace,
		PlaytestID: &pt.ID,
		Action:     "code.grant_orphaned",
	})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if got.ActorUserID != nil {
		t.Errorf("system-emitted row stored actor_user_id = %v; want nil", got.ActorUserID)
	}
}

func TestAuditLogListByPlaytest_OrdersNewestFirst(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "audit-order")
	store := repo.NewPgAuditLogStore(testPool)
	ctx := context.Background()

	actions := []string{"playtest.edit", "nda.edit", "playtest.status_transition"}
	for _, act := range actions {
		if _, err := store.Append(ctx, &repo.AuditLog{
			Namespace:  testNamespace,
			PlaytestID: &pt.ID,
			Action:     act,
		}); err != nil {
			t.Fatalf("Append %s: %v", act, err)
		}
		time.Sleep(2 * time.Millisecond)
	}

	rows, err := store.ListByPlaytest(ctx, pt.ID, 10)
	if err != nil {
		t.Fatalf("ListByPlaytest: %v", err)
	}
	if len(rows) != len(actions) {
		t.Fatalf("row count = %d, want %d", len(rows), len(actions))
	}
	if rows[0].Action != actions[len(actions)-1] {
		t.Errorf("newest row action = %q, want %q",
			rows[0].Action, actions[len(actions)-1])
	}
}
