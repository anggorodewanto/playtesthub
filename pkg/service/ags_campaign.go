package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/anggorodewanto/playtesthub/pkg/ags"
	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// initialCodeQuantity bounds — PRD §4.6 / errors.md row 27.
const (
	minInitialCodeQty = 1
	maxInitialCodeQty = 50_000
)

// agsInitialCreateTimeout is the §4.6 exception: the entire AGS
// generate+fetch sequence on initial create is bounded by 300s with
// no per-call retry.
const agsInitialCreateTimeout = 300 * time.Second

// WithAGSClient wires the AGS Platform client used by AGS_CAMPAIGN
// playtest creation (M2 phase 8) and the future TopUpCodes /
// SyncFromAGS RPCs (phase 9). Service constructions without one
// wired surface Internal on the AGS_CAMPAIGN create path so a wiring
// regression fails loudly.
func (s *PlaytesthubServiceServer) WithAGSClient(c ags.Client) *PlaytesthubServiceServer {
	s.agsClient = c
	return s
}

// WithAGSCodeBatchSize overrides the default batch size used for
// AGS CreateCodes pagination (PRD §4.6 / docs/ags-failure-modes.md).
// Bootapp passes the env-driven value from config.AGSCodeBatchSize.
func (s *PlaytesthubServiceServer) WithAGSCodeBatchSize(n int) *PlaytesthubServiceServer {
	if n > 0 {
		s.agsCodeBatchSize = n
	}
	return s
}

// WithLogger sets the slog.Logger used for AGS partial-fulfillment
// warnings + cleanup-failure logs. Defaults to slog.Default when not
// set (unit tests live without a logger configured).
func (s *PlaytesthubServiceServer) WithLogger(l *slog.Logger) *PlaytesthubServiceServer {
	s.logger = l
	return s
}

// validateAGSCampaignRequest enforces the AGS_CAMPAIGN-specific
// preconditions for CreatePlaytest. Returns the validated quantity
// or a status error.
func validateAGSCampaignRequest(req *pb.CreatePlaytestRequest) (int32, error) {
	if req.InitialCodeQuantity == nil {
		return 0, status.Error(codes.InvalidArgument, "initial_code_quantity is required for AGS_CAMPAIGN")
	}
	q := req.GetInitialCodeQuantity()
	if q < minInitialCodeQty || q > maxInitialCodeQty {
		return 0, status.Errorf(codes.InvalidArgument, "initial_code_quantity must be between %d and %d (got %d)", minInitialCodeQty, maxInitialCodeQty, q)
	}
	return q, nil
}

// createAGSCampaignPlaytest runs the PRD §4.6 step 2 sequence:
// AGS Item create → AGS Campaign create → AGS CreateCodes (batched) →
// playtest insert + code insert in one DB tx. AGS calls happen first
// so the playtest row carries the ags ids on initial INSERT (avoids
// a second UPDATE inside the tx); a partial AGS failure runs the
// cleanup matrix (delete campaign + item, best-effort, log WARN on
// failure) before surfacing the error to the admin.
func (s *PlaytesthubServiceServer) createAGSCampaignPlaytest(ctx context.Context, draft *repo.Playtest, quantity int32) (*pb.CreatePlaytestResponse, error) {
	if s.agsClient == nil {
		return nil, status.Error(codes.Internal, "ags client not wired")
	}
	if s.txRunner == nil {
		return nil, status.Error(codes.Internal, "tx runner not wired")
	}
	if s.code == nil {
		return nil, status.Error(codes.Internal, "code store not wired")
	}

	// PRD §4.6 exception: 300s timeout for the entire
	// generate+fetch sequence on initial create, with no retries.
	agsCtx, cancel := context.WithTimeout(ctx, agsInitialCreateTimeout)
	defer cancel()

	itemID, err := s.agsCreateItem(agsCtx, draft)
	if err != nil {
		s.recordCampaignCreateFailed(ctx, draft, err, false, false)
		return nil, mapAGSError(err)
	}

	campaignID, err := s.agsCreateCampaign(agsCtx, draft, itemID)
	if err != nil {
		cleanupOK := s.cleanupAfterCampaignCreateFailure(ctx, itemID, "")
		s.recordCampaignCreateFailed(ctx, draft, err, true, cleanupOK)
		return nil, mapAGSError(err)
	}

	generated, partialFulfillment, err := s.agsCreateCodesBatched(agsCtx, campaignID, int(quantity))
	if err != nil {
		cleanupOK := s.cleanupAfterCampaignCreateFailure(ctx, itemID, campaignID)
		s.recordCampaignGenerateCodesFailed(ctx, draft, campaignID, int(quantity), err)
		s.recordCampaignCreateFailed(ctx, draft, err, true, cleanupOK)
		return nil, mapAGSError(err)
	}

	draft.AGSItemID = &itemID
	draft.AGSCampaignID = &campaignID

	var inserted *repo.Playtest
	txErr := s.txRunner.InTx(ctx, func(q repo.Querier) error {
		got, createErr := s.playtest.CreateTx(ctx, q, draft)
		if createErr != nil {
			return createErr
		}
		// CopyFrom is atomic per batch (UNIQUE (playtest_id, value)
		// would abort the whole batch on collision; impossible here
		// because AGS-generated values are namespaced per campaign).
		if _, copyErr := s.code.BulkInsertGeneratedTx(ctx, q, got.ID, generated); copyErr != nil {
			return copyErr
		}
		inserted = got
		return nil
	})
	if txErr != nil {
		// DB rolled back. Run AGS cleanup so we don't leak the
		// just-provisioned Item + Campaign.
		_ = s.cleanupAfterCampaignCreateFailure(ctx, itemID, campaignID)
		s.recordCampaignCreateFailed(ctx, draft, txErr, true, false)
		if errors.Is(txErr, repo.ErrUniqueViolation) {
			return nil, status.Errorf(codes.AlreadyExists, "slug %q already exists in namespace %q", draft.Slug, s.namespace)
		}
		return nil, status.Errorf(codes.Internal, "creating playtest with codes: %v", txErr)
	}

	// Audit on success. Use the playtest id assigned by Postgres
	// so the rows reference the actual entity.
	s.recordCampaignCreate(ctx, inserted, itemID, campaignID, int(quantity))
	s.recordCampaignGenerateCodes(ctx, inserted, campaignID, len(generated), len(generated))

	if partialFulfillment {
		s.warnPartialFulfillment(inserted.ID.String(), int(quantity), len(generated))
	}

	return &pb.CreatePlaytestResponse{Playtest: playtestToProto(inserted)}, nil
}

