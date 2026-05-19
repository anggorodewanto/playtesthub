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

// runAutoApproveChain mirrors pkg/service.tryAutoApprove inside one tx:
// advisory lock → capLimit check → Reserve → FencedFinalize → ApproveCAS.
// Used by both concurrency tests below — the test owns the loop and
// outcome bookkeeping, this helper owns the per-applicant SQL chain so
// the test reads like the production code path.
func runAutoApproveChain(ctx context.Context, runner repo.TxRunner, applicants *repo.PgApplicantStore, codes *repo.PgCodeStore, playtestID uuid.UUID, a *repo.Applicant, limit int) error {
	return runner.InTx(ctx, func(q repo.Querier) error {
		if _, e := q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "autoapprove:"+playtestID.String()); e != nil {
			return e
		}
		count, e := applicants.CountAutoApprovedByPlaytest(ctx, q, playtestID)
		if e != nil {
			return e
		}
		if count >= limit {
			return errCapHit
		}
		code, e := codes.Reserve(ctx, q, playtestID, a.UserID)
		if e != nil {
			return e
		}
		rows, e := codes.FencedFinalize(ctx, q, code.ID, a.UserID, *code.ReservedAt)
		if e != nil {
			return e
		}
		if rows != 1 {
			return errors.New("fenced finalize did not affect 1 row")
		}
		_, e = applicants.ApproveCAS(ctx, q, a.ID, code.ID, time.Now().UTC().Truncate(time.Microsecond), true)
		return e
	})
}

var errCapHit = errors.New("test: auto-approve capLimit reached")

// TestAutoApprove_HundredSignups_OnlyCapSucceed pins the PRD §5.4 / M5.A
// invariant: with auto_approve_limit=10 and 100 concurrent signups, the
// playtest-scoped advisory lock + capLimit query in one tx must yield exactly
// 10 wins and 90 leave-pending fallbacks (no over-approve, no DB
// errors). Mirrors the SQL chain the service runs in production.
func TestAutoApprove_HundredSignups_OnlyCapSucceed(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "auto-100")
	applicants := repo.NewPgApplicantStore(testPool)
	codes := repo.NewPgCodeStore(testPool)
	runner := repo.NewPgTxRunner(testPool)
	ctx := context.Background()

	// Seed 100 PENDING applicants up front so the burst can race on the
	// capLimit rather than on insert ordering. Seed 100 UNUSED codes so the
	// pool never bottlenecks before the capLimit.
	const N = 100
	const capLimit = 10
	seeded := make([]*repo.Applicant, 0, N)
	for i := range N {
		_ = i
		a, err := applicants.Insert(ctx, newApplicant(pt.ID, uuid.New()))
		if err != nil {
			t.Fatalf("seed applicant: %v", err)
		}
		seeded = append(seeded, a)
	}
	values := make([]string, N)
	for i := range N {
		values[i] = "K-" + uuid.NewString()
	}
	if _, err := codes.BulkInsert(ctx, pt.ID, values); err != nil {
		t.Fatalf("seed codes: %v", err)
	}

	type result struct {
		err error
	}
	results := make(chan result, N)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, a := range seeded {
		wg.Add(1)
		go func(a *repo.Applicant) {
			defer wg.Done()
			<-start
			results <- result{err: runAutoApproveChain(ctx, runner, applicants, codes, pt.ID, a, capLimit)}
		}(a)
	}
	close(start)
	wg.Wait()
	close(results)

	var wins, caps int
	for r := range results {
		switch {
		case r.err == nil:
			wins++
		case errors.Is(r.err, errCapHit):
			caps++
		default:
			t.Errorf("unexpected chain error: %v", r.err)
		}
	}
	if wins != capLimit {
		t.Errorf("wins = %d, want exactly %d (capLimit)", wins, capLimit)
	}
	if caps != N-capLimit {
		t.Errorf("capLimit-hit count = %d, want %d", caps, N-capLimit)
	}

	// DB invariants: exactly `capLimit` applicants APPROVED with
	// auto_approved=true; exactly `capLimit` GRANTED codes; the rest of the
	// pool stays UNUSED.
	var approved, autoApproved int
	if err := testPool.QueryRow(ctx,
		`SELECT COUNT(*), COUNT(*) FILTER (WHERE auto_approved=true)
		   FROM applicant WHERE playtest_id=$1 AND status='APPROVED'`, pt.ID).
		Scan(&approved, &autoApproved); err != nil {
		t.Fatalf("count approved: %v", err)
	}
	if approved != capLimit || autoApproved != capLimit {
		t.Errorf("approved=%d auto_approved=%d, want %d/%d", approved, autoApproved, capLimit, capLimit)
	}

	var granted, unused int
	if err := testPool.QueryRow(ctx,
		`SELECT
		   COUNT(*) FILTER (WHERE state='GRANTED'),
		   COUNT(*) FILTER (WHERE state='UNUSED')
		   FROM code WHERE playtest_id=$1`, pt.ID).
		Scan(&granted, &unused); err != nil {
		t.Fatalf("count codes: %v", err)
	}
	if granted != capLimit || unused != N-capLimit {
		t.Errorf("granted=%d unused=%d, want %d/%d", granted, unused, capLimit, N-capLimit)
	}
}

// TestAutoApprove_PoolEmptyMidBurst_SurplusStaysPending pins the PRD
// §5.4 pool-empty fallback: when the pool drains mid-burst, surplus
// signups surface ErrPoolEmpty from Reserve, the service swallows it
// and leaves the applicant PENDING. No DB error escapes to the player.
func TestAutoApprove_PoolEmptyMidBurst_SurplusStaysPending(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "auto-poolempty")
	applicants := repo.NewPgApplicantStore(testPool)
	codes := repo.NewPgCodeStore(testPool)
	runner := repo.NewPgTxRunner(testPool)
	ctx := context.Background()

	const N = 20
	const capLimit = 50 // capLimit deliberately above pool size — pool is the bottleneck.
	const pool = 5

	seeded := make([]*repo.Applicant, 0, N)
	for i := range N {
		_ = i
		a, err := applicants.Insert(ctx, newApplicant(pt.ID, uuid.New()))
		if err != nil {
			t.Fatalf("seed applicant: %v", err)
		}
		seeded = append(seeded, a)
	}
	values := make([]string, pool)
	for i := range pool {
		values[i] = "P-" + uuid.NewString()
	}
	if _, err := codes.BulkInsert(ctx, pt.ID, values); err != nil {
		t.Fatalf("seed codes: %v", err)
	}

	results := make(chan error, N)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, a := range seeded {
		wg.Add(1)
		go func(a *repo.Applicant) {
			defer wg.Done()
			<-start
			results <- runAutoApproveChain(ctx, runner, applicants, codes, pt.ID, a, capLimit)
		}(a)
	}
	close(start)
	wg.Wait()
	close(results)

	var wins, poolEmpty int
	for err := range results {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, repo.ErrPoolEmpty):
			poolEmpty++
		default:
			t.Errorf("unexpected chain error: %v", err)
		}
	}
	if wins != pool {
		t.Errorf("wins = %d, want pool size %d", wins, pool)
	}
	if poolEmpty != N-pool {
		t.Errorf("ErrPoolEmpty count = %d, want %d", poolEmpty, N-pool)
	}
}

// Keep pgx imported even if future edits remove every direct reference;
// concurrency tests above rely on the package being initialised so the
// pgxpool TestMain wiring is in scope.
var _ = pgx.ReadOnly
