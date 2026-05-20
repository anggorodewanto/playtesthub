package service

import (
	"context"
	"strings"
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
	snowflake := "discord:snowflake"
	a := &repo.Applicant{
		ID:              uuid.New(),
		PlaytestID:      pt.ID,
		UserID:          userID,
		DiscordHandle:   "DisplayName",
		DiscordUserID:   &snowflake,
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
	if a.DiscordUserID == nil || j.DiscordUserID != *a.DiscordUserID {
		t.Fatalf("recipient = %q, want applicant.discord_user_id %v", j.DiscordUserID, a.DiscordUserID)
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

// TestRetryFailedDms_EnqueuesEveryFailedApplicant covers the bulk
// happy path: every APPROVED applicant whose last_dm_status='failed'
// is enqueued as a manual job; non-failed applicants are untouched.
func TestRetryFailedDms_EnqueuesEveryFailedApplicant(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("retry-failed-bulk-happy")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	a1 := seedFailedDMApplicant(rig, pt, uuid.New())
	a2 := seedFailedDMApplicant(rig, pt, uuid.New())
	// Untouched: not failed.
	seedPendingApplicant(rig, pt, uuid.New())
	dm := &fakeDMEnqueuer{}
	rig.svr = rig.svr.WithDMQueue(dm)

	resp, err := rig.svr.RetryFailedDms(authCtx(uuid.New()), &pb.RetryFailedDmsRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
	})
	if err != nil {
		t.Fatalf("RetryFailedDms: %v", err)
	}
	if resp.GetEnqueued() != 2 || resp.GetOverflow() != 0 {
		t.Fatalf("counts: enqueued=%d overflow=%d, want 2/0", resp.GetEnqueued(), resp.GetOverflow())
	}

	jobs := dm.snapshot()
	if len(jobs) != 2 {
		t.Fatalf("want 2 enqueued jobs, got %d", len(jobs))
	}
	got := map[uuid.UUID]bool{}
	for _, j := range jobs {
		if !j.Manual {
			t.Errorf("job %v: Manual=false; bulk retry must mark Manual=true", j.ApplicantID)
		}
		got[j.ApplicantID] = true
	}
	if !got[a1.ID] || !got[a2.ID] {
		t.Errorf("missing applicants in jobs: got=%v want=%v,%v", got, a1.ID, a2.ID)
	}
}

// TestRetryFailedDms_OverflowSurfacesInResponse: when Enqueue returns
// ErrQueueFull (already-marked-failed-and-audited inside the queue),
// the count is reflected in `overflow` and the call still succeeds for
// the remaining applicants.
func TestRetryFailedDms_OverflowSurfacesInResponse(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("retry-failed-overflow")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	seedFailedDMApplicant(rig, pt, uuid.New())
	seedFailedDMApplicant(rig, pt, uuid.New())
	dm := &fakeDMEnqueuer{err: dmqueue.ErrQueueFull}
	rig.svr = rig.svr.WithDMQueue(dm)

	resp, err := rig.svr.RetryFailedDms(authCtx(uuid.New()), &pb.RetryFailedDmsRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
	})
	if err != nil {
		t.Fatalf("RetryFailedDms: %v", err)
	}
	if resp.GetEnqueued() != 0 || resp.GetOverflow() != 2 {
		t.Fatalf("counts: enqueued=%d overflow=%d, want 0/2", resp.GetEnqueued(), resp.GetOverflow())
	}
}

// TestRetryFailedDms_NoFailedApplicants_ZeroResponse: a clean playtest
// with no failed applicants returns 0/0 and does not error.
func TestRetryFailedDms_NoFailedApplicants_ZeroResponse(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("retry-failed-empty")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	seedPendingApplicant(rig, pt, uuid.New())
	dm := &fakeDMEnqueuer{}
	rig.svr = rig.svr.WithDMQueue(dm)

	resp, err := rig.svr.RetryFailedDms(authCtx(uuid.New()), &pb.RetryFailedDmsRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
	})
	if err != nil {
		t.Fatalf("RetryFailedDms: %v", err)
	}
	if resp.GetEnqueued() != 0 || resp.GetOverflow() != 0 {
		t.Fatalf("counts: enqueued=%d overflow=%d, want 0/0", resp.GetEnqueued(), resp.GetOverflow())
	}
	if got := len(dm.snapshot()); got != 0 {
		t.Errorf("Enqueue called %d times on empty cohort", got)
	}
}

// TestRetryFailedDms_PlaytestNotFound covers the missing/soft-deleted
// playtest path: NotFound (mirrors RetryDM).
func TestRetryFailedDms_PlaytestNotFound(t *testing.T) {
	rig := withApproveStores(t)
	rig.svr = rig.svr.WithDMQueue(&fakeDMEnqueuer{})
	_, err := rig.svr.RetryFailedDms(authCtx(uuid.New()), &pb.RetryFailedDmsRequest{
		Namespace:  testNamespace,
		PlaytestId: uuid.New().String(),
	})
	requireStatus(t, err, codes.NotFound)
}

