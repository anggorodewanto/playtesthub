package repo_test

import (
	"context"
	"errors"
	"testing"
	"time"
	"unicode/utf8"

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

func TestApplicantApproveCAS_PendingTransitions(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "apl-approve")
	store := repo.NewPgApplicantStore(testPool)
	codeStore := repo.NewPgCodeStore(testPool)
	ctx := context.Background()

	a, err := store.Insert(ctx, newApplicant(pt.ID, uuid.New()))
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if _, err := codeStore.BulkInsert(ctx, pt.ID, []string{"K-1"}); err != nil {
		t.Fatalf("seed code: %v", err)
	}
	codes, err := codeStore.CountByState(ctx, pt.ID)
	if err != nil || codes[repo.CodeStateUnused] != 1 {
		t.Fatalf("seed code count = %+v err=%v", codes, err)
	}

	// Pull the seeded code id directly so we can use it as the grant.
	var codeID uuid.UUID
	if err := testPool.QueryRow(ctx, `SELECT id FROM code WHERE playtest_id=$1`, pt.ID).Scan(&codeID); err != nil {
		t.Fatalf("look up code id: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	got, err := store.ApproveCAS(ctx, testPool, a.ID, codeID, now)
	if err != nil {
		t.Fatalf("ApproveCAS: %v", err)
	}
	if got.Status != "APPROVED" {
		t.Errorf("status = %q, want APPROVED", got.Status)
	}
	if got.GrantedCodeID == nil || *got.GrantedCodeID != codeID {
		t.Errorf("granted_code_id = %v, want %v", got.GrantedCodeID, codeID)
	}
	if got.ApprovedAt == nil || !got.ApprovedAt.Equal(now) {
		t.Errorf("approved_at = %v, want %v", got.ApprovedAt, now)
	}
}

// errors.md row 11: two admins click Approve on the same PENDING
// applicant simultaneously — second caller must see CAS mismatch.
func TestApplicantApproveCAS_DoubleApproveLosesCAS(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "apl-approve-race")
	store := repo.NewPgApplicantStore(testPool)
	codeStore := repo.NewPgCodeStore(testPool)
	ctx := context.Background()

	a, err := store.Insert(ctx, newApplicant(pt.ID, uuid.New()))
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if _, err := codeStore.BulkInsert(ctx, pt.ID, []string{"K-A", "K-B"}); err != nil {
		t.Fatalf("seed codes: %v", err)
	}
	var codeIDs []uuid.UUID
	rows, err := testPool.Query(ctx, `SELECT id FROM code WHERE playtest_id=$1`, pt.ID)
	if err != nil {
		t.Fatalf("look up codes: %v", err)
	}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			t.Fatalf("scan code id: %v", err)
		}
		codeIDs = append(codeIDs, id)
	}
	rows.Close()

	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err := store.ApproveCAS(ctx, testPool, a.ID, codeIDs[0], now); err != nil {
		t.Fatalf("first ApproveCAS: %v", err)
	}
	_, err = store.ApproveCAS(ctx, testPool, a.ID, codeIDs[1], now)
	if !errors.Is(err, repo.ErrStatusCASMismatch) {
		t.Errorf("second ApproveCAS: got %v, want ErrStatusCASMismatch", err)
	}
}

func TestApplicantRejectCAS_PendingToRejected(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "apl-reject")
	store := repo.NewPgApplicantStore(testPool)
	ctx := context.Background()

	a, err := store.Insert(ctx, newApplicant(pt.ID, uuid.New()))
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	reason := "ineligible region"
	got, err := store.RejectCAS(ctx, testPool, a.ID, &reason)
	if err != nil {
		t.Fatalf("RejectCAS: %v", err)
	}
	if got.Status != repo.ApplicantStatusRejected {
		t.Errorf("status = %q, want REJECTED", got.Status)
	}
	if got.RejectionReason == nil || *got.RejectionReason != reason {
		t.Errorf("rejection_reason = %v, want %q", got.RejectionReason, reason)
	}

	// Second reject loses the CAS.
	_, err = store.RejectCAS(ctx, testPool, a.ID, &reason)
	if !errors.Is(err, repo.ErrStatusCASMismatch) {
		t.Errorf("second RejectCAS: got %v, want ErrStatusCASMismatch", err)
	}
}

