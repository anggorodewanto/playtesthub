package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	"github.com/anggorodewanto/playtesthub/pkg/dmqueue"
	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// fakeDMEnqueuer records every Enqueue call so tests can assert the
// service handed the right Job to the queue.
type fakeDMEnqueuer struct {
	mu   sync.Mutex
	jobs []dmqueue.Job
	err  error
}

func (f *fakeDMEnqueuer) Enqueue(_ context.Context, j dmqueue.Job) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.jobs = append(f.jobs, j)
	return nil
}

func (f *fakeDMEnqueuer) snapshot() []dmqueue.Job {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]dmqueue.Job, len(f.jobs))
	copy(out, f.jobs)
	return out
}

func seedFailedDMApplicant(rig approveTestRig, pt *repo.Playtest, userID uuid.UUID) *repo.Applicant {
	failed := dmStatusFailed
	reason := "discord: 500"
	now := time.Now()
	a := &repo.Applicant{
		ID:              uuid.New(),
		PlaytestID:      pt.ID,
		UserID:          userID,
		DiscordHandle:   "discord:snowflake",
		Platforms:       []string{"STEAM"},
		Status:          applicantStatusApproved,
		ApprovedAt:      &now,
		LastDMStatus:    &failed,
		LastDMAttemptAt: &now,
		LastDMError:     &reason,
		CreatedAt:       now,
	}
	rig.applicants.rows = append(rig.applicants.rows, a)
	return a
}

