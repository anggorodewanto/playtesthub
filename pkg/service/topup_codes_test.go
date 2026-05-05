package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	"github.com/anggorodewanto/playtesthub/pkg/ags"
	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// agsCampaignPlaytest seeds an OPEN AGS_CAMPAIGN playtest with both AGS
// ids populated — the precondition every TopUpCodes / SyncFromAGS call
// asserts. The agsClient is updated in lockstep so its CreateItem /
// CreateCampaign calls succeed for the same id, exercising downstream
// AGS round-trips against a self-consistent fixture.
func agsCampaignPlaytest(slug string, mem *ags.MemClient) *repo.Playtest {
	pt := openPlaytest(slug)
	pt.DistributionModel = distModelAGSCampaign
	ctx := context.Background()
	campaignID, _ := mem.CreateCampaign(ctx, ags.CampaignSpec{Name: pt.Title})
	itemID, _ := mem.CreateItem(ctx, ags.ItemSpec{Name: pt.Title})
	_ = mem.LinkItemToCampaign(ctx, campaignID, itemID, pt.Title)
	pt.AGSItemID = &itemID
	pt.AGSCampaignID = &campaignID
	return pt
}

// ---------------- TopUpCodes ------------------------------------------------

func TestTopUpCodes_HappyPath_GeneratesAndAuditsAndReturnsStats(t *testing.T) {
	rig := withAGSStores(t)
	pt := agsCampaignPlaytest("topup-happy", rig.agsClient)
	rig.playtests.rows = append(rig.playtests.rows, pt)

	resp, err := rig.svr.TopUpCodes(authCtx(uuid.New()), &pb.TopUpCodesRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		Quantity:   3,
	})
	if err != nil {
		t.Fatalf("TopUpCodes: %v", err)
	}
	if resp.GetAdded() != 3 {
		t.Errorf("added = %d, want 3", resp.GetAdded())
	}
	if resp.GetPool().GetTotal() != 3 || resp.GetPool().GetUnused() != 3 {
		t.Errorf("stats = %+v, want total=3 unused=3", resp.GetPool())
	}
	if got := rig.audit.countAction(repo.ActionCampaignGenerateCodes); got != 1 {
		t.Errorf("campaign.generate_codes count = %d, want 1", got)
	}
	if got := rig.audit.countAction(repo.ActionCampaignGenerateCodesFailed); got != 0 {
		t.Errorf("campaign.generate_codes_failed count = %d, want 0 on success", got)
	}
}

// PRD §4.6 batch size = agsCodeBatchSize. The handler walks
// `quantity / batchSize` AGS round-trips, each persisted under its own
// short tx. quantity=5 with batchSize=2 → 2+2+1.
func TestTopUpCodes_BatchedGeneration(t *testing.T) {
	rig := withAGSStores(t)
	rig.svr = rig.svr.WithAGSCodeBatchSize(2)
	pt := agsCampaignPlaytest("topup-batch", rig.agsClient)
	rig.playtests.rows = append(rig.playtests.rows, pt)

	resp, err := rig.svr.TopUpCodes(authCtx(uuid.New()), &pb.TopUpCodesRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		Quantity:   5,
	})
	if err != nil {
		t.Fatalf("TopUpCodes: %v", err)
	}
	if resp.GetAdded() != 5 {
		t.Errorf("added = %d, want 5 (2+2+1)", resp.GetAdded())
	}
	if rig.code.insertGeneratedCalls != 3 {
		t.Errorf("InsertGeneratedAtomic call count = %d, want 3 (one per AGS batch)", rig.code.insertGeneratedCalls)
	}
}

