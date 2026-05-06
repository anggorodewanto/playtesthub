package repo_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// List powers the M3 ListAuditLog RPC. Cursor pagination is
// (created_at, id) DESC, DESC; actor + action filters compose; the
// `system` magic value matches actor_user_id IS NULL per PRD §4.7.

func TestAuditLogList_Pagination(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "audit-list-page")
	store := repo.NewPgAuditLogStore(testPool)
	ctx := context.Background()

	for i := range 5 {
		if _, err := store.Append(ctx, &repo.AuditLog{
			Namespace:  testNamespace,
			PlaytestID: &pt.ID,
			Action:     "playtest.edit",
		}); err != nil {
			t.Fatalf("seed Append %d: %v", i, err)
		}
		// Ensure created_at strictly orders rows so the cursor can
		// resolve them deterministically; without this two rows
		// share a millisecond and the ID tiebreak masks ordering
		// bugs.
		time.Sleep(2 * time.Millisecond)
	}

	page, err := store.List(ctx, repo.AuditLogPageQuery{
		PlaytestID: pt.ID,
		Limit:      2,
	})
	if err != nil {
		t.Fatalf("List page1: %v", err)
	}
	if len(page.Rows) != 2 {
		t.Errorf("page1 len = %d, want 2", len(page.Rows))
	}
	if page.NextPageToken == "" {
		t.Errorf("page1 NextPageToken empty; want non-empty")
	}

	page2, err := store.List(ctx, repo.AuditLogPageQuery{
		PlaytestID: pt.ID,
		Limit:      2,
		PageToken:  page.NextPageToken,
	})
	if err != nil {
		t.Fatalf("List page2: %v", err)
	}
	if len(page2.Rows) != 2 {
		t.Errorf("page2 len = %d, want 2", len(page2.Rows))
	}

	page3, err := store.List(ctx, repo.AuditLogPageQuery{
		PlaytestID: pt.ID,
		Limit:      2,
		PageToken:  page2.NextPageToken,
	})
	if err != nil {
		t.Fatalf("List page3: %v", err)
	}
	if len(page3.Rows) != 1 {
		t.Errorf("page3 len = %d, want 1", len(page3.Rows))
	}
	if page3.NextPageToken != "" {
		t.Errorf("page3 NextPageToken = %q, want empty", page3.NextPageToken)
	}
}

// `system` ActorFilter must match rows where actor_user_id IS NULL.
// PRD §4.7 / §5.7: the admin viewer's filter pill maps directly to
// this magic string.
func TestAuditLogList_ActorFilterSystem(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "audit-list-actor")
	store := repo.NewPgAuditLogStore(testPool)
	ctx := context.Background()

	admin := uuid.New()
	if _, err := store.Append(ctx, &repo.AuditLog{
		Namespace:   testNamespace,
		PlaytestID:  &pt.ID,
		ActorUserID: &admin,
		Action:      "applicant.approve",
	}); err != nil {
		t.Fatalf("seed admin row: %v", err)
	}
	if _, err := store.Append(ctx, &repo.AuditLog{
		Namespace:  testNamespace,
		PlaytestID: &pt.ID,
		Action:     "code.grant_orphaned",
	}); err != nil {
		t.Fatalf("seed system row: %v", err)
	}

	page, err := store.List(ctx, repo.AuditLogPageQuery{
		PlaytestID:  pt.ID,
		ActorFilter: "system",
	})
	if err != nil {
		t.Fatalf("List system: %v", err)
	}
	if len(page.Rows) != 1 {
		t.Fatalf("system filter returned %d rows, want 1", len(page.Rows))
	}
	if page.Rows[0].Action != "code.grant_orphaned" {
		t.Errorf("system row action = %q, want code.grant_orphaned", page.Rows[0].Action)
	}
	if page.Rows[0].ActorUserID != nil {
		t.Errorf("system row ActorUserID = %v, want nil", page.Rows[0].ActorUserID)
	}
}