// TestRetryFailedDms_SoftDeletedPlaytest_NotFound: a soft-deleted
// playtest hides as NotFound (mirrors PRD §5.1 visibility).
func TestRetryFailedDms_SoftDeletedPlaytest_NotFound(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("retry-failed-deleted")
	now := time.Now()
	pt.DeletedAt = &now
	rig.playtests.rows = append(rig.playtests.rows, pt)
	rig.svr = rig.svr.WithDMQueue(&fakeDMEnqueuer{})

	_, err := rig.svr.RetryFailedDms(authCtx(uuid.New()), &pb.RetryFailedDmsRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
	})
	requireStatus(t, err, codes.NotFound)
}

// TestRetryFailedDms_BadPlaytestID_InvalidArgument: a non-UUID
// playtest_id maps to InvalidArgument.
func TestRetryFailedDms_BadPlaytestID_InvalidArgument(t *testing.T) {
	rig := withApproveStores(t)
	rig.svr = rig.svr.WithDMQueue(&fakeDMEnqueuer{})
	_, err := rig.svr.RetryFailedDms(authCtx(uuid.New()), &pb.RetryFailedDmsRequest{
		Namespace:  testNamespace,
		PlaytestId: "not-a-uuid",
	})
	requireStatus(t, err, codes.InvalidArgument)
}

// TestRetryFailedDms_Unauth_Unauthenticated: no actor in context →
// Unauthenticated (mirrors every admin RPC).
func TestRetryFailedDms_Unauth_Unauthenticated(t *testing.T) {
	rig := withApproveStores(t)
	rig.svr = rig.svr.WithDMQueue(&fakeDMEnqueuer{})
	_, err := rig.svr.RetryFailedDms(context.Background(), &pb.RetryFailedDmsRequest{
		Namespace:  testNamespace,
		PlaytestId: uuid.New().String(),
	})
	requireStatus(t, err, codes.Unauthenticated)
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

// TestBuildApprovalDMBody_NoBaseURL_FallsBackToLegacyCopy locks in the
// legacy non-clickable wording when PLAYER_BASE_URL is unset, so forks
// running without a public player origin keep the existing behaviour.
func TestBuildApprovalDMBody_NoBaseURL_FallsBackToLegacyCopy(t *testing.T) {
	pt := &repo.Playtest{Title: "Acme Closed Beta", Slug: "acme-beta"}
	got := buildApprovalDMBody(pt, "", "")
	want := `You're approved for "Acme Closed Beta". Open the playtest to view your code.`
	if got != want {
		t.Fatalf("legacy DM body mismatch:\n  got:  %q\n  want: %q", got, want)
	}
}

// TestBuildApprovalDMBody_WithBaseURL_EmbedsHashRouterDeepLink is the
// guard that the deep link points at the hash-router pending route and
// uses the configured player origin verbatim. Discord renders bare URLs
// as tappable links so no markdown wrapping is needed.
func TestBuildApprovalDMBody_WithBaseURL_EmbedsHashRouterDeepLink(t *testing.T) {
	pt := &repo.Playtest{Title: "Acme Closed Beta", Slug: "acme-beta"}
	got := buildApprovalDMBody(pt, "https://anggorodewanto.github.io/playtesthub", "")
	want := `You're approved for "Acme Closed Beta". View your code: https://anggorodewanto.github.io/playtesthub/#/playtest/acme-beta/pending`
	if got != want {
		t.Fatalf("deep-link DM body mismatch:\n  got:  %q\n  want: %q", got, want)
	}
}

// TestApproveApplicant_DMBodyIncludesDeepLinkWhenConfigured wires
// WithPlayerBaseURL end-to-end through ApproveApplicant and asserts the
// enqueued job's Message carries the deep link, proving the bootapp
// path that production uses.
func TestApproveApplicant_DMBodyIncludesDeepLinkWhenConfigured(t *testing.T) {
	rig := withApproveStores(t)
	pt := steamKeysPlaytest("approve-deep-link")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	a := seedPendingApplicant(rig, pt, uuid.New())
	seedPoolCode(rig, pt, "STEAM-KEY-DEEP")
	dm := &fakeDMEnqueuer{}
	rig.svr = rig.svr.
		WithDMQueue(dm).
		WithPlayerBaseURL("https://example.test/playtesthub")

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
	wantSuffix := "https://example.test/playtesthub/#/playtest/" + pt.Slug + "/pending"
	if !strings.Contains(jobs[0].Message, wantSuffix) {
		t.Fatalf("DM body missing deep link suffix %q\n  got: %q", wantSuffix, jobs[0].Message)
	}
}