// PRD §4.6 partial fulfillment commits whatever AGS returned and warns.
// AGS returning fewer codes than asked is a successful-but-partial
// outcome; gRPC code is OK and `added` reflects the truth.
func TestTopUpCodes_PartialFulfillment_CommitsAndWarns(t *testing.T) {
	rig := withAGSStores(t)
	pt := agsCampaignPlaytest("topup-partial", rig.agsClient)
	rig.playtests.rows = append(rig.playtests.rows, pt)
	rig.svr = rig.svr.WithAGSClient(&partialFulfillClient{cap: 2, mem: rig.agsClient})

	resp, err := rig.svr.TopUpCodes(authCtx(uuid.New()), &pb.TopUpCodesRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		Quantity:   5,
	})
	if err != nil {
		t.Fatalf("TopUpCodes (partial fulfillment must commit): %v", err)
	}
	if resp.GetAdded() != 2 {
		t.Errorf("added = %d, want 2 (capped)", resp.GetAdded())
	}
	if got := rig.audit.countAction(repo.ActionCampaignGenerateCodes); got != 1 {
		t.Errorf("campaign.generate_codes count = %d, want 1", got)
	}
}

// PRD §4.6 line 99: "no DB tx — each batch inserted independently".
// A mid-loop AGS failure keeps prior batches and surfaces as Unavailable.
func TestTopUpCodes_AGSFailureMidLoop_KeepsPriorBatches(t *testing.T) {
	rig := withAGSStores(t)
	rig.svr = rig.svr.WithAGSCodeBatchSize(2)
	pt := agsCampaignPlaytest("topup-midfail", rig.agsClient)
	rig.playtests.rows = append(rig.playtests.rows, pt)
	// First call succeeds (batch 1 persists), second is 5xx through every
	// retry attempt (4 attempts per the standard policy).
	rig.agsClient.CreateCodesErr = []error{
		nil,
		stubHTTPErr{500, "boom"}, stubHTTPErr{500, "boom"}, stubHTTPErr{500, "boom"}, stubHTTPErr{500, "boom"},
	}

	_, err := rig.svr.TopUpCodes(authCtx(uuid.New()), &pb.TopUpCodesRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		Quantity:   5,
	})
	requireStatus(t, err, codes.Unavailable)
	rows, _ := rig.code.ListByPlaytest(context.Background(), pt.ID)
	if len(rows) != 2 {
		t.Errorf("persisted rows = %d, want 2 (batch 1 survived mid-loop failure)", len(rows))
	}
	if got := rig.audit.countAction(repo.ActionCampaignGenerateCodesFailed); got != 1 {
		t.Errorf("campaign.generate_codes_failed count = %d, want 1", got)
	}
}

// errors.md row 23: HTTP 429 → ResourceExhausted.
func TestTopUpCodes_AGS429_MapsToResourceExhausted(t *testing.T) {
	rig := withAGSStores(t)
	pt := agsCampaignPlaytest("topup-429", rig.agsClient)
	rig.playtests.rows = append(rig.playtests.rows, pt)
	rig.agsClient.CreateCodesErr = []error{stubHTTPErr{429, "rate limited"}}

	_, err := rig.svr.TopUpCodes(authCtx(uuid.New()), &pb.TopUpCodesRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		Quantity:   5,
	})
	requireStatus(t, err, codes.ResourceExhausted)
}

func TestTopUpCodes_RejectsSteamKeysPlaytest(t *testing.T) {
	rig := withAGSStores(t)
	pt := steamKeysPlaytest("topup-wrong-model")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	_, err := rig.svr.TopUpCodes(authCtx(uuid.New()), &pb.TopUpCodesRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		Quantity:   1,
	})
	requireStatus(t, err, codes.FailedPrecondition)
}

func TestTopUpCodes_SoftDeletedNotFound(t *testing.T) {
	rig := withAGSStores(t)
	pt := agsCampaignPlaytest("topup-deleted", rig.agsClient)
	now := time.Now()
	pt.DeletedAt = &now
	rig.playtests.rows = append(rig.playtests.rows, pt)

	_, err := rig.svr.TopUpCodes(authCtx(uuid.New()), &pb.TopUpCodesRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		Quantity:   1,
	})
	requireStatus(t, err, codes.NotFound)
}

func TestTopUpCodes_QtyOutOfBounds_InvalidArgument(t *testing.T) {
	rig := withAGSStores(t)
	pt := agsCampaignPlaytest("topup-qty", rig.agsClient)
	rig.playtests.rows = append(rig.playtests.rows, pt)

	for _, q := range []int32{0, -1, 50_001} {
		_, err := rig.svr.TopUpCodes(authCtx(uuid.New()), &pb.TopUpCodesRequest{
			Namespace:  testNamespace,
			PlaytestId: pt.ID.String(),
			Quantity:   q,
		})
		requireStatus(t, err, codes.InvalidArgument)
	}
}

