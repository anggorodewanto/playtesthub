package service

import (
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

func TestCheckADTBuild_HealthyPersistsOK(t *testing.T) {
	h := newADTTestServer(t)
	row := seedADTPlaytestRow(h, "adt-check-ok")

	resp, err := h.svr.CheckADTBuild(authCtx(uuid.New()), &pb.CheckADTBuildRequest{
		Namespace:  testNamespace,
		PlaytestId: row.ID.String(),
	})
	if err != nil {
		t.Fatalf("CheckADTBuild: %v", err)
	}
	if !resp.GetHealthy() {
		t.Error("healthy = false, want true")
	}
	if got := resp.GetPlaytest().GetAdtBuildStatus(); got != adtBuildStatusOK {
		t.Errorf("adt_build_status = %q, want %q", got, adtBuildStatusOK)
	}
	if resp.GetPlaytest().GetAdtBuildCheckedAt() == nil {
		t.Error("adt_build_checked_at not set")
	}
	if row.ADTBuildStatus == nil || *row.ADTBuildStatus != adtBuildStatusOK {
		t.Errorf("persisted status = %v, want OK", row.ADTBuildStatus)
	}
}

func TestCheckADTBuild_BuildNotFoundPersistsUnavailable(t *testing.T) {
	h := newADTTestServer(t)
	h.svr.WithADTClient(buildNotFoundADTClient{})
	row := seedADTPlaytestRow(h, "adt-check-gone")

	resp, err := h.svr.CheckADTBuild(authCtx(uuid.New()), &pb.CheckADTBuildRequest{
		Namespace:  testNamespace,
		PlaytestId: row.ID.String(),
	})
	if err != nil {
		t.Fatalf("CheckADTBuild: %v (build-not-found is a determinate result, not an error)", err)
	}
	if resp.GetHealthy() {
		t.Error("healthy = true, want false")
	}
	if got := resp.GetPlaytest().GetAdtBuildStatus(); got != adtBuildStatusUnavailable {
		t.Errorf("adt_build_status = %q, want %q", got, adtBuildStatusUnavailable)
	}
	if row.ADTBuildStatus == nil || *row.ADTBuildStatus != adtBuildStatusUnavailable {
		t.Errorf("persisted status = %v, want UNAVAILABLE", row.ADTBuildStatus)
	}
}

func TestCheckADTBuild_NonADTPlaytest_FailedPrecondition(t *testing.T) {
	h := newADTTestServer(t)
	row := &repo.Playtest{
		ID:                uuid.New(),
		Namespace:         testNamespace,
		Slug:              "steam-check",
		Title:             "T",
		DistributionModel: distModelSteamKeys,
		Status:            statusDraft,
	}
	h.pt.rows = append(h.pt.rows, row)

	_, err := h.svr.CheckADTBuild(authCtx(uuid.New()), &pb.CheckADTBuildRequest{
		Namespace:  testNamespace,
		PlaytestId: row.ID.String(),
	})
	requireStatus(t, err, codes.FailedPrecondition)
	requireMsgContains(t, err, "ADT distribution")
}

func TestCheckADTBuild_PlaytestNotFound_NotFound(t *testing.T) {
	h := newADTTestServer(t)

	_, err := h.svr.CheckADTBuild(authCtx(uuid.New()), &pb.CheckADTBuildRequest{
		Namespace:  testNamespace,
		PlaytestId: uuid.New().String(),
	})
	requireStatus(t, err, codes.NotFound)
}

// A transient ADT failure (generic 5xx) surfaces as Unavailable and must
// NOT overwrite the stored status — last-known health survives a blip.
func TestCheckADTBuild_TransientError_DoesNotPersist(t *testing.T) {
	h := newADTTestServer(t)
	h.svr.WithADTClient(&erroringADTClient{})
	row := seedADTPlaytestRow(h, "adt-check-transient")

	_, err := h.svr.CheckADTBuild(authCtx(uuid.New()), &pb.CheckADTBuildRequest{
		Namespace:  testNamespace,
		PlaytestId: row.ID.String(),
	})
	requireStatus(t, err, codes.Unavailable)
	if row.ADTBuildStatus != nil {
		t.Errorf("status = %v, want nil (transient error must not persist)", row.ADTBuildStatus)
	}
}

// Approve-time persistence (path 3): a build-not-found approve fails AND
// records UNAVAILABLE so the detail page reflects it without a manual check.
func TestApproveApplicant_ADT_BuildNotFound_PersistsUnavailable(t *testing.T) {
	h := newADTApprovedHarness(t)
	h.svr.WithADTClient(buildNotFoundADTClient{})
	pt := seedADTPlaytest(t, h)
	a := seedPendingApplicantADT(t, h.svr, pt.ID)

	_, err := h.svr.ApproveApplicant(authCtx(uuid.New()), &pb.ApproveApplicantRequest{
		Namespace:   testNamespace,
		ApplicantId: a.ID.String(),
	})
	requireStatus(t, err, codes.FailedPrecondition)
	requireMsgContains(t, err, "ADT build no longer exists")
	if pt.ADTBuildStatus == nil || *pt.ADTBuildStatus != adtBuildStatusUnavailable {
		t.Errorf("persisted status = %v, want UNAVAILABLE", pt.ADTBuildStatus)
	}
}

func TestApproveApplicant_ADT_HappyPath_PersistsOK(t *testing.T) {
	h := newADTApprovedHarness(t)
	pt := seedADTPlaytest(t, h)
	a := seedPendingApplicantADT(t, h.svr, pt.ID)

	if _, err := h.svr.ApproveApplicant(authCtx(uuid.New()), &pb.ApproveApplicantRequest{
		Namespace:   testNamespace,
		ApplicantId: a.ID.String(),
	}); err != nil {
		t.Fatalf("ApproveApplicant: %v", err)
	}
	if pt.ADTBuildStatus == nil || *pt.ADTBuildStatus != adtBuildStatusOK {
		t.Errorf("persisted status = %v, want OK", pt.ADTBuildStatus)
	}
}
