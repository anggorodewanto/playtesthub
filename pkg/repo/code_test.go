package repo_test

import (
	"context"
	"errors"
	"testing"

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
