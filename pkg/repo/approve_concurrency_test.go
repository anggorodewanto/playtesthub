package repo_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// TestApprove_ConcurrentRace_FirstWinsSecondCASMismatches mirrors the
// PRD §5.4 / errors.md row 11 contract: two admins clicking Approve on
// the same PENDING applicant simultaneously — first wins; second
// surfaces ErrStatusCASMismatch (which the service maps to
// FailedPrecondition "applicant already approved"). The test runs the
// full Reserve → FencedFinalize → ApproveCAS pipeline inside two
// concurrent transactions against a real Postgres so the row-level
// locks the SQL relies on are actually exercised.
func TestApprove_ConcurrentRace_FirstWinsSecondCASMismatches(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "apl-approve-concurrent")
	applicants := repo.NewPgApplicantStore(testPool)
	codes := repo.NewPgCodeStore(testPool)
	runner := repo.NewPgTxRunner(testPool)
	ctx := context.Background()

	a, err := applicants.Insert(ctx, newApplicant(pt.ID, uuid.New()))
	if err != nil {
		t.Fatalf("seed applicant: %v", err)
	}
	if _, err := codes.BulkInsert(ctx, pt.ID, []string{"RACE-A", "RACE-B"}); err != nil {
		t.Fatalf("seed codes: %v", err)
	}

	// Each goroutine runs the canonical approve-pipeline in one tx.
	// Only one tx can ApproveCAS the row PENDING → APPROVED — the other
	// must see ErrStatusCASMismatch.
	type result struct {
		err error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func(actor uuid.UUID) {
			defer wg.Done()
			<-start
			err := runner.InTx(ctx, func(q repo.Querier) error {
				code, e := codes.Reserve(ctx, q, pt.ID, actor)
				if e != nil {
					return e
				}
				rows, e := codes.FencedFinalize(ctx, q, code.ID, actor, *code.ReservedAt)
				if e != nil {
					return e
				}
				if rows != 1 {
					return errors.New("fenced finalize did not affect 1 row")
				}
				_, e = applicants.ApproveCAS(ctx, q, a.ID, code.ID, time.Now().UTC().Truncate(time.Microsecond))
				return e
			})
			results <- result{err: err}
		}(uuid.New())
	}
	close(start)
	wg.Wait()
	close(results)

	var wins, casMisses int
	for r := range results {
		switch {
		case r.err == nil:
			wins++
		case errors.Is(r.err, repo.ErrStatusCASMismatch):
			casMisses++
		default:
			t.Errorf("unexpected error from approve goroutine: %v", r.err)
		}
	}
	if wins != 1 || casMisses != 1 {
		t.Errorf("wins=%d casMisses=%d, want 1/1", wins, casMisses)
	}

	final, err := applicants.GetByID(ctx, a.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if final.Status != repo.ApplicantStatusApproved {
		t.Errorf("final status = %q, want APPROVED", final.Status)
	}
	// Exactly one code should be GRANTED, the other RESERVED-then-
	// rolled-back (still UNUSED — the loser's tx rolled back its
	// reservation).
	rows, err := testPool.Query(ctx,
		`SELECT state, COUNT(*) FROM code WHERE playtest_id=$1 GROUP BY state ORDER BY state`,
		pt.ID)
	if err != nil {
		t.Fatalf("count code states: %v", err)
	}
	defer rows.Close()
	got := map[string]int{}
	for rows.Next() {
		var st string
		var n int
		if err := rows.Scan(&st, &n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got[st] = n
	}
	if got[repo.CodeStateGranted] != 1 {
		t.Errorf("GRANTED count = %d, want 1; full counts=%+v", got[repo.CodeStateGranted], got)
	}
}

// TestPgTxRunner_RollsBackOnError ensures the transaction wrapper
// rolls back when fn returns a non-nil error — the approve flow's
// 0-row finalize defensive path relies on this so a half-applied tx
// does not leak a phantom RESERVED row.
func TestPgTxRunner_RollsBackOnError(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "tx-rollback")
	codes := repo.NewPgCodeStore(testPool)
	runner := repo.NewPgTxRunner(testPool)
	ctx := context.Background()
	if _, err := codes.BulkInsert(ctx, pt.ID, []string{"ROLLBACK-1"}); err != nil {
		t.Fatalf("seed code: %v", err)
	}

	sentinel := errors.New("rollback")
	err := runner.InTx(ctx, func(q repo.Querier) error {
		if _, e := codes.Reserve(ctx, q, pt.ID, uuid.New()); e != nil {
			return e
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}

	// Inspect the row directly: it should still be UNUSED with no
	// reserved_by/reserved_at because the rollback erased the Reserve.
	var state string
	var reservedBy *uuid.UUID
	if err := testPool.QueryRow(ctx,
		`SELECT state, reserved_by FROM code WHERE playtest_id=$1`, pt.ID).
		Scan(&state, &reservedBy); err != nil {
		t.Fatalf("post-rollback select: %v", err)
	}
	if state != repo.CodeStateUnused {
		t.Errorf("state after rollback = %q, want UNUSED", state)
	}
	if reservedBy != nil {
		t.Errorf("reserved_by after rollback = %v, want nil", reservedBy)
	}
}

// suppressUnused keeps pgx imported even if future test edits remove
// every direct reference; pgx's classifier helpers are part of the
// approve-flow contract and a future test in this file may need them.
var _ = pgx.ReadOnly
