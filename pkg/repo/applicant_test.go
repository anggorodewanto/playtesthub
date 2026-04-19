package repo_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

func seedPlaytest(t *testing.T, slug string) *repo.Playtest {
	t.Helper()
	pt, err := repo.NewPgPlaytestStore(testPool).
		Create(context.Background(), newSteamKeysPlaytest(slug))
	if err != nil {
		t.Fatalf("seed playtest %q: %v", slug, err)
	}
	return pt
}

func newApplicant(playtestID uuid.UUID, userID uuid.UUID) *repo.Applicant {
	return &repo.Applicant{
		PlaytestID:    playtestID,
		UserID:        userID,
		DiscordHandle: "user#0001",
		Platforms:     []string{"STEAM"},
	}
}

func TestApplicantInsert_PopulatesDefaults(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "apl-insert")
	store := repo.NewPgApplicantStore(testPool)

	got, err := store.Insert(context.Background(), newApplicant(pt.ID, uuid.New()))
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if got.Status != "PENDING" {
		t.Errorf("status default = %q, want PENDING", got.Status)
	}
	if got.ID == uuid.Nil || got.CreatedAt.IsZero() {
		t.Error("Insert did not populate id/created_at")
	}
}

// Key M1-phase-4 invariant: (playtest_id, user_id) uniqueness is the
// signup-idempotency natural key. Service layer will catch
// ErrUniqueViolation and resolve via GetByPlaytestUser.
func TestApplicantInsert_IdempotencyKeyViolation(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "apl-idem")
	store := repo.NewPgApplicantStore(testPool)
	ctx := context.Background()

	userID := uuid.New()
	if _, err := store.Insert(ctx, newApplicant(pt.ID, userID)); err != nil {
		t.Fatalf("first Insert: %v", err)
	}
	_, err := store.Insert(ctx, newApplicant(pt.ID, userID))
	if !errors.Is(err, repo.ErrUniqueViolation) {
		t.Errorf("duplicate (playtest,user): got %v, want ErrUniqueViolation", err)
	}
}

func TestApplicantGetByPlaytestUser_RoundTrip(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "apl-lookup")
	store := repo.NewPgApplicantStore(testPool)
	ctx := context.Background()

	userID := uuid.New()
	inserted, err := store.Insert(ctx, newApplicant(pt.ID, userID))
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	got, err := store.GetByPlaytestUser(ctx, pt.ID, userID)
	if err != nil {
		t.Fatalf("GetByPlaytestUser: %v", err)
	}
	if got.ID != inserted.ID {
		t.Errorf("got id %v, want %v", got.ID, inserted.ID)
	}
}

func TestApplicantGetByPlaytestUser_NotFound(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "apl-miss")
	store := repo.NewPgApplicantStore(testPool)

	_, err := store.GetByPlaytestUser(context.Background(), pt.ID, uuid.New())
	if !errors.Is(err, repo.ErrNotFound) {
		t.Errorf("missing applicant: got %v, want ErrNotFound", err)
	}
}

func TestApplicantListByPlaytest_FiltersAndOrders(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "apl-list")
	store := repo.NewPgApplicantStore(testPool)
	ctx := context.Background()

	// Insert three applicants, small sleeps to guarantee strictly
	// monotonic created_at values (DESC ordering check).
	ids := make([]uuid.UUID, 0, 3)
	for i := range 3 {
		a, err := store.Insert(ctx, newApplicant(pt.ID, uuid.New()))
		if err != nil {
			t.Fatalf("Insert %d: %v", i, err)
		}
		ids = append(ids, a.ID)
		time.Sleep(2 * time.Millisecond)
	}

	// Approve the middle one.
	mid, err := store.GetByID(ctx, ids[1])
	if err != nil {
		t.Fatalf("GetByID mid: %v", err)
	}
	mid.Status = "APPROVED"
	now := time.Now().UTC()
	mid.ApprovedAt = &now
	if _, err := store.UpdateStatus(ctx, mid); err != nil {
		t.Fatalf("approve mid: %v", err)
	}

	pending, err := store.ListByPlaytest(ctx, pt.ID, "PENDING")
	if err != nil {
		t.Fatalf("ListByPlaytest PENDING: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("PENDING count = %d, want 2", len(pending))
	}
	// Newest-first: ids[2] before ids[0].
	if len(pending) == 2 && pending[0].ID != ids[2] {
		t.Errorf("DESC ordering: first = %v, want %v", pending[0].ID, ids[2])
	}

	all, err := store.ListByPlaytest(ctx, pt.ID, "")
	if err != nil {
		t.Fatalf("ListByPlaytest all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("all count = %d, want 3", len(all))
	}
}

func TestApplicantUpdateStatus_RoundTrip(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "apl-upd")
	store := repo.NewPgApplicantStore(testPool)
	ctx := context.Background()

	inserted, err := store.Insert(ctx, newApplicant(pt.ID, uuid.New()))
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	reason := "does not meet criteria"
	inserted.Status = "REJECTED"
	inserted.RejectionReason = &reason

	updated, err := store.UpdateStatus(ctx, inserted)
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if updated.Status != "REJECTED" {
		t.Errorf("status = %q, want REJECTED", updated.Status)
	}
	if updated.RejectionReason == nil || *updated.RejectionReason != reason {
		t.Errorf("rejection_reason round-trip broke: got %v", updated.RejectionReason)
	}
}