func TestTopUpCodes_RequiresActor(t *testing.T) {
	rig := withAGSStores(t)
	_, err := rig.svr.TopUpCodes(context.Background(), &pb.TopUpCodesRequest{
		Namespace:  testNamespace,
		PlaytestId: uuid.New().String(),
		Quantity:   1,
	})
	requireStatus(t, err, codes.Unauthenticated)
}

func TestTopUpCodes_NamespaceMismatchPermissionDenied(t *testing.T) {
	rig := withAGSStores(t)
	_, err := rig.svr.TopUpCodes(authCtx(uuid.New()), &pb.TopUpCodesRequest{
		Namespace:  "wrong",
		PlaytestId: uuid.New().String(),
		Quantity:   1,
	})
	requireStatus(t, err, codes.PermissionDenied)
}

func TestTopUpCodes_AGSClientNotWiredInternal(t *testing.T) {
	rig := withAGSStores(t)
	rig.svr.agsClient = nil
	_, err := rig.svr.TopUpCodes(authCtx(uuid.New()), &pb.TopUpCodesRequest{
		Namespace:  testNamespace,
		PlaytestId: uuid.New().String(),
		Quantity:   1,
	})
	requireStatus(t, err, codes.Internal)
}

// ---------------- SyncFromAGS -----------------------------------------------

func TestSyncFromAGS_HappyPath_AddsMissingCodesAndAudits(t *testing.T) {
	rig := withAGSStores(t)
	pt := agsCampaignPlaytest("sync-happy", rig.agsClient)
	rig.playtests.rows = append(rig.playtests.rows, pt)
	// Seed 3 AGS-side codes; DB has none.
	if _, err := rig.agsClient.CreateCodes(context.Background(), *pt.AGSCampaignID, 3); err != nil {
		t.Fatalf("seed AGS: %v", err)
	}

	resp, err := rig.svr.SyncFromAGS(authCtx(uuid.New()), &pb.SyncFromAGSRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
	})
	if err != nil {
		t.Fatalf("SyncFromAGS: %v", err)
	}
	if resp.GetAdded() != 3 {
		t.Errorf("added = %d, want 3", resp.GetAdded())
	}
	if resp.GetPool().GetTotal() != 3 {
		t.Errorf("pool total = %d, want 3", resp.GetPool().GetTotal())
	}
	if got := rig.audit.countAction(repo.ActionCampaignGenerateCodes); got != 1 {
		t.Errorf("campaign.generate_codes count = %d, want 1", got)
	}
}

// PRD §4.6 step 5: idempotent on UNIQUE(playtest_id, value). Re-running
// after a complete sync inserts zero rows and emits no audit row.
func TestSyncFromAGS_Idempotent_ReRunInsertsZero(t *testing.T) {
	rig := withAGSStores(t)
	pt := agsCampaignPlaytest("sync-idem", rig.agsClient)
	rig.playtests.rows = append(rig.playtests.rows, pt)
	if _, err := rig.agsClient.CreateCodes(context.Background(), *pt.AGSCampaignID, 3); err != nil {
		t.Fatalf("seed AGS: %v", err)
	}
	first, err := rig.svr.SyncFromAGS(authCtx(uuid.New()), &pb.SyncFromAGSRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
	})
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if first.GetAdded() != 3 {
		t.Fatalf("first sync added = %d, want 3", first.GetAdded())
	}

	second, err := rig.svr.SyncFromAGS(authCtx(uuid.New()), &pb.SyncFromAGSRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
	})
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if second.GetAdded() != 0 {
		t.Errorf("second sync added = %d, want 0 (idempotent)", second.GetAdded())
	}
	// Only one audit row total — the no-op second sync must not emit.
	if got := rig.audit.countAction(repo.ActionCampaignGenerateCodes); got != 1 {
		t.Errorf("campaign.generate_codes count = %d, want 1 (second call is a no-op)", got)
	}
}

