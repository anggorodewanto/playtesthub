package service

import (
	"testing"
	"time"

	"github.com/google/uuid"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// withAutoApproveStores wires the full set of stores Signup's M5.A
// auto-approve chain needs: codes + tx runner + audit. Tx runner is
// the fake (nil-Querier) variant — concurrency is exercised against a
// live DB in auto_approve_concurrency_test.go.
func withAutoApproveStores(t *testing.T) approveTestRig {
	t.Helper()
	svr, pt, ap := newTestServer()
	codes := &fakeCodeStore{}
	audit := &fakeAuditLogStore{}
	dm := &fakeDMEnqueuer{}
	svr = svr.
		WithCodeStore(codes).
		WithAuditLogStore(audit).
		WithTxRunner(fakeTxRunner{}).
		WithDMQueue(dm).
		WithDiscordLookup(&fakeHandleLookup{handle: "Alice"})
	return approveTestRig{svr: svr, playtests: pt, applicants: ap, codes: codes, audit: audit}
}

func autoApprovePlaytest(slug string, limit int32) *repo.Playtest {
	pt := steamKeysPlaytest(slug)
	pt.AutoApprove = true
	pt.AutoApproveLimit = &limit
	return pt
}

func TestSignup_AutoApprove_PromotesUnderCap(t *testing.T) {
	rig := withAutoApproveStores(t)
	pt := autoApprovePlaytest("auto-happy", 5)
	rig.playtests.rows = append(rig.playtests.rows, pt)
	seedPoolCode(rig, pt, "STEAM-1")

	userID := uuid.New()
	resp, err := rig.svr.Signup(signupCtx(userID, "111"), &pb.SignupRequest{
		Slug:      "auto-happy",
		Platforms: []pb.Platform{pb.Platform_PLATFORM_STEAM},
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if resp.GetApplicant().GetStatus() != pb.ApplicantStatus_APPLICANT_STATUS_APPROVED {
		t.Fatalf("status = %s, want APPROVED", resp.GetApplicant().GetStatus())
	}
	if resp.GetApplicant().GetGrantedCodeId() == "" {
		t.Error("granted_code_id should be populated on auto-approve")
	}

	// Audit row must be `applicant.auto_approved` — not `applicant.approve`.
	if got := len(rig.audit.rows); got != 1 {
		t.Fatalf("audit row count = %d, want 1", got)
	}
	if rig.audit.rows[0].Action != repo.ActionApplicantAutoApproved {
		t.Errorf("audit action = %q, want %q", rig.audit.rows[0].Action, repo.ActionApplicantAutoApproved)
	}
	// System-attributed (no actor) per PRD §5.4.
	if rig.audit.rows[0].ActorUserID != nil {
		t.Errorf("applicant.auto_approved must be system-emitted; got actor %v", rig.audit.rows[0].ActorUserID)
	}

	// Persisted row carries auto_approved=true.
	stored := rig.applicants.rows[0]
	if !stored.AutoApproved {
		t.Error("applicant.auto_approved = false, want true")
	}
}

func TestSignup_AutoApprove_CapReached_LeavesPending(t *testing.T) {
	rig := withAutoApproveStores(t)
	pt := autoApprovePlaytest("auto-capped", 1)
	rig.playtests.rows = append(rig.playtests.rows, pt)
	// Seed an already-auto-approved applicant so the cap is at limit.
	existingGrant := uuid.New()
	approvedAt := time.Now()
	rig.applicants.rows = append(rig.applicants.rows, &repo.Applicant{
		ID: uuid.New(), PlaytestID: pt.ID, UserID: uuid.New(),
		Status: applicantStatusApproved, GrantedCodeID: &existingGrant,
		ApprovedAt: &approvedAt, AutoApproved: true, CreatedAt: time.Now(),
	})
	// Seed an UNUSED code so we'd otherwise succeed.
	seedPoolCode(rig, pt, "STEAM-2")

	resp, err := rig.svr.Signup(signupCtx(uuid.New(), "222"), &pb.SignupRequest{
		Slug:      "auto-capped",
		Platforms: []pb.Platform{pb.Platform_PLATFORM_STEAM},
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if resp.GetApplicant().GetStatus() != pb.ApplicantStatus_APPLICANT_STATUS_PENDING {
		t.Fatalf("status = %s, want PENDING (cap hit)", resp.GetApplicant().GetStatus())
	}

	// No new auto_approved audit row — only the seeded applicant carries it.
	for _, r := range rig.audit.rows {
		if r.Action == repo.ActionApplicantAutoApproved {
			t.Errorf("unexpected applicant.auto_approved audit row after cap hit: %+v", r)
		}
	}
	// Pool code stays UNUSED — cap check must run before Reserve.
	if rig.codes.rows[0].State != repo.CodeStateUnused {
		t.Errorf("code state = %q, want UNUSED (cap hit before reserve)", rig.codes.rows[0].State)
	}
}

func TestSignup_AutoApprove_PoolEmpty_LeavesPending(t *testing.T) {
	rig := withAutoApproveStores(t)
	pt := autoApprovePlaytest("auto-empty", 5)
	rig.playtests.rows = append(rig.playtests.rows, pt)
	// No pool code seeded — Reserve returns ErrPoolEmpty inside the tx.

	resp, err := rig.svr.Signup(signupCtx(uuid.New(), "333"), &pb.SignupRequest{
		Slug:      "auto-empty",
		Platforms: []pb.Platform{pb.Platform_PLATFORM_STEAM},
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if resp.GetApplicant().GetStatus() != pb.ApplicantStatus_APPLICANT_STATUS_PENDING {
		t.Errorf("status = %s, want PENDING (pool empty silent fallback)", resp.GetApplicant().GetStatus())
	}
	if got := len(rig.audit.rows); got != 0 {
		t.Errorf("audit row count = %d, want 0 (pool-empty fallback writes no audit)", got)
	}
}

func TestSignup_AutoApproveDisabled_StaysPending(t *testing.T) {
	rig := withAutoApproveStores(t)
	pt := steamKeysPlaytest("no-auto")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	seedPoolCode(rig, pt, "STEAM-3")

	resp, err := rig.svr.Signup(signupCtx(uuid.New(), "444"), &pb.SignupRequest{
		Slug:      "no-auto",
		Platforms: []pb.Platform{pb.Platform_PLATFORM_STEAM},
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if resp.GetApplicant().GetStatus() != pb.ApplicantStatus_APPLICANT_STATUS_PENDING {
		t.Errorf("status = %s, want PENDING (auto-approve off)", resp.GetApplicant().GetStatus())
	}
	if rig.codes.rows[0].State != repo.CodeStateUnused {
		t.Errorf("code state = %q, want UNUSED (auto-approve off must not consume code)", rig.codes.rows[0].State)
	}
}

// Manual ApproveApplicant on a PENDING applicant of an auto-approve
// playtest is uncapped: even after the auto-approve cap is reached, an
// admin can still manually approve more applicants. Auto-approve is the
// applicant-driven fast path; manual approve is the admin-driven
// override.
func TestApproveApplicant_ManualBeyondAutoApproveCap(t *testing.T) {
	rig := withAutoApproveStores(t)
	pt := autoApprovePlaytest("manual-uncapped", 1)
	rig.playtests.rows = append(rig.playtests.rows, pt)
	// Seed one auto-approved applicant — cap is at the limit.
	autoGrant := uuid.New()
	autoApprovedAt := time.Now()
	rig.applicants.rows = append(rig.applicants.rows, &repo.Applicant{
		ID: uuid.New(), PlaytestID: pt.ID, UserID: uuid.New(),
		Status: applicantStatusApproved, GrantedCodeID: &autoGrant,
		ApprovedAt: &autoApprovedAt, AutoApproved: true, CreatedAt: time.Now(),
	})
	// Now seed a PENDING applicant + a pool code for the manual approve.
	pending := seedPendingApplicant(rig, pt, uuid.New())
	seedPoolCode(rig, pt, "STEAM-4")

	resp, err := rig.svr.ApproveApplicant(authCtx(uuid.New()), &pb.ApproveApplicantRequest{
		Namespace:   testNamespace,
		ApplicantId: pending.ID.String(),
	})
	if err != nil {
		t.Fatalf("manual approve over cap: %v", err)
	}
	if resp.GetApplicant().GetStatus() != pb.ApplicantStatus_APPLICANT_STATUS_APPROVED {
		t.Errorf("manual approve status = %s, want APPROVED", resp.GetApplicant().GetStatus())
	}
	// Manually-approved applicant must not be marked auto_approved.
	if resp.GetApplicant().GetAutoApproved() {
		t.Error("manual approve set auto_approved=true (must be false to keep cap accounting correct)")
	}
}
