package repo_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

func TestCodeBulkInsert_CountsAndDefaults(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "code-bulk")
	store := repo.NewPgCodeStore(testPool)

	values := []string{"KEY-AAA-111", "KEY-BBB-222", "KEY-CCC-333"}
	inserted, err := store.BulkInsert(context.Background(), pt.ID, values)
	if err != nil {
		t.Fatalf("BulkInsert: %v", err)
	}
	if inserted != len(values) {
		t.Errorf("inserted = %d, want %d", inserted, len(values))
	}

	counts, err := store.CountByState(context.Background(), pt.ID)
	if err != nil {
		t.Fatalf("CountByState: %v", err)
	}
	if counts[repo.CodeStateUnused] != len(values) {
		t.Errorf("UNUSED count = %d, want %d", counts[repo.CodeStateUnused], len(values))
	}
	if counts[repo.CodeStateReserved] != 0 || counts[repo.CodeStateGranted] != 0 {
		t.Errorf("unexpected non-UNUSED state counts: %+v", counts)
	}
}

func TestCodeBulkInsert_EmptyBatch(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "code-empty")
	store := repo.NewPgCodeStore(testPool)

	n, err := store.BulkInsert(context.Background(), pt.ID, nil)
	if err != nil {
		t.Fatalf("BulkInsert empty: %v", err)
	}
	if n != 0 {
		t.Errorf("empty batch inserted %d rows, want 0", n)
	}
}

// Key M1-phase-4 invariant (STATUS.md): UNIQUE (playtest_id, value) on
// Code. CSV re-upload with a repeated key must fail the batch, not
// silently merge.
func TestCodeBulkInsert_DuplicateValueViolation(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "code-dup")
	store := repo.NewPgCodeStore(testPool)
	ctx := context.Background()

	if _, err := store.BulkInsert(ctx, pt.ID, []string{"dup-key"}); err != nil {
		t.Fatalf("first BulkInsert: %v", err)
	}

	_, err := store.BulkInsert(ctx, pt.ID, []string{"dup-key"})
	if !errors.Is(err, repo.ErrUniqueViolation) {
		t.Errorf("duplicate value: got %v, want ErrUniqueViolation", err)
	}
}

// The same value is allowed in different playtests; uniqueness is
// scoped per playtest_id.
func TestCodeBulkInsert_SameValueAcrossPlaytestsOK(t *testing.T) {
	truncateAll(t)
	pt1 := seedPlaytest(t, "code-cross-a")
	pt2 := seedPlaytest(t, "code-cross-b")
	store := repo.NewPgCodeStore(testPool)
	ctx := context.Background()

	if _, err := store.BulkInsert(ctx, pt1.ID, []string{"shared-key"}); err != nil {
		t.Fatalf("insert into pt1: %v", err)
	}
	if _, err := store.BulkInsert(ctx, pt2.ID, []string{"shared-key"}); err != nil {
		t.Errorf("insert into pt2: %v", err)
	}
}

func TestCodeReserve_HappyPath(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "code-reserve-happy")
	store := repo.NewPgCodeStore(testPool)
	ctx := context.Background()

	if _, err := store.BulkInsert(ctx, pt.ID, []string{"K-1", "K-2", "K-3"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	user := uuid.New()
	got, err := store.Reserve(ctx, testPool, pt.ID, user)
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if got.State != repo.CodeStateReserved {
		t.Errorf("state = %q, want RESERVED", got.State)
	}
	if got.ReservedBy == nil || *got.ReservedBy != user {
		t.Errorf("reserved_by = %v, want %v", got.ReservedBy, user)
	}
	if got.ReservedAt == nil {
		t.Error("reserved_at not populated")
	}

	counts, err := store.CountByState(ctx, pt.ID)
	if err != nil {
		t.Fatalf("CountByState: %v", err)
	}
	if counts[repo.CodeStateReserved] != 1 || counts[repo.CodeStateUnused] != 2 {
		t.Errorf("counts = %+v, want RESERVED=1 UNUSED=2", counts)
	}
}

// errors.md rows 12-13: empty pool is the model-specific
// ResourceExhausted case. Repo surfaces it as the sentinel ErrPoolEmpty.
func TestCodeReserve_EmptyPoolReturnsSentinel(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "code-reserve-empty")
	store := repo.NewPgCodeStore(testPool)

	_, err := store.Reserve(context.Background(), testPool, pt.ID, uuid.New())
	if !errors.Is(err, repo.ErrPoolEmpty) {
		t.Errorf("empty pool: got %v, want ErrPoolEmpty", err)
	}
}