// PRD §5.4 / dm-queue.md: lastDmError is byte-truncated to 500 bytes
// preserving valid UTF-8 codepoint boundaries. Build a string whose
// untruncated length straddles the 500th byte mid-codepoint and assert
// the persisted result has byte length ≤500 and decodes cleanly as
// UTF-8.
func TestApplicantUpdateDMStatus_TruncatesUTF8At500Bytes(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "apl-dm-trunc")
	store := repo.NewPgApplicantStore(testPool)
	ctx := context.Background()

	a, err := store.Insert(ctx, newApplicant(pt.ID, uuid.New()))
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// "é" is two UTF-8 bytes (0xC3 0xA9). 251 copies = 502 bytes; the
	// naive byte cut at index 500 lands mid-codepoint.
	const e = "é"
	long := ""
	for range 251 {
		long += e
	}
	if len(long) != 502 {
		t.Fatalf("test fixture wrong: len=%d want 502", len(long))
	}

	now := time.Now().UTC().Truncate(time.Microsecond)
	got, err := store.UpdateDMStatus(ctx, a.ID, "failed", now, &long)
	if err != nil {
		t.Fatalf("UpdateDMStatus: %v", err)
	}
	if got.LastDMError == nil {
		t.Fatal("last_dm_error nil after update")
	}
	if got.LastDMStatus == nil || *got.LastDMStatus != "failed" {
		t.Errorf("last_dm_status = %v, want failed", got.LastDMStatus)
	}
	if !got.LastDMAttemptAt.Equal(now) {
		t.Errorf("last_dm_attempt_at = %v, want %v", got.LastDMAttemptAt, now)
	}
	if len(*got.LastDMError) > 500 {
		t.Errorf("len(last_dm_error) = %d, want ≤500", len(*got.LastDMError))
	}
	if !utf8.ValidString(*got.LastDMError) {
		t.Errorf("truncated last_dm_error not valid UTF-8: %q", *got.LastDMError)
	}
	// Final char must be a complete "é" — naive cut would have left a
	// dangling 0xC3.
	if (*got.LastDMError)[len(*got.LastDMError)-2:] != "é" {
		t.Errorf("trailing 2 bytes not 'é': got %q", (*got.LastDMError)[len(*got.LastDMError)-2:])
	}
}

// TestApplicantListPaged_PaginatesNewestFirstWithStableTiebreak walks
// the cursor across the full set: first page → cursor → second page,
// no overlaps, no gaps. Verifies the (created_at, id) DESC tuple
// ordering used by the M2 ListApplicants RPC.
func TestApplicantListPaged_PaginatesNewestFirstWithStableTiebreak(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "apl-paged")
	store := repo.NewPgApplicantStore(testPool)
	ctx := context.Background()

	const total = 7
	inserted := make([]uuid.UUID, 0, total)
	for range total {
		a, err := store.Insert(ctx, newApplicant(pt.ID, uuid.New()))
		if err != nil {
			t.Fatalf("seed: %v", err)
		}
		inserted = append(inserted, a.ID)
		time.Sleep(2 * time.Millisecond)
	}

	page1, err := store.ListPaged(ctx, repo.ApplicantPageQuery{PlaytestID: pt.ID, Limit: 3})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1.Rows) != 3 || page1.NextPageToken == "" {
		t.Fatalf("page1 len=%d token=%q, want 3 + non-empty", len(page1.Rows), page1.NextPageToken)
	}

	page2, err := store.ListPaged(ctx, repo.ApplicantPageQuery{PlaytestID: pt.ID, Limit: 3, PageToken: page1.NextPageToken})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2.Rows) != 3 || page2.NextPageToken == "" {
		t.Fatalf("page2 len=%d token=%q, want 3 + non-empty", len(page2.Rows), page2.NextPageToken)
	}

	page3, err := store.ListPaged(ctx, repo.ApplicantPageQuery{PlaytestID: pt.ID, Limit: 3, PageToken: page2.NextPageToken})
	if err != nil {
		t.Fatalf("page3: %v", err)
	}
	if len(page3.Rows) != 1 || page3.NextPageToken != "" {
		t.Errorf("page3 len=%d token=%q, want 1 + empty", len(page3.Rows), page3.NextPageToken)
	}

	// Newest-first: page1[0] is the last seeded applicant.
	if page1.Rows[0].ID != inserted[total-1] {
		t.Errorf("DESC ordering: page1[0]=%v want %v", page1.Rows[0].ID, inserted[total-1])
	}

	// No overlaps + no gaps across pages.
	seen := map[uuid.UUID]bool{}
	for _, p := range []*repo.ApplicantPage{page1, page2, page3} {
		for _, r := range p.Rows {
			if seen[r.ID] {
				t.Errorf("duplicate row id across pages: %v", r.ID)
			}
			seen[r.ID] = true
		}
	}
	if len(seen) != total {
		t.Errorf("total rows across pages = %d, want %d", len(seen), total)
	}
}

