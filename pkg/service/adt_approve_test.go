package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	"github.com/anggorodewanto/playtesthub/pkg/adt"
	"github.com/anggorodewanto/playtesthub/pkg/dmqueue"
	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// fakeDMQueue captures enqueued jobs for assertion.
type fakeDMQueue struct {
	jobs []dmqueue.Job
	err  error
}

func (f *fakeDMQueue) Enqueue(_ context.Context, j dmqueue.Job) error {
	if f.err != nil {
		return f.err
	}
	f.jobs = append(f.jobs, j)
	return nil
}

// inlineTxRunner runs the supplied fn synchronously with q=nil — the
// fake repo doesn't need a live pg.Tx and the in-tx CAS / advisory lock
// branches accept nil per their `if q != nil` guards.
type inlineTxRunner struct{}

func (inlineTxRunner) InTx(ctx context.Context, fn func(repo.Querier) error) error {
	return fn(nil)
}

// adtApprovedHarness extends adtTestHarness with a wired tx runner +
// DM queue so the approve path runs end-to-end against in-memory fakes.
type adtApprovedHarness struct {
	*adtTestHarness

	dm *fakeDMQueue
}

func newADTApprovedHarness(t *testing.T) *adtApprovedHarness {
	t.Helper()
	h := newADTTestServer(t)
	dm := &fakeDMQueue{}
	h.svr.
		WithTxRunner(inlineTxRunner{}).
		WithDMQueue(dm)
	return &adtApprovedHarness{adtTestHarness: h, dm: dm}
}

func seedADTPlaytest(t *testing.T, h *adtApprovedHarness) *repo.Playtest {
	t.Helper()
	ns := testADTNamespace
	game := testADTGameID
	build := testADTBuildID
	pt := &repo.Playtest{
		ID:                uuid.New(),
		Namespace:         testNamespace,
		Slug:              "adt-approve",
		Title:             "ADT Beta",
		DistributionModel: distModelADT,
		Status:            statusOpen,
		ADTNamespace:      &ns,
		ADTGameID:         &game,
		ADTBuildID:        &build,
	}
	h.pt.rows = append(h.pt.rows, pt)
	return pt
}

func seedPendingApplicantADT(t *testing.T, svr *PlaytesthubServiceServer, ptID uuid.UUID) *repo.Applicant {
	t.Helper()
	// We can't easily call svr.applicant directly since it's unexported,
	// but the test server passes the *fakeApplicantStore through ap; we
	// access via Signup-like insertion through the fake by reaching the
	// embedded store. The signup test helpers do this via the bare fake.
	got, err := svr.applicant.Insert(context.Background(), &repo.Applicant{
		PlaytestID:    ptID,
		UserID:        uuid.New(),
		DiscordHandle: "tester#0001",
		Platforms:     []string{"STEAM"},
		Status:        applicantStatusPending,
	})
	if err != nil {
		t.Fatalf("Insert applicant: %v", err)
	}
	return got
}

func TestApproveApplicant_ADT_HappyPath_IssuesURLAndEnqueuesDM(t *testing.T) {
	h := newADTApprovedHarness(t)
	pt := seedADTPlaytest(t, h)
	a := seedPendingApplicantADT(t, h.svr, pt.ID)

	resp, err := h.svr.ApproveApplicant(authCtx(uuid.New()), &pb.ApproveApplicantRequest{
		Namespace:   testNamespace,
		ApplicantId: a.ID.String(),
	})
	if err != nil {
		t.Fatalf("ApproveApplicant: %v", err)
	}
	if got := resp.GetApplicant().GetStatus(); got != pb.ApplicantStatus_APPLICANT_STATUS_APPROVED {
		t.Errorf("status = %s, want APPROVED", got)
	}
	if resp.GetApplicant().GetGrantedCodeId() != "" {
		t.Errorf("granted_code_id = %q, want empty for ADT", resp.GetApplicant().GetGrantedCodeId())
	}
	if len(h.dm.jobs) != 1 {
		t.Fatalf("DM jobs = %d, want 1", len(h.dm.jobs))
	}
	if msg := h.dm.jobs[0].Message; !strings.Contains(msg, "Download your playtest build") {
		t.Errorf("DM body missing ADT prefix: %q", msg)
	}
	if got := h.mem.IssuedURLs(); len(got) != 1 {
		t.Errorf("MemClient IssuedURLs = %d, want 1", len(got))
	}
}