func TestAuditLogList_ActorFilterByUUID(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "audit-list-actor-uuid")
	store := repo.NewPgAuditLogStore(testPool)
	ctx := context.Background()

	admin1 := uuid.New()
	admin2 := uuid.New()
	for _, actor := range []uuid.UUID{admin1, admin2, admin1} {
		a := actor
		if _, err := store.Append(ctx, &repo.AuditLog{
			Namespace:   testNamespace,
			PlaytestID:  &pt.ID,
			ActorUserID: &a,
			Action:      "playtest.edit",
		}); err != nil {
			t.Fatalf("seed actor row: %v", err)
		}
	}

	page, err := store.List(ctx, repo.AuditLogPageQuery{
		PlaytestID:  pt.ID,
		ActorFilter: admin1.String(),
	})
	if err != nil {
		t.Fatalf("List by uuid: %v", err)
	}
	if len(page.Rows) != 2 {
		t.Errorf("admin1 filter returned %d rows, want 2", len(page.Rows))
	}
	for _, r := range page.Rows {
		if r.ActorUserID == nil || *r.ActorUserID != admin1 {
			t.Errorf("row actor = %v, want %v", r.ActorUserID, admin1)
		}
	}
}

func TestAuditLogList_ActionFilterExactMatch(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "audit-list-action")
	store := repo.NewPgAuditLogStore(testPool)
	ctx := context.Background()

	for _, act := range []string{"applicant.approve", "applicant.reject", "applicant.approve"} {
		if _, err := store.Append(ctx, &repo.AuditLog{
			Namespace:  testNamespace,
			PlaytestID: &pt.ID,
			Action:     act,
		}); err != nil {
			t.Fatalf("seed %s: %v", act, err)
		}
	}

	page, err := store.List(ctx, repo.AuditLogPageQuery{
		PlaytestID:   pt.ID,
		ActionFilter: "applicant.approve",
	})
	if err != nil {
		t.Fatalf("List action: %v", err)
	}
	if len(page.Rows) != 2 {
		t.Errorf("action filter returned %d rows, want 2", len(page.Rows))
	}
	for _, r := range page.Rows {
		if r.Action != "applicant.approve" {
			t.Errorf("row action = %q, want applicant.approve", r.Action)
		}
	}
}

func TestAuditLogList_RejectsBadToken(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "audit-list-bad-token")
	store := repo.NewPgAuditLogStore(testPool)

	_, err := store.List(context.Background(), repo.AuditLogPageQuery{
		PlaytestID: pt.ID,
		PageToken:  "##not-base64##",
	})
	if !errors.Is(err, repo.ErrInvalidAuditLogToken) {
		t.Errorf("List with bad token = %v, want ErrInvalidAuditLogToken", err)
	}
}

// Filters compose: actor + action + cursor narrow the stream
// independently. The combined query must return only rows that
// satisfy every filter.
func TestAuditLogList_ComposedFilters(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "audit-list-compose")
	store := repo.NewPgAuditLogStore(testPool)
	ctx := context.Background()

	admin := uuid.New()
	other := uuid.New()
	type seed struct {
		actor  *uuid.UUID
		action string
	}
	for _, s := range []seed{
		{&admin, "applicant.approve"},
		{&admin, "applicant.reject"},
		{&other, "applicant.approve"},
		{nil, "applicant.approve"},
	} {
		if _, err := store.Append(ctx, &repo.AuditLog{
			Namespace:   testNamespace,
			PlaytestID:  &pt.ID,
			ActorUserID: s.actor,
			Action:      s.action,
		}); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	page, err := store.List(ctx, repo.AuditLogPageQuery{
		PlaytestID:   pt.ID,
		ActorFilter:  admin.String(),
		ActionFilter: "applicant.approve",
	})
	if err != nil {
		t.Fatalf("List composed: %v", err)
	}
	if len(page.Rows) != 1 {
		t.Errorf("composed filter returned %d rows, want 1", len(page.Rows))
	}
}