func TestApplicantListPaged_StatusAndDMFailedFilters(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "apl-paged-filters")
	store := repo.NewPgApplicantStore(testPool)
	ctx := context.Background()

	pending, err := store.Insert(ctx, newApplicant(pt.ID, uuid.New()))
	if err != nil {
		t.Fatalf("seed pending: %v", err)
	}
	approved, err := store.Insert(ctx, newApplicant(pt.ID, uuid.New()))
	if err != nil {
		t.Fatalf("seed approved: %v", err)
	}
	approved.Status = repo.ApplicantStatusApproved
	if _, err := store.UpdateStatus(ctx, approved); err != nil {
		t.Fatalf("approve: %v", err)
	}
	failedDM := "failed"
	if _, err := store.UpdateDMStatus(ctx, approved.ID, failedDM, time.Now().UTC(), nil); err != nil {
		t.Fatalf("dm-fail: %v", err)
	}
	_ = pending // unused; only counted via the filter result

	approvedPage, err := store.ListPaged(ctx, repo.ApplicantPageQuery{
		PlaytestID: pt.ID,
		Status:     repo.ApplicantStatusApproved,
	})
	if err != nil {
		t.Fatalf("approved page: %v", err)
	}
	if len(approvedPage.Rows) != 1 || approvedPage.Rows[0].ID != approved.ID {
		t.Errorf("status filter: got %d rows", len(approvedPage.Rows))
	}

	dmFailedPage, err := store.ListPaged(ctx, repo.ApplicantPageQuery{
		PlaytestID:   pt.ID,
		DMFailedOnly: true,
	})
	if err != nil {
		t.Fatalf("dm-failed page: %v", err)
	}
	if len(dmFailedPage.Rows) != 1 || dmFailedPage.Rows[0].ID != approved.ID {
		t.Errorf("dm-failed filter: got %d rows", len(dmFailedPage.Rows))
	}
}

func TestApplicantListPaged_BadPageToken_ReturnsErrInvalidPageToken(t *testing.T) {
	truncateAll(t)
	pt := seedPlaytest(t, "apl-paged-badtoken")
	store := repo.NewPgApplicantStore(testPool)

	_, err := store.ListPaged(context.Background(), repo.ApplicantPageQuery{
		PlaytestID: pt.ID,
		PageToken:  "this-is-not-base64!",
	})
	if !errors.Is(err, repo.ErrInvalidPageToken) {
		t.Errorf("got %v, want ErrInvalidPageToken", err)
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
	inserted.Status = repo.ApplicantStatusRejected
	inserted.RejectionReason = &reason

	updated, err := store.UpdateStatus(ctx, inserted)
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if updated.Status != repo.ApplicantStatusRejected {
		t.Errorf("status = %q, want REJECTED", updated.Status)
	}
	if updated.RejectionReason == nil || *updated.RejectionReason != reason {
		t.Errorf("rejection_reason round-trip broke: got %v", updated.RejectionReason)
	}
}