func TestCodeFencedFinalize_HappyPath(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "code-finalize-happy")
	store := repo.NewPgCodeStore(testPool)
	ctx := context.Background()

	if _, err := store.BulkInsert(ctx, pt.ID, []string{"K-1"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	user := uuid.New()
	reserved, err := store.Reserve(ctx, testPool, pt.ID, user)
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	rows, err := store.FencedFinalize(ctx, testPool, reserved.ID, user, *reserved.ReservedAt)
	if err != nil {
		t.Fatalf("FencedFinalize: %v", err)
	}
	if rows != 1 {
		t.Errorf("rows affected = %d, want 1", rows)
	}

	final, err := store.GetByID(ctx, reserved.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if final.State != repo.CodeStateGranted {
		t.Errorf("state = %q, want GRANTED", final.State)
	}
	if final.GrantedAt == nil {
		t.Error("granted_at not populated")
	}
}

// schema.md §"Approve flow" (STATUS.md M2 phase 3): the canonical
// fenced UPDATE returns 0 rows when reservedBy / reservedAt change
// between reserve and finalize — the reclaim-and-steal scenario.
func TestCodeFencedFinalize_ReclaimAndStealReturnsZeroRows(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "code-finalize-stolen")
	store := repo.NewPgCodeStore(testPool)
	ctx := context.Background()

	if _, err := store.BulkInsert(ctx, pt.ID, []string{"K-X"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	userA := uuid.New()
	reserved, err := store.Reserve(ctx, testPool, pt.ID, userA)
	if err != nil {
		t.Fatalf("Reserve A: %v", err)
	}
	originalReservedAt := *reserved.ReservedAt

	// Reclaim+steal scenario: the reclaim worker flips the row back to
	// UNUSED, then user B reserves it. fenced UPDATE keyed on (userA,
	// originalReservedAt) must affect 0 rows.
	if _, err := testPool.Exec(ctx, `
		UPDATE code SET state='UNUSED', reserved_by=NULL, reserved_at=NULL WHERE id=$1`, reserved.ID); err != nil {
		t.Fatalf("simulate reclaim: %v", err)
	}
	userB := uuid.New()
	if _, err := store.Reserve(ctx, testPool, pt.ID, userB); err != nil {
		t.Fatalf("Reserve B: %v", err)
	}

	rows, err := store.FencedFinalize(ctx, testPool, reserved.ID, userA, originalReservedAt)
	if err != nil {
		t.Fatalf("FencedFinalize: %v", err)
	}
	if rows != 0 {
		t.Errorf("rows affected = %d, want 0 (reclaim-and-steal)", rows)
	}

	// The row stayed RESERVED for B, not GRANTED.
	final, err := store.GetByID(ctx, reserved.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if final.State != repo.CodeStateReserved {
		t.Errorf("state = %q, want RESERVED (B's reservation)", final.State)
	}
	if final.ReservedBy == nil || *final.ReservedBy != userB {
		t.Errorf("reserved_by = %v, want %v", final.ReservedBy, userB)
	}
}

// Reclaim releases RESERVED rows past TTL; younger reservations stay.
func TestCodeReclaim_ReleasesStaleReservations(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "code-reclaim")
	store := repo.NewPgCodeStore(testPool)
	ctx := context.Background()

	if _, err := store.BulkInsert(ctx, pt.ID, []string{"K-1", "K-2"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// One stale (reserved 10 minutes ago), one fresh (just reserved).
	if _, err := testPool.Exec(ctx, `
		UPDATE code
		   SET state='RESERVED', reserved_by=$1, reserved_at=NOW() - INTERVAL '10 minutes'
		 WHERE playtest_id=$2 AND value='K-1'`, uuid.New(), pt.ID); err != nil {
		t.Fatalf("seed stale: %v", err)
	}
	if _, err := store.Reserve(ctx, testPool, pt.ID, uuid.New()); err != nil {
		t.Fatalf("Reserve fresh: %v", err)
	}

	released, err := store.Reclaim(ctx, 5*time.Minute)
	if err != nil {
		t.Fatalf("Reclaim: %v", err)
	}
	if released != 1 {
		t.Errorf("released = %d, want 1", released)
	}

	counts, err := store.CountByState(ctx, pt.ID)
	if err != nil {
		t.Fatalf("CountByState: %v", err)
	}
	if counts[repo.CodeStateUnused] != 1 || counts[repo.CodeStateReserved] != 1 {
		t.Errorf("post-reclaim counts = %+v, want UNUSED=1 RESERVED=1", counts)
	}
}

func TestCodeBulkInsert_StateEnumRejection(t *testing.T) {
	// The table-level CHECK constraint forbids unknown state values.
	// CopyFrom always writes UNUSED (the default), so we exercise the
	// CHECK via a raw UPDATE. This guards against future code paths
	// that build state strings dynamically.
	truncateAll(t)
	pt := seedPlaytest(t, "code-enum")
	store := repo.NewPgCodeStore(testPool)
	ctx := context.Background()

	if _, err := store.BulkInsert(ctx, pt.ID, []string{"key-1"}); err != nil {
		t.Fatalf("BulkInsert: %v", err)
	}
	_, err := testPool.Exec(ctx,
		`UPDATE code SET state = 'NOT_A_STATE' WHERE playtest_id = $1`, pt.ID)
	if err == nil {
		t.Fatal("bad state update: got nil error, want CHECK violation")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Errorf("bad state update: got %v, want SQLSTATE 23514", err)
	}
}