// agsCreateItem provisions an ENTITLEMENT-type Item derived from the
// playtest's title/description.
func (s *PlaytesthubServiceServer) agsCreateItem(ctx context.Context, draft *repo.Playtest) (string, error) {
	policy := ags.WithoutRetries()
	var id string
	err := policy.Run(ctx, "CreateItem", func(attemptCtx context.Context) error {
		got, callErr := s.agsClient.CreateItem(attemptCtx, ags.ItemSpec{
			Name:        draft.Title,
			Description: draft.Description,
		})
		if callErr != nil {
			return callErr
		}
		id = got
		return nil
	})
	return id, err
}

// agsCreateCampaign provisions a Campaign referencing itemID.
func (s *PlaytesthubServiceServer) agsCreateCampaign(ctx context.Context, draft *repo.Playtest, itemID string) (string, error) {
	policy := ags.WithoutRetries()
	var id string
	err := policy.Run(ctx, "CreateCampaign", func(attemptCtx context.Context) error {
		got, callErr := s.agsClient.CreateCampaign(attemptCtx, ags.CampaignSpec{
			Name:        draft.Title,
			Description: draft.Description,
			ItemID:      itemID,
		})
		if callErr != nil {
			return callErr
		}
		id = got
		return nil
	})
	return id, err
}

// agsCreateCodesBatched walks the requested quantity in batches of
// agsCodeBatchSize, accumulating the returned values. partial is
// true when total received < total requested but every batch
// succeeded — the §4.6 partial-fulfillment commit-and-warn path.
func (s *PlaytesthubServiceServer) agsCreateCodesBatched(ctx context.Context, campaignID string, quantity int) ([]string, bool, error) {
	batchSize := s.agsCodeBatchSize
	if batchSize <= 0 {
		batchSize = 1000
	}
	out := make([]string, 0, quantity)
	remaining := quantity
	policy := ags.WithoutRetries()
	for remaining > 0 {
		want := min(remaining, batchSize)
		var batchOut ags.CodeBatchResult
		err := policy.Run(ctx, "CreateCodes", func(attemptCtx context.Context) error {
			res, callErr := s.agsClient.CreateCodes(attemptCtx, campaignID, want)
			if callErr != nil {
				return callErr
			}
			batchOut = res
			return nil
		})
		if err != nil {
			return nil, false, err
		}
		out = append(out, batchOut.Codes...)
		remaining -= len(batchOut.Codes)
		if len(batchOut.Codes) < want {
			// Partial fulfillment on this batch — stop here so the
			// caller can commit what we have and warn the admin.
			break
		}
	}
	return out, len(out) < quantity, nil
}

