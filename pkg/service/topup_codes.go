package service

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/anggorodewanto/playtesthub/pkg/ags"
	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// TopUpCodes generates additional AGS_CAMPAIGN codes for an existing
// playtest. PRD §4.6 step 4:
//   - generate-only (CreateCodes against the playtest's `agsCampaignId`);
//   - not idempotent — each call burns a fresh AGS-side batch;
//   - no DB tx wrapper — each AGS batch is inserted under its own short
//     tx that takes pg_advisory_xact_lock(hashtext(playtestId::text)),
//     same discipline as UploadCodes (§4.3). A failure mid-loop keeps
//     prior batches.
//   - retry policy: standard 30s/3-retries (docs/ags-failure-modes.md
//     §"Retry policy") — distinct from CreatePlaytest's
//     300s/no-retry initial-create exception.
//
// Audits: `campaign.generate_codes` on success (per batch is one AGS
// call but we collapse to one audit row carrying the total added);
// `campaign.generate_codes_failed` on AGS failure.
func (s *PlaytesthubServiceServer) TopUpCodes(ctx context.Context, req *pb.TopUpCodesRequest) (*pb.TopUpCodesResponse, error) {
	if _, err := requireActor(ctx); err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	if err := s.requireAGSCodePathWired(); err != nil {
		return nil, err
	}
	playtestID, err := parseReqUUID("playtest_id", req.GetPlaytestId())
	if err != nil {
		return nil, err
	}
	qty := req.GetQuantity()
	if qty < minInitialCodeQty || qty > maxInitialCodeQty {
		return nil, status.Errorf(codes.InvalidArgument,
			"quantity must be between %d and %d (got %d)", minInitialCodeQty, maxInitialCodeQty, qty)
	}

	pt, err := s.loadAGSCampaignPlaytest(ctx, playtestID)
	if err != nil {
		return nil, err
	}

	added, callErr := s.generateAndPersistTopUp(ctx, pt, int(qty))
	if callErr != nil {
		s.recordCampaignGenerateCodesFailed(ctx, pt, *pt.AGSCampaignID, int(qty), callErr)
		return nil, mapAGSError(callErr)
	}

	stats, err := s.codePoolStatsForPlaytest(ctx, pt.ID)
	if err != nil {
		return nil, err
	}
	if added > 0 {
		s.recordCampaignGenerateCodes(ctx, pt, *pt.AGSCampaignID, added, int(stats.GetTotal()))
	}
	if added < int(qty) {
		s.warnPartialFulfillment(pt.ID.String(), int(qty), added)
	}

	return &pb.TopUpCodesResponse{Pool: stats, Added: int32(added)}, nil
}

// SyncFromAGS pulls every code AGS holds for the playtest's campaign
// and persists any that the local DB is missing. Idempotent on
// UNIQUE(playtest_id, value): re-running after a successful sync is a
// no-op (PRD §4.6 step 5). Standard retry policy (30s/3 retries).
//
// Recovery use case: a CreatePlaytest tx that committed AGS-side codes
// but rolled back DB-side leaves an AGS pool the local DB never saw —
// SyncFromAGS reattaches the same campaign id's codes to the playtest.
//
// Audits: a single `campaign.generate_codes` row carrying the count
// actually added (system-emitted; no actor — the row schema reuses the
// generate_codes shape because the side effect is the same: pool size
// grew). `campaign.generate_codes_failed` on AGS failure.
func (s *PlaytesthubServiceServer) SyncFromAGS(ctx context.Context, req *pb.SyncFromAGSRequest) (*pb.SyncFromAGSResponse, error) {
	if _, err := requireActor(ctx); err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	if err := s.requireAGSCodePathWired(); err != nil {
		return nil, err
	}
	playtestID, err := parseReqUUID("playtest_id", req.GetPlaytestId())
	if err != nil {
		return nil, err
	}

	pt, err := s.loadAGSCampaignPlaytest(ctx, playtestID)
	if err != nil {
		return nil, err
	}

	fetched, fetchErr := s.agsFetchAllCodes(ctx, *pt.AGSCampaignID)
	if fetchErr != nil {
		s.recordCampaignGenerateCodesFailed(ctx, pt, *pt.AGSCampaignID, 0, fetchErr)
		return nil, mapAGSError(fetchErr)
	}

	added, insertErr := s.code.InsertGeneratedAtomic(ctx, pt.ID, fetched)
	if insertErr != nil {
		return nil, status.Errorf(codes.Internal, "syncing codes: %v", insertErr)
	}

	stats, err := s.codePoolStatsForPlaytest(ctx, pt.ID)
	if err != nil {
		return nil, err
	}
	if added > 0 {
		s.recordCampaignGenerateCodes(ctx, pt, *pt.AGSCampaignID, added, int(stats.GetTotal()))
	}

	return &pb.SyncFromAGSResponse{Pool: stats, Added: int32(added)}, nil
}

