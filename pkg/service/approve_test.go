package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// fakeTxRunner runs fn against a nil Querier — the in-memory fakes
// never consult the arg. Exists only so the service can call
// s.txRunner.InTx; the real repo.PgTxRunner runs in the integration
// suite.
type fakeTxRunner struct{}

func (fakeTxRunner) InTx(ctx context.Context, fn func(q repo.Querier) error) error {
	return fn(nil)
}

// approveTestRig wires the full set of stores ApproveApplicant /
// RejectApplicant / ListApplicants / GetGrantedCode need: playtests,
// applicants, codes, audit log, and a tx runner.
type approveTestRig struct {
	svr        *PlaytesthubServiceServer
	playtests  *fakePlaytestStore
	applicants *fakeApplicantStore
	codes      *fakeCodeStore
	audit      *fakeAuditLogStore
}

func withApproveStores(t *testing.T) approveTestRig {
	t.Helper()
	svr, pt, ap := newTestServer()
	codes := &fakeCodeStore{}
	audit := &fakeAuditLogStore{}
	svr = svr.WithCodeStore(codes).WithAuditLogStore(audit).WithTxRunner(fakeTxRunner{})
	return approveTestRig{svr: svr, playtests: pt, applicants: ap, codes: codes, audit: audit}
}

func seedPendingApplicant(rig approveTestRig, pt *repo.Playtest, userID uuid.UUID) *repo.Applicant {
	a := &repo.Applicant{
		ID:            uuid.New(),
		PlaytestID:    pt.ID,
		UserID:        userID,
		DiscordHandle: "Player",
		Platforms:     []string{"STEAM"},
		Status:        applicantStatusPending,
		CreatedAt:     time.Now(),
	}
	rig.applicants.rows = append(rig.applicants.rows, a)
	return a
}

func seedPoolCode(rig approveTestRig, pt *repo.Playtest, value string) *repo.Code {
	c := &repo.Code{
		ID:         uuid.New(),
		PlaytestID: pt.ID,
		Value:      value,
		State:      repo.CodeStateUnused,
		CreatedAt:  time.Now(),
	}
	rig.codes.rows = append(rig.codes.rows, c)
	return c
}

// ---------------- ApproveApplicant ------------------------------------------