// AGS-side has more codes than the DB → the gap fills, existing rows
// stay put. Exercises the partial-recovery use case from PRD §4.6
// step 5: a rolled-back CreatePlaytest left codes orphaned on AGS;
// SyncFromAGS reattaches the gap.
func TestSyncFromAGS_PartialOverlap_AddsOnlyMissing(t *testing.T) {
	rig := withAGSStores(t)
	pt := agsCampaignPlaytest("sync-partial", rig.agsClient)
	rig.playtests.rows = append(rig.playtests.rows, pt)
	if _, err := rig.agsClient.CreateCodes(context.Background(), *pt.AGSCampaignID, 5); err != nil {
		t.Fatalf("seed AGS: %v", err)
	}
	// Pre-seed 2 of the 5 already-known codes into the DB.
	known, err := rig.agsClient.FetchCodes(context.Background(), *pt.AGSCampaignID)
	if err != nil {
		t.Fatalf("fetch ags: %v", err)
	}
	if _, err := rig.code.BulkInsert(context.Background(), pt.ID, known[:2]); err != nil {
		t.Fatalf("seed db: %v", err)
	}

	resp, err := rig.svr.SyncFromAGS(authCtx(uuid.New()), &pb.SyncFromAGSRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
	})
	if err != nil {
		t.Fatalf("SyncFromAGS: %v", err)
	}
	if resp.GetAdded() != 3 {
		t.Errorf("added = %d, want 3 (5 AGS - 2 already-known)", resp.GetAdded())
	}
	if resp.GetPool().GetTotal() != 5 {
		t.Errorf("pool total = %d, want 5", resp.GetPool().GetTotal())
	}
}

func TestSyncFromAGS_AGSFailure_AuditsAndMapsError(t *testing.T) {
	rig := withAGSStores(t)
	pt := agsCampaignPlaytest("sync-fail", rig.agsClient)
	rig.playtests.rows = append(rig.playtests.rows, pt)
	rig.agsClient.FetchCodesErr = []error{
		stubHTTPErr{500, "boom"}, stubHTTPErr{500, "boom"}, stubHTTPErr{500, "boom"}, stubHTTPErr{500, "boom"},
	}

	_, err := rig.svr.SyncFromAGS(authCtx(uuid.New()), &pb.SyncFromAGSRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
	})
	requireStatus(t, err, codes.Unavailable)
	if got := rig.audit.countAction(repo.ActionCampaignGenerateCodesFailed); got != 1 {
		t.Errorf("campaign.generate_codes_failed count = %d, want 1", got)
	}
}

func TestSyncFromAGS_RejectsSteamKeysPlaytest(t *testing.T) {
	rig := withAGSStores(t)
	pt := steamKeysPlaytest("sync-wrong-model")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	_, err := rig.svr.SyncFromAGS(authCtx(uuid.New()), &pb.SyncFromAGSRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
	})
	requireStatus(t, err, codes.FailedPrecondition)
}

func TestSyncFromAGS_SoftDeletedNotFound(t *testing.T) {
	rig := withAGSStores(t)
	pt := agsCampaignPlaytest("sync-deleted", rig.agsClient)
	now := time.Now()
	pt.DeletedAt = &now
	rig.playtests.rows = append(rig.playtests.rows, pt)

	_, err := rig.svr.SyncFromAGS(authCtx(uuid.New()), &pb.SyncFromAGSRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
	})
	requireStatus(t, err, codes.NotFound)
}

func TestSyncFromAGS_MissingCampaignID_FailedPrecondition(t *testing.T) {
	rig := withAGSStores(t)
	pt := agsCampaignPlaytest("sync-no-campaign", rig.agsClient)
	pt.AGSCampaignID = nil
	rig.playtests.rows = append(rig.playtests.rows, pt)

	_, err := rig.svr.SyncFromAGS(authCtx(uuid.New()), &pb.SyncFromAGSRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
	})
	requireStatus(t, err, codes.FailedPrecondition)
}

func TestSyncFromAGS_RequiresActor(t *testing.T) {
	rig := withAGSStores(t)
	_, err := rig.svr.SyncFromAGS(context.Background(), &pb.SyncFromAGSRequest{
		Namespace:  testNamespace,
		PlaytestId: uuid.New().String(),
	})
	requireStatus(t, err, codes.Unauthenticated)
}