// cleanupAfterCampaignCreateFailure runs the docs/ags-failure-modes.md
// cleanup matrix. Both deletes are best-effort; failures log WARN
// and do NOT propagate to the admin (the original error is what
// matters). campaignID may be empty when only Item was created.
func (s *PlaytesthubServiceServer) cleanupAfterCampaignCreateFailure(ctx context.Context, itemID, campaignID string) bool {
	logger := s.loggerOrDefault()
	allOK := true
	if campaignID != "" {
		if err := s.agsClient.DeleteCampaign(ctx, campaignID); err != nil {
			allOK = false
			logger.Warn("ags cleanup: delete campaign failed", "agsCampaignId", campaignID, "agsItemId", itemID, "error", err.Error())
		}
	}
	if itemID != "" {
		if err := s.agsClient.DeleteItem(ctx, itemID); err != nil {
			allOK = false
			logger.Warn("ags cleanup: delete item failed", "agsItemId", itemID, "error", err.Error())
		}
	}
	return allOK
}

func (s *PlaytesthubServiceServer) recordCampaignCreate(ctx context.Context, p *repo.Playtest, itemID, campaignID string, quantity int) {
	if s.audit == nil {
		return
	}
	if err := repo.AppendCampaignCreate(ctx, s.audit, s.namespace, p.ID, itemID, campaignID, p.Title, quantity); err != nil {
		s.loggerOrDefault().Warn("appending campaign.create audit failed", "error", err.Error())
	}
}

func (s *PlaytesthubServiceServer) recordCampaignCreateFailed(ctx context.Context, p *repo.Playtest, callErr error, cleanupAttempted, cleanupSuccess bool) {
	if s.audit == nil {
		return
	}
	// Pre-INSERT failures have no playtest_id; use uuid.Nil-to-empty
	// translation by passing an empty uuid (the audit row ends up
	// namespace-scoped).
	id := p.ID
	if err := repo.AppendCampaignCreateFailed(ctx, s.audit, s.namespace, id, callErr.Error(), cleanupAttempted, cleanupSuccess); err != nil {
		s.loggerOrDefault().Warn("appending campaign.create_failed audit failed", "error", err.Error())
	}
}

func (s *PlaytesthubServiceServer) recordCampaignGenerateCodes(ctx context.Context, p *repo.Playtest, campaignID string, quantity, total int) {
	if s.audit == nil {
		return
	}
	if err := repo.AppendCampaignGenerateCodes(ctx, s.audit, s.namespace, p.ID, campaignID, quantity, total); err != nil {
		s.loggerOrDefault().Warn("appending campaign.generate_codes audit failed", "error", err.Error())
	}
}

func (s *PlaytesthubServiceServer) recordCampaignGenerateCodesFailed(ctx context.Context, p *repo.Playtest, campaignID string, requested int, callErr error) {
	if s.audit == nil {
		return
	}
	if err := repo.AppendCampaignGenerateCodesFailed(ctx, s.audit, s.namespace, p.ID, campaignID, requested, callErr.Error()); err != nil {
		s.loggerOrDefault().Warn("appending campaign.generate_codes_failed audit failed", "error", err.Error())
	}
}

func (s *PlaytesthubServiceServer) warnPartialFulfillment(playtestID string, requested, actual int) {
	s.loggerOrDefault().Warn(
		fmt.Sprintf("Requested %d codes, received %d. You may need to top up.", requested, actual),
		"event", "ags_partial_fulfillment",
		"playtestId", playtestID,
		"requested", requested,
		"received", actual,
	)
}

func (s *PlaytesthubServiceServer) loggerOrDefault() *slog.Logger {
	if s.logger != nil {
		return s.logger
	}
	return slog.Default()
}

// mapAGSError translates the pkg/ags sentinels into byte-exact gRPC
// rows from docs/errors.md (rows 23 + 24). Other errors collapse to
// Internal so AGS-side bugs don't leak as misleading codes.
func mapAGSError(err error) error {
	if errors.Is(err, ags.ErrRateLimited) {
		return status.Error(codes.ResourceExhausted, "upstream AGS rate limited (HTTP 429)")
	}
	if errors.Is(err, ags.ErrUnavailable) {
		return status.Error(codes.Unavailable, "upstream AGS unavailable (retries exhausted)")
	}
	var ce *ags.ClientError
	if errors.As(err, &ce) {
		return status.Errorf(codes.Internal, "ags %s: client error %d: %s", ce.Op, ce.StatusCode, ce.Message)
	}
	return status.Errorf(codes.Internal, "ags: %v", err)
}