func TestApproveApplicant_ADT_FallbackURLWhenADTUnavailable(t *testing.T) {
	h := newADTApprovedHarness(t)
	// Drop the linkage flag → MemClient.IssueDownloadURL returns
	// ErrLinkageMissing → resolveADTDownloadURL surfaces FailedPrecondition
	// without consulting the fallback URL. To exercise the fallback we
	// need a non-linkage-missing error; swap in a client that returns a
	// generic error.
	h.svr.WithADTClient(&erroringADTClient{baseURL: "https://example.com/build.zip"})
	fallback := "https://example.com/build.zip"
	pt := seedADTPlaytest(t, h)
	pt.ADTFallbackDownloadURL = &fallback
	a := seedPendingApplicantADT(t, h.svr, pt.ID)

	resp, err := h.svr.ApproveApplicant(authCtx(uuid.New()), &pb.ApproveApplicantRequest{
		Namespace:   testNamespace,
		ApplicantId: a.ID.String(),
	})
	if err != nil {
		t.Fatalf("ApproveApplicant: %v", err)
	}
	if got := resp.GetApplicant().GetStatus(); got != pb.ApplicantStatus_APPLICANT_STATUS_APPROVED {
		t.Errorf("status = %s, want APPROVED", got)
	}
	if len(h.dm.jobs) != 1 {
		t.Fatalf("DM jobs = %d, want 1", len(h.dm.jobs))
	}
	if got := h.dm.jobs[0].Message; !strings.Contains(got, fallback) {
		t.Errorf("DM body missing fallback URL: %q", got)
	}
}

// erroringADTClient always fails with a generic (non-linkage-missing)
// error, exercising the fallback path. ListBuilds / ListGames are unused.
type erroringADTClient struct{ baseURL string }

func (erroringADTClient) ListBuilds(context.Context, string, string, string) ([]adt.Build, error) {
	return nil, nil
}
func (erroringADTClient) ListGames(context.Context, string, string) ([]adt.Game, error) {
	return nil, nil
}
func (erroringADTClient) IssueDownloadURL(context.Context, adt.IssueDownloadURLParams) (adt.IssuedDownloadURL, error) {
	return adt.IssuedDownloadURL{}, &adtTestError{}
}
func (erroringADTClient) DeleteLinkage(context.Context, string, string) error {
	return nil
}

type adtTestError struct{}

func (*adtTestError) Error() string { return "adt: 503 service unavailable" }

func TestGetGrantedCode_ADT_FailedPrecondition(t *testing.T) {
	h := newADTApprovedHarness(t)
	pt := seedADTPlaytest(t, h)
	_, err := h.svr.GetGrantedCode(authCtx(uuid.New()), &pb.GetGrantedCodeRequest{
		PlaytestId: pt.ID.String(),
	})
	requireStatus(t, err, codes.FailedPrecondition)
	requireMsgContains(t, err, "use GetADTDownloadInfo")
}

func TestGetADTDownloadInfo_HappyPath(t *testing.T) {
	h := newADTApprovedHarness(t)
	pt := seedADTPlaytest(t, h)
	userID := uuid.New()
	approvedAt := time.Now().UTC()
	h.pt.rows[0].Status = statusOpen
	if _, err := h.svr.applicant.Insert(context.Background(), &repo.Applicant{
		PlaytestID: pt.ID,
		UserID:     userID,
		Platforms:  []string{"STEAM"},
		Status:     applicantStatusApproved,
		ApprovedAt: &approvedAt,
	}); err != nil {
		t.Fatalf("seed approved applicant: %v", err)
	}

	resp, err := h.svr.GetADTDownloadInfo(authCtx(userID), &pb.GetADTDownloadInfoRequest{
		PlaytestId: pt.ID.String(),
	})
	if err != nil {
		t.Fatalf("GetADTDownloadInfo: %v", err)
	}
	if len(resp.GetUrls()) == 0 {
		t.Error("expected non-empty URL list")
	}
	if resp.GetSource() != adtURLSourceIssued {
		t.Errorf("source = %q, want %q", resp.GetSource(), adtURLSourceIssued)
	}
}

func TestGetADTDownloadInfo_NonADTPlaytest_FailedPrecondition(t *testing.T) {
	h := newADTApprovedHarness(t)
	pt := &repo.Playtest{
		ID:                uuid.New(),
		Namespace:         testNamespace,
		Slug:              "steam-pt",
		DistributionModel: distModelSteamKeys,
		Status:            statusOpen,
	}
	h.pt.rows = append(h.pt.rows, pt)
	_, err := h.svr.GetADTDownloadInfo(authCtx(uuid.New()), &pb.GetADTDownloadInfoRequest{
		PlaytestId: pt.ID.String(),
	})
	requireStatus(t, err, codes.FailedPrecondition)
	requireMsgContains(t, err, "use GetGrantedCode")
}