func TestApproveApplicant_HappyPath_FlipsApplicantAndAuditsApprove(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("approve-happy")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	a := seedPendingApplicant(rig, pt, uuid.New())
	seedPoolCode(rig, pt, "STEAM-KEY-A")

	resp, err := rig.svr.ApproveApplicant(authCtx(uuid.New()), &pb.ApproveApplicantRequest{
		Namespace:   testNamespace,
		ApplicantId: a.ID.String(),
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if resp.GetApplicant().GetStatus() != pb.ApplicantStatus_APPLICANT_STATUS_APPROVED {
		t.Fatalf("status = %s, want APPROVED", resp.GetApplicant().GetStatus())
	}
	if resp.GetApplicant().GetGrantedCodeId() == "" {
		t.Error("granted_code_id should be populated")
	}
	if got := len(rig.audit.rows); got != 1 {
		t.Fatalf("audit row count = %d, want 1", got)
	}
	if rig.audit.rows[0].Action != repo.ActionApplicantApprove {
		t.Errorf("audit action = %q, want %q", rig.audit.rows[0].Action, repo.ActionApplicantApprove)
	}
	if !strings.Contains(string(rig.audit.rows[0].After), "grantedCodeId") {
		t.Errorf("audit payload missing grantedCodeId: %s", rig.audit.rows[0].After)
	}
	// Audit must NEVER carry a raw code value (PRD §6 redaction).
	if strings.Contains(string(rig.audit.rows[0].After), "STEAM-KEY-A") {
		t.Errorf("audit payload leaked raw code value: %s", rig.audit.rows[0].After)
	}
	// Pool: code flipped GRANTED.
	if rig.codes.rows[0].State != repo.CodeStateGranted {
		t.Errorf("code state = %q, want GRANTED", rig.codes.rows[0].State)
	}
}

func TestApproveApplicant_ReApprove_ReturnsExistingNoNewAudit(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("approve-idempotent")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	grantedCode := uuid.New()
	approvedAt := time.Now().Add(-time.Hour)
	a := &repo.Applicant{
		ID: uuid.New(), PlaytestID: pt.ID, UserID: uuid.New(),
		Status: applicantStatusApproved, GrantedCodeID: &grantedCode, ApprovedAt: &approvedAt,
		CreatedAt: time.Now(),
	}
	rig.applicants.rows = append(rig.applicants.rows, a)

	resp, err := rig.svr.ApproveApplicant(authCtx(uuid.New()), &pb.ApproveApplicantRequest{
		Namespace:   testNamespace,
		ApplicantId: a.ID.String(),
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if resp.GetApplicant().GetGrantedCodeId() != grantedCode.String() {
		t.Errorf("granted_code_id = %q, want %q", resp.GetApplicant().GetGrantedCodeId(), grantedCode)
	}
	if got := len(rig.audit.rows); got != 0 {
		t.Errorf("re-approve emitted %d audit rows, want 0", got)
	}
}

func TestApproveApplicant_AlreadyRejected_FailedPrecondition(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("approve-rejected")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	a := &repo.Applicant{
		ID: uuid.New(), PlaytestID: pt.ID, UserID: uuid.New(),
		Status: applicantStatusRejected, CreatedAt: time.Now(),
	}
	rig.applicants.rows = append(rig.applicants.rows, a)

	_, err := rig.svr.ApproveApplicant(authCtx(uuid.New()), &pb.ApproveApplicantRequest{
		Namespace:   testNamespace,
		ApplicantId: a.ID.String(),
	})
	requireStatus(t, err, codes.FailedPrecondition)
	requireMsgContains(t, err, errMsgApplicantRejected)
}

func TestApproveApplicant_PoolEmpty_SteamKeysModelMessage(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("pool-empty-steam")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	a := seedPendingApplicant(rig, pt, uuid.New())
	// No pool code seeded — Reserve returns ErrPoolEmpty.

	_, err := rig.svr.ApproveApplicant(authCtx(uuid.New()), &pb.ApproveApplicantRequest{
		Namespace:   testNamespace,
		ApplicantId: a.ID.String(),
	})
	requireStatus(t, err, codes.ResourceExhausted)
	requireMsgContains(t, err, errMsgPoolEmptySteamKeys)
}

func TestApproveApplicant_PoolEmpty_AGSCampaignModelMessage(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("pool-empty-ags")
	pt.DistributionModel = distModelAGSCampaign
	rig.playtests.rows = append(rig.playtests.rows, pt)
	a := seedPendingApplicant(rig, pt, uuid.New())

	_, err := rig.svr.ApproveApplicant(authCtx(uuid.New()), &pb.ApproveApplicantRequest{
		Namespace:   testNamespace,
		ApplicantId: a.ID.String(),
	})
	requireStatus(t, err, codes.ResourceExhausted)
	requireMsgContains(t, err, errMsgPoolEmptyAGSCampaign)
}

func TestApproveApplicant_FencedFinalizeZeroRows_AbortedAndGrantOrphanedAudit(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("orphan")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	a := seedPendingApplicant(rig, pt, uuid.New())
	seedPoolCode(rig, pt, "STEAM-KEY-X")
	zero := int64(0)
	rig.codes.finalizeRowsOverride = &zero

	_, err := rig.svr.ApproveApplicant(authCtx(uuid.New()), &pb.ApproveApplicantRequest{
		Namespace:   testNamespace,
		ApplicantId: a.ID.String(),
	})
	requireStatus(t, err, codes.Aborted)
	requireMsgContains(t, err, errMsgReservationExpired)

	if got := len(rig.audit.rows); got != 1 {
		t.Fatalf("audit row count = %d, want 1 (code.grant_orphaned)", got)
	}
	if rig.audit.rows[0].Action != repo.ActionCodeGrantOrphaned {
		t.Errorf("audit action = %q, want %q", rig.audit.rows[0].Action, repo.ActionCodeGrantOrphaned)
	}
	if rig.audit.rows[0].ActorUserID != nil {
		t.Errorf("code.grant_orphaned must be system-emitted (nil actor); got %v", rig.audit.rows[0].ActorUserID)
	}
}

func TestApproveApplicant_PlaytestClosed_FailedPrecondition(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("closed")
	pt.Status = statusClosed
	rig.playtests.rows = append(rig.playtests.rows, pt)
	a := seedPendingApplicant(rig, pt, uuid.New())

	_, err := rig.svr.ApproveApplicant(authCtx(uuid.New()), &pb.ApproveApplicantRequest{
		Namespace:   testNamespace,
		ApplicantId: a.ID.String(),
	})
	requireStatus(t, err, codes.FailedPrecondition)
	requireMsgContains(t, err, errMsgPlaytestClosed)
}

func TestApproveApplicant_PlaytestDraft_FailedPrecondition(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("draft")
	pt.Status = statusDraft
	rig.playtests.rows = append(rig.playtests.rows, pt)
	a := seedPendingApplicant(rig, pt, uuid.New())

	_, err := rig.svr.ApproveApplicant(authCtx(uuid.New()), &pb.ApproveApplicantRequest{
		Namespace:   testNamespace,
		ApplicantId: a.ID.String(),
	})
	requireStatus(t, err, codes.FailedPrecondition)
	requireMsgContains(t, err, errMsgPlaytestDraft)
}

func TestApproveApplicant_SoftDeletedPlaytest_NotFound(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("deleted")
	now := time.Now()
	pt.DeletedAt = &now
	rig.playtests.rows = append(rig.playtests.rows, pt)
	a := seedPendingApplicant(rig, pt, uuid.New())

	_, err := rig.svr.ApproveApplicant(authCtx(uuid.New()), &pb.ApproveApplicantRequest{
		Namespace:   testNamespace,
		ApplicantId: a.ID.String(),
	})
	requireStatus(t, err, codes.NotFound)
}

func TestApproveApplicant_MissingActor_Unauthenticated(t *testing.T) {
	rig := withApproveStores(t)
	_, err := rig.svr.ApproveApplicant(context.Background(), &pb.ApproveApplicantRequest{
		Namespace: testNamespace, ApplicantId: uuid.New().String(),
	})
	requireStatus(t, err, codes.Unauthenticated)
}

func TestApproveApplicant_NamespaceMismatch_PermissionDenied(t *testing.T) {
	rig := withApproveStores(t)
	_, err := rig.svr.ApproveApplicant(authCtx(uuid.New()), &pb.ApproveApplicantRequest{
		Namespace: "other-namespace", ApplicantId: uuid.New().String(),
	})
	requireStatus(t, err, codes.PermissionDenied)
}

func TestApproveApplicant_BadApplicantUUID_InvalidArgument(t *testing.T) {
	rig := withApproveStores(t)
	_, err := rig.svr.ApproveApplicant(authCtx(uuid.New()), &pb.ApproveApplicantRequest{
		Namespace:   testNamespace,
		ApplicantId: "not-a-uuid",
	})
	requireStatus(t, err, codes.InvalidArgument)
}

// ---------------- RejectApplicant -------------------------------------------

func TestRejectApplicant_HappyPath_TerminalAndAudits(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("reject-happy")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	a := seedPendingApplicant(rig, pt, uuid.New())
	reason := "duplicate signup"

	resp, err := rig.svr.RejectApplicant(authCtx(uuid.New()), &pb.RejectApplicantRequest{
		Namespace:       testNamespace,
		ApplicantId:     a.ID.String(),
		RejectionReason: &reason,
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if resp.GetApplicant().GetStatus() != pb.ApplicantStatus_APPLICANT_STATUS_REJECTED {
		t.Errorf("status = %s, want REJECTED", resp.GetApplicant().GetStatus())
	}
	if resp.GetApplicant().GetRejectionReason() != reason {
		t.Errorf("rejection_reason = %q, want %q", resp.GetApplicant().GetRejectionReason(), reason)
	}
	if got := len(rig.audit.rows); got != 1 || rig.audit.rows[0].Action != repo.ActionApplicantReject {
		t.Fatalf("audit not applicant.reject: %+v", rig.audit.rows)
	}
}

func TestRejectApplicant_AlreadyRejected_Idempotent(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("reject-twice")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	a := &repo.Applicant{
		ID: uuid.New(), PlaytestID: pt.ID, UserID: uuid.New(),
		Status: applicantStatusRejected, CreatedAt: time.Now(),
	}
	rig.applicants.rows = append(rig.applicants.rows, a)

	resp, err := rig.svr.RejectApplicant(authCtx(uuid.New()), &pb.RejectApplicantRequest{
		Namespace:   testNamespace,
		ApplicantId: a.ID.String(),
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if resp.GetApplicant().GetStatus() != pb.ApplicantStatus_APPLICANT_STATUS_REJECTED {
		t.Errorf("status = %s, want REJECTED", resp.GetApplicant().GetStatus())
	}
	if got := len(rig.audit.rows); got != 0 {
		t.Errorf("re-reject emitted %d audit rows, want 0", got)
	}
}

func TestRejectApplicant_AlreadyApproved_FailedPrecondition(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("reject-approved")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	a := &repo.Applicant{
		ID: uuid.New(), PlaytestID: pt.ID, UserID: uuid.New(),
		Status: applicantStatusApproved, CreatedAt: time.Now(),
	}
	rig.applicants.rows = append(rig.applicants.rows, a)

	_, err := rig.svr.RejectApplicant(authCtx(uuid.New()), &pb.RejectApplicantRequest{
		Namespace:   testNamespace,
		ApplicantId: a.ID.String(),
	})
	requireStatus(t, err, codes.FailedPrecondition)
}

func TestRejectApplicant_PlaytestClosed_FailedPrecondition(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("reject-closed")
	pt.Status = statusClosed
	rig.playtests.rows = append(rig.playtests.rows, pt)
	a := seedPendingApplicant(rig, pt, uuid.New())

	_, err := rig.svr.RejectApplicant(authCtx(uuid.New()), &pb.RejectApplicantRequest{
		Namespace:   testNamespace,
		ApplicantId: a.ID.String(),
	})
	requireStatus(t, err, codes.FailedPrecondition)
	requireMsgContains(t, err, errMsgPlaytestClosed)
}

// ---------------- ListApplicants --------------------------------------------

func TestListApplicants_HappyPath_PaginatesNewestFirst(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("list")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	// Seed 5 applicants with distinct created_at.
	now := time.Now()
	for i := 0; i < 5; i++ {
		a := &repo.Applicant{
			ID:         uuid.New(),
			PlaytestID: pt.ID,
			UserID:     uuid.New(),
			Status:     applicantStatusPending,
			CreatedAt:  now.Add(time.Duration(i) * time.Minute),
		}
		rig.applicants.rows = append(rig.applicants.rows, a)
	}

	resp, err := rig.svr.ListApplicants(authCtx(uuid.New()), &pb.ListApplicantsRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		PageSize:   2,
	})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(resp.GetApplicants()) != 2 {
		t.Fatalf("first page len = %d, want 2", len(resp.GetApplicants()))
	}
	if resp.GetNextPageToken() == "" {
		t.Fatal("expected non-empty next_page_token after first page of 2")
	}

	resp2, err := rig.svr.ListApplicants(authCtx(uuid.New()), &pb.ListApplicantsRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		PageSize:   2,
		PageToken:  resp.GetNextPageToken(),
	})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(resp2.GetApplicants()) != 2 {
		t.Errorf("second page len = %d, want 2", len(resp2.GetApplicants()))
	}
}

func TestListApplicants_StatusFilter(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("list-status")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	pending := &repo.Applicant{
		ID: uuid.New(), PlaytestID: pt.ID, UserID: uuid.New(),
		Status: applicantStatusPending, CreatedAt: time.Now().Add(-2 * time.Minute),
	}
	approved := &repo.Applicant{
		ID: uuid.New(), PlaytestID: pt.ID, UserID: uuid.New(),
		Status: applicantStatusApproved, CreatedAt: time.Now().Add(-1 * time.Minute),
	}
	rig.applicants.rows = append(rig.applicants.rows, pending, approved)

	resp, err := rig.svr.ListApplicants(authCtx(uuid.New()), &pb.ListApplicantsRequest{
		Namespace:    testNamespace,
		PlaytestId:   pt.ID.String(),
		StatusFilter: pb.ApplicantStatus_APPLICANT_STATUS_APPROVED,
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got := len(resp.GetApplicants()); got != 1 {
		t.Fatalf("count = %d, want 1", got)
	}
	if resp.GetApplicants()[0].GetStatus() != pb.ApplicantStatus_APPLICANT_STATUS_APPROVED {
		t.Errorf("status = %s, want APPROVED", resp.GetApplicants()[0].GetStatus())
	}
}

func TestListApplicants_DMFailedFilter(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("list-dm")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	failed := "failed"
	sent := "sent"
	rig.applicants.rows = append(rig.applicants.rows,
		&repo.Applicant{
			ID: uuid.New(), PlaytestID: pt.ID, UserID: uuid.New(),
			Status: applicantStatusApproved, LastDMStatus: &failed, CreatedAt: time.Now(),
		},
		&repo.Applicant{
			ID: uuid.New(), PlaytestID: pt.ID, UserID: uuid.New(),
			Status: applicantStatusApproved, LastDMStatus: &sent, CreatedAt: time.Now().Add(-time.Minute),
		},
	)

	resp, err := rig.svr.ListApplicants(authCtx(uuid.New()), &pb.ListApplicantsRequest{
		Namespace:      testNamespace,
		PlaytestId:     pt.ID.String(),
		DmFailedFilter: true,
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got := len(resp.GetApplicants()); got != 1 {
		t.Fatalf("count = %d, want 1", got)
	}
}

func TestListApplicants_BadPageToken_InvalidArgument(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("list-bad-token")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	_, err := rig.svr.ListApplicants(authCtx(uuid.New()), &pb.ListApplicantsRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		PageToken:  "not-a-base64-token!!",
	})
	requireStatus(t, err, codes.InvalidArgument)
}

func TestListApplicants_SoftDeletedPlaytest_NotFound(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("list-deleted")
	now := time.Now()
	pt.DeletedAt = &now
	rig.playtests.rows = append(rig.playtests.rows, pt)

	_, err := rig.svr.ListApplicants(authCtx(uuid.New()), &pb.ListApplicantsRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
	})
	requireStatus(t, err, codes.NotFound)
}

// ---------------- GetGrantedCode --------------------------------------------

func TestGetGrantedCode_HappyPath_ReturnsValueAndModel(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("granted")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	userID := uuid.New()
	code := seedPoolCode(rig, pt, "STEAM-XYZ")
	code.State = repo.CodeStateGranted

	approvedAt := time.Now()
	rig.applicants.rows = append(rig.applicants.rows, &repo.Applicant{
		ID: uuid.New(), PlaytestID: pt.ID, UserID: userID,
		Status: applicantStatusApproved, GrantedCodeID: &code.ID,
		ApprovedAt: &approvedAt, CreatedAt: time.Now(),
	})

	resp, err := rig.svr.GetGrantedCode(authCtx(userID), &pb.GetGrantedCodeRequest{
		PlaytestId: pt.ID.String(),
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if resp.GetValue() != "STEAM-XYZ" {
		t.Errorf("value = %q, want STEAM-XYZ", resp.GetValue())
	}
	if resp.GetDistributionModel() != pb.DistributionModel_DISTRIBUTION_MODEL_STEAM_KEYS {
		t.Errorf("distribution_model = %s, want STEAM_KEYS", resp.GetDistributionModel())
	}
}

func TestGetGrantedCode_SoftDeletedPlaytest_NotFound(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("granted-deleted")
	now := time.Now()
	pt.DeletedAt = &now
	rig.playtests.rows = append(rig.playtests.rows, pt)

	_, err := rig.svr.GetGrantedCode(authCtx(uuid.New()), &pb.GetGrantedCodeRequest{
		PlaytestId: pt.ID.String(),
	})
	requireStatus(t, err, codes.NotFound)
}

func TestGetGrantedCode_NotApproved_NotFound(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("granted-pending")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	userID := uuid.New()
	rig.applicants.rows = append(rig.applicants.rows, &repo.Applicant{
		ID: uuid.New(), PlaytestID: pt.ID, UserID: userID,
		Status: applicantStatusPending, CreatedAt: time.Now(),
	})

	_, err := rig.svr.GetGrantedCode(authCtx(userID), &pb.GetGrantedCodeRequest{
		PlaytestId: pt.ID.String(),
	})
	requireStatus(t, err, codes.NotFound)
}

func TestGetGrantedCode_MissingActor_Unauthenticated(t *testing.T) {
	rig := withApproveStores(t)
	_, err := rig.svr.GetGrantedCode(context.Background(), &pb.GetGrantedCodeRequest{
		PlaytestId: uuid.New().String(),
	})
	requireStatus(t, err, codes.Unauthenticated)
}