// TestRetryDM_HappyPath_EnqueuesManualJob covers the documented gate
// (status=APPROVED + last_dm_status=failed) and asserts the queue
// receives a manual=true job carrying the applicant's discord handle
// + playtest title.
func TestRetryDM_HappyPath_EnqueuesManualJob(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("retry-dm-happy")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	a := seedFailedDMApplicant(rig, pt, uuid.New())
	dm := &fakeDMEnqueuer{}
	rig.svr = rig.svr.WithDMQueue(dm)

	resp, err := rig.svr.RetryDM(authCtx(uuid.New()), &pb.RetryDMRequest{
		Namespace:   testNamespace,
		ApplicantId: a.ID.String(),
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if resp.GetApplicant().GetId() != a.ID.String() {
		t.Fatalf("response applicant id mismatch: %s vs %s", resp.GetApplicant().GetId(), a.ID)
	}

	jobs := dm.snapshot()
	if len(jobs) != 1 {
		t.Fatalf("want 1 enqueued job, got %d", len(jobs))
	}
	j := jobs[0]
	if j.ApplicantID != a.ID || j.PlaytestID != pt.ID || j.UserID != a.UserID {
		t.Fatalf("job ids mismatch: %+v", j)
	}
	if !j.Manual {
		t.Fatalf("manual=false; RetryDM must mark Manual=true")
	}
	if j.DiscordUserID != a.DiscordHandle {
		t.Fatalf("recipient = %q, want applicant.discord_handle %q", j.DiscordUserID, a.DiscordHandle)
	}
}

// TestRetryDM_DoubleClick_EnqueuesTwice asserts the no-cooldown rule
// from PRD §5.4: two back-to-back RetryDM calls, both seeing
// last_dm_status=failed, enqueue two jobs.
func TestRetryDM_DoubleClick_EnqueuesTwice(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("retry-dm-double")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	a := seedFailedDMApplicant(rig, pt, uuid.New())
	dm := &fakeDMEnqueuer{}
	rig.svr = rig.svr.WithDMQueue(dm)
	ctx := authCtx(uuid.New())
	req := &pb.RetryDMRequest{Namespace: testNamespace, ApplicantId: a.ID.String()}

	if _, err := rig.svr.RetryDM(ctx, req); err != nil {
		t.Fatalf("first RetryDM: %v", err)
	}
	if _, err := rig.svr.RetryDM(ctx, req); err != nil {
		t.Fatalf("second RetryDM: %v", err)
	}
	if got := len(dm.snapshot()); got != 2 {
		t.Fatalf("double-click should enqueue twice, got %d jobs", got)
	}
}

// TestRetryDM_PendingApplicant_FailedPrecondition: only APPROVED
// applicants are eligible (dm-queue.md gate).
func TestRetryDM_PendingApplicant_FailedPrecondition(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("retry-dm-pending")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	a := seedPendingApplicant(rig, pt, uuid.New())
	rig.svr = rig.svr.WithDMQueue(&fakeDMEnqueuer{})

	_, err := rig.svr.RetryDM(authCtx(uuid.New()), &pb.RetryDMRequest{
		Namespace:   testNamespace,
		ApplicantId: a.ID.String(),
	})
	requireStatus(t, err, codes.FailedPrecondition)
}

// TestRetryDM_DMStatusSent_FailedPrecondition: an applicant whose
// last DM is in the sent state is outside the gate (admin would have
// to wait for a failure to retry — there is no path to "re-send a
// successful DM" from the UI per PRD §5.4 + dm-queue.md).
func TestRetryDM_DMStatusSent_FailedPrecondition(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("retry-dm-sent")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	sent := dmStatusSent
	now := time.Now()
	a := &repo.Applicant{
		ID:              uuid.New(),
		PlaytestID:      pt.ID,
		UserID:          uuid.New(),
		DiscordHandle:   "Player",
		Platforms:       []string{"STEAM"},
		Status:          applicantStatusApproved,
		ApprovedAt:      &now,
		LastDMStatus:    &sent,
		LastDMAttemptAt: &now,
		CreatedAt:       now,
	}
	rig.applicants.rows = append(rig.applicants.rows, a)
	rig.svr = rig.svr.WithDMQueue(&fakeDMEnqueuer{})

	_, err := rig.svr.RetryDM(authCtx(uuid.New()), &pb.RetryDMRequest{
		Namespace:   testNamespace,
		ApplicantId: a.ID.String(),
	})
	requireStatus(t, err, codes.FailedPrecondition)
}

// TestRetryDM_Unauthenticated covers the auth gate + the "queue not
// wired" wiring-regression path.
func TestRetryDM_Unauthenticated(t *testing.T) {
	rig := withApproveStores(t)
	rig.svr = rig.svr.WithDMQueue(&fakeDMEnqueuer{})
	_, err := rig.svr.RetryDM(context.Background(), &pb.RetryDMRequest{
		Namespace:   testNamespace,
		ApplicantId: uuid.New().String(),
	})
	requireStatus(t, err, codes.Unauthenticated)
}

func TestRetryDM_NotWired_Internal(t *testing.T) {
	rig := withApproveStores(t)
	a := uuid.New()
	_, err := rig.svr.RetryDM(authCtx(uuid.New()), &pb.RetryDMRequest{
		Namespace:   testNamespace,
		ApplicantId: a.String(),
	})
	requireStatus(t, err, codes.Internal)
}

// TestApproveApplicant_EnqueuesAutoSendDM verifies the M2 phase 7
// integration: a successful approve enqueues a non-manual DM. The
// approve audit row is independent of the DM enqueue.
func TestApproveApplicant_EnqueuesAutoSendDM(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("approve-enqueues-dm")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	a := seedPendingApplicant(rig, pt, uuid.New())
	seedPoolCode(rig, pt, "STEAM-KEY-DM")
	dm := &fakeDMEnqueuer{}
	rig.svr = rig.svr.WithDMQueue(dm)

	if _, err := rig.svr.ApproveApplicant(authCtx(uuid.New()), &pb.ApproveApplicantRequest{
		Namespace:   testNamespace,
		ApplicantId: a.ID.String(),
	}); err != nil {
		t.Fatalf("ApproveApplicant: %v", err)
	}

	jobs := dm.snapshot()
	if len(jobs) != 1 {
		t.Fatalf("want 1 enqueued auto-send DM, got %d", len(jobs))
	}
	if jobs[0].Manual {
		t.Fatalf("auto-send must enqueue Manual=false")
	}
}