// requireAGSCodePathWired guards the AGS client + code store + tx
// runner trio. tx runner is not used by Top-up/Sync directly but the
// AGS_CAMPAIGN code path requires it for downstream paths the admin
// might invoke (Approve), and a missing dependency now is a wiring
// regression that should fail loudly.
func (s *PlaytesthubServiceServer) requireAGSCodePathWired() error {
	if s.agsClient == nil {
		return status.Error(codes.Internal, "ags client not wired")
	}
	if s.code == nil {
		return status.Error(codes.Internal, "code store not wired")
	}
	return nil
}

// loadAGSCampaignPlaytest fetches the playtest and applies the
// admin-write visibility gates: NotFound for missing/soft-deleted,
// FailedPrecondition for STEAM_KEYS, FailedPrecondition for missing
// `ags_campaign_id` (a malformed AGS_CAMPAIGN row that never finished
// auto-provisioning).
func (s *PlaytesthubServiceServer) loadAGSCampaignPlaytest(ctx context.Context, playtestID uuid.UUID) (*repo.Playtest, error) {
	pt, err := s.playtest.GetByID(ctx, s.namespace, playtestID)
	if e := mapPlaytestLookupErr(err, playtestSoftDelete(pt), "fetching playtest"); e != nil {
		return nil, e
	}
	if pt.DistributionModel != distModelAGSCampaign {
		return nil, status.Error(codes.FailedPrecondition,
			"distribution_model=AGS_CAMPAIGN required (this playtest is STEAM_KEYS; use UploadCodes)")
	}
	if pt.AGSCampaignID == nil || *pt.AGSCampaignID == "" {
		return nil, status.Error(codes.FailedPrecondition,
			"playtest is missing ags_campaign_id; recreate the playtest")
	}
	return pt, nil
}

// generateAndPersistTopUp drives the per-batch AGS round-trip + insert
// loop. Each batch is a separate AGS call AND a separate DB tx (advisory
// lock held only inside InsertGeneratedAtomic) so partial progress
// survives a mid-loop failure (PRD §4.6 "no DB tx — each batch inserted
// independently"). Returns total inserted + the first non-nil call
// error. Standard 30s / 3-retry policy applies per AGS call.
func (s *PlaytesthubServiceServer) generateAndPersistTopUp(ctx context.Context, pt *repo.Playtest, quantity int) (int, error) {
	batchSize := s.agsCodeBatchSize
	if batchSize <= 0 {
		batchSize = 1000
	}
	policy := ags.DefaultRetryPolicy()
	totalAdded := 0
	remaining := quantity
	for remaining > 0 {
		want := min(remaining, batchSize)
		var batch ags.CodeBatchResult
		err := policy.Run(ctx, "CreateCodes", func(attemptCtx context.Context) error {
			res, callErr := s.agsClient.CreateCodes(attemptCtx, *pt.AGSCampaignID, want)
			if callErr != nil {
				return callErr
			}
			batch = res
			return nil
		})
		if err != nil {
			return totalAdded, err
		}
		if len(batch.Codes) == 0 {
			break
		}
		added, insertErr := s.code.InsertGeneratedAtomic(ctx, pt.ID, batch.Codes)
		if insertErr != nil {
			return totalAdded, insertErr
		}
		totalAdded += added
		remaining -= len(batch.Codes)
		if len(batch.Codes) < want {
			break
		}
	}
	return totalAdded, nil
}

// agsFetchAllCodes wraps Client.FetchCodes in the standard retry
// policy. Pagination is the SDK adapter's job.
func (s *PlaytesthubServiceServer) agsFetchAllCodes(ctx context.Context, campaignID string) ([]string, error) {
	policy := ags.DefaultRetryPolicy()
	var out []string
	err := policy.Run(ctx, "FetchCodes", func(attemptCtx context.Context) error {
		got, callErr := s.agsClient.FetchCodes(attemptCtx, campaignID)
		if callErr != nil {
			return callErr
		}
		out = got
		return nil
	})
	return out, err
}

// codePoolStatsForPlaytest renders the post-call CodePoolStats from
// ListByPlaytest so TopUp / Sync responses carry the live counters.
func (s *PlaytesthubServiceServer) codePoolStatsForPlaytest(ctx context.Context, playtestID uuid.UUID) (*pb.CodePoolStats, error) {
	rows, err := s.code.ListByPlaytest(ctx, playtestID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "listing codes: %v", err)
	}
	return codePoolStats(rows), nil
}
