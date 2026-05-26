package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/anggorodewanto/playtesthub/pkg/adt"
	"github.com/anggorodewanto/playtesthub/pkg/agsid"
	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// Byte-exact gRPC error messages from docs/errors.md. Tests assert on
// these strings verbatim — DO NOT edit without updating errors.md.
const (
	errMsgReservationExpired       = "reservation expired, please retry"
	errMsgApplicantAlreadyApproved = "applicant already approved"
	errMsgRejectRaced              = "applicant was approved before reject could complete"
	errMsgApplicantRejected        = "applicant is rejected and cannot be re-approved"
	errMsgPlaytestClosed           = "playtest is closed; approve/reject is no longer allowed"
	errMsgPlaytestDraft            = "playtest is in draft; approve/reject requires OPEN status"
	errMsgPoolEmptySteamKeys       = "No codes remaining in pool. Upload more codes to continue approving."
	errMsgPoolEmptyAGSCampaign     = "No codes remaining in pool. Generate more codes to continue approving."
)

// errFinalizeOrphaned is the in-tx sentinel raised when the fenced
// finalize affects 0 rows. Surfaced to the caller as gRPC Aborted with
// the byte-exact errors.md message; the audit row is written outside
// the rolled-back tx so it survives.
var errFinalizeOrphaned = errors.New("service: fenced finalize affected 0 rows")

// WithTxRunner wires the tx orchestrator used by ApproveApplicant
// (M2 phase 6). Service constructions without one wired surface
// Internal on the approve path so a wiring regression fails loudly.
func (s *PlaytesthubServiceServer) WithTxRunner(r repo.TxRunner) *PlaytesthubServiceServer {
	s.txRunner = r
	return s
}

// ApproveApplicant runs the PRD §4.1 step 6 flow: reserve a code →
// fenced-finalize the grant → mark the applicant APPROVED, all in one
// DB transaction. On a fenced-finalize 0-row affected (the reclaim-
// and-steal race) the tx rolls back, a code.grant_orphaned audit row
// is written outside the rolled-back tx, and the caller gets gRPC
// Aborted with the byte-exact errors.md message.
//
// ADT distribution (M5.B): skips code reservation entirely; calls
// adt.Client.IssueDownloadURL → falls back to playtest.adtFallbackDownloadUrl
// on ADT 4xx/5xx (linkage row still present) → surfaces Unavailable
// otherwise. The DM body embeds the resolved URL.
//
// Idempotency: a second click on an already-APPROVED applicant
// returns the existing row without writing a new audit row or burning
// a code. REJECTED applicants surface FailedPrecondition.
func (s *PlaytesthubServiceServer) ApproveApplicant(ctx context.Context, req *pb.ApproveApplicantRequest) (*pb.ApproveApplicantResponse, error) {
	actorID, applicant, playtest, idempotent, err := s.resolveApproveContext(ctx, req)
	if err != nil {
		return nil, err
	}
	if idempotent != nil {
		return &pb.ApproveApplicantResponse{Applicant: adminApplicantToProto(idempotent)}, nil
	}
	if playtest.DistributionModel == distModelADT {
		return s.approveADT(ctx, actorID, applicant, playtest)
	}
	if s.code == nil {
		return nil, status.Error(codes.Internal, "code store not wired")
	}

	updated, grantedCodeID, txErr := s.runApproveTx(ctx, applicant, playtest)
	if txErr != nil {
		return nil, s.mapApproveTxError(ctx, txErr, applicant, playtest)
	}

	if s.audit != nil {
		if auditErr := repo.AppendApplicantApprove(ctx, s.audit, s.namespace, playtest.ID, actorID, updated.ID, grantedCodeID); auditErr != nil {
			return nil, status.Errorf(codes.Internal, "appending applicant.approve audit: %v", auditErr)
		}
	}

	// Enqueue the welcome DM. Auto-send (manual=false) so the worker
	// does not write applicant.dm_sent on success — that audit row is
	// reserved for RetryDM per PRD §5.4. Queue overflow surfaces inside
	// dmqueue.Enqueue as a synchronous markFailed (lastDmStatus='failed',
	// reason=dm_queue_overflow) — we ignore the returned error here so
	// the approve RPC stays non-blocking on DM behaviour, matching the
	// "approval RPC returns immediately" rule from dm-queue.md.
	if s.dmQueue != nil {
		_ = s.dmQueue.Enqueue(ctx, buildDMJob(updated, playtest, false, s.playerBaseURL, nil))
	}

	return &pb.ApproveApplicantResponse{Applicant: adminApplicantToProto(updated)}, nil
}

// approveADT runs the ADT-branch ApproveApplicant flow: no code pool,
// resolves a download URL (issued via adt.Client.IssueDownloadURL or
// the playtest's static fallback), marks the applicant APPROVED with
// no granted_code_id, writes the audit row + enqueues the DM.
func (s *PlaytesthubServiceServer) approveADT(ctx context.Context, actorID uuid.UUID, applicant *repo.Applicant, playtest *repo.Playtest) (*pb.ApproveApplicantResponse, error) {
	downloadURLs, source, resolveErr := s.resolveADTDownloadURL(ctx, playtest, applicant)
	if resolveErr != nil {
		return nil, resolveErr
	}

	var updated *repo.Applicant
	txErr := s.txRunner.InTx(ctx, func(q repo.Querier) error {
		upd, e := s.applicant.ApproveCASNoCode(ctx, q, applicant.ID, time.Now().UTC(), false)
		if e != nil {
			return e
		}
		updated = upd
		return nil
	})
	if errors.Is(txErr, repo.ErrStatusCASMismatch) {
		return nil, status.Error(codes.FailedPrecondition, errMsgApplicantAlreadyApproved)
	}
	if txErr != nil {
		return nil, status.Errorf(codes.Internal, "approve tx: %v", txErr)
	}

	if s.audit != nil {
		if auditErr := repo.AppendApplicantApproveADT(ctx, s.audit, s.namespace, playtest.ID, actorID, updated.ID, downloadURLs, source); auditErr != nil {
			return nil, status.Errorf(codes.Internal, "appending applicant.approve audit: %v", auditErr)
		}
	}

	if s.dmQueue != nil {
		_ = s.dmQueue.Enqueue(ctx, buildDMJob(updated, playtest, false, s.playerBaseURL, downloadURLs))
	}
	return &pb.ApproveApplicantResponse{Applicant: adminApplicantToProto(updated)}, nil
}

// adtURLSourceIssued / adtURLSourceFallback label whether an ADT
// download URL came from adt.Client.IssueDownloadURL or the playtest's
// static fallback. Persisted on the applicant.approve audit row.
const (
	adtURLSourceIssued   = "issued"
	adtURLSourceFallback = "fallback"
)

// resolveADTDownloadURL is the shared ADT URL-list resolver used by
// approve and the RetryDM paths. ADT 4xx/5xx with the linkage row
// still present falls back to a single-element list containing
// playtest.adtFallbackDownloadUrl when set. Linkage missing surfaces
// as FailedPrecondition with the byte-exact errors.md message. No
// fallback + no linkage + Unavailable → leaves applicant PENDING.
// Returns the full URL list ADT minted (one element per build asset)
// in ADT's original order.
func (s *PlaytesthubServiceServer) resolveADTDownloadURL(ctx context.Context, playtest *repo.Playtest, applicant *repo.Applicant) ([]string, string, error) {
	if s.adtClient == nil {
		return nil, "", status.Error(codes.Internal, "ADT client not configured")
	}
	if playtest.ADTNamespace == nil || playtest.ADTGameID == nil || playtest.ADTBuildID == nil {
		return nil, "", status.Error(codes.Internal, "ADT playtest missing identifiers")
	}
	studio, err := s.resolveStudioNamespace(ctx)
	if err != nil {
		return nil, "", err
	}
	issued, issueErr := s.adtClient.IssueDownloadURL(ctx, adt.IssueDownloadURLParams{
		StudioNamespace: studio,
		ADTNamespace:    *playtest.ADTNamespace,
		ADTGameID:       *playtest.ADTGameID,
		ADTBuildID:      *playtest.ADTBuildID,
		ApplicantIdent:  applicant.ID.String(),
	})
	// Record build health from this real issue attempt (M5.C) before any
	// fallback masks it — so the detail page reflects a dead ADT build even
	// when adt_fallback_download_url is keeping approvals working.
	s.recordADTBuildHealth(ctx, playtest, issueErr)
	if issueErr == nil {
		return issued.URLs, adtURLSourceIssued, nil
	}
	if errors.Is(issueErr, adt.ErrLinkageMissing) {
		return nil, "", status.Error(codes.FailedPrecondition, "adt linkage no longer exists or service token rejected, re-link required")
	}
	if playtest.ADTFallbackDownloadURL != nil && *playtest.ADTFallbackDownloadURL != "" {
		return []string{*playtest.ADTFallbackDownloadURL}, adtURLSourceFallback, nil
	}
	if errors.Is(issueErr, adt.ErrBuildNotFound) {
		return nil, "", status.Error(codes.FailedPrecondition, "ADT build no longer exists; it may have been deleted from ADT. Set a fallback download URL or re-create the playtest with a current build.")
	}
	return nil, "", status.Errorf(codes.Unavailable, "calling ADT IssueDownloadURL: %v", issueErr)
}

// resolveApproveContext loads the applicant + playtest and returns
// either an "idempotent return" handle (caller short-circuits) or the
// validated context for the tx phase. Splitting it out keeps the main
// handler short and brings the cognitive-complexity score under
// golangci-lint's gocognit cap.
func (s *PlaytesthubServiceServer) resolveApproveContext(ctx context.Context, req *pb.ApproveApplicantRequest) (uuid.UUID, *repo.Applicant, *repo.Playtest, *repo.Applicant, error) {
	actorID, err := requireActor(ctx)
	if err != nil {
		return uuid.Nil, nil, nil, nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return uuid.Nil, nil, nil, nil, err
	}
	if s.txRunner == nil {
		return uuid.Nil, nil, nil, nil, status.Error(codes.Internal, "tx runner not wired")
	}
	applicantID, err := parseReqUUID("applicant_id", req.GetApplicantId())
	if err != nil {
		return uuid.Nil, nil, nil, nil, err
	}
	a, err := s.applicant.GetByID(ctx, applicantID)
	if errors.Is(err, repo.ErrNotFound) {
		return uuid.Nil, nil, nil, nil, status.Error(codes.NotFound, "applicant not found")
	}
	if err != nil {
		return uuid.Nil, nil, nil, nil, status.Errorf(codes.Internal, "fetching applicant: %v", err)
	}
	if a.Status == applicantStatusApproved {
		return actorID, nil, nil, a, nil
	}
	if a.Status == applicantStatusRejected {
		return uuid.Nil, nil, nil, nil, status.Error(codes.FailedPrecondition, errMsgApplicantRejected)
	}
	pt, err := s.loadPlaytestForMutation(ctx, a.PlaytestID)
	if err != nil {
		return uuid.Nil, nil, nil, nil, err
	}
	return actorID, a, pt, nil, nil
}

// loadPlaytestForMutation fetches the playtest row and applies the
// approve/reject visibility gates: NotFound for missing/soft-deleted,
// FailedPrecondition for DRAFT/CLOSED.
func (s *PlaytesthubServiceServer) loadPlaytestForMutation(ctx context.Context, playtestID uuid.UUID) (*repo.Playtest, error) {
	pt, err := s.playtest.GetByID(ctx, s.namespace, playtestID)
	if e := mapPlaytestLookupErr(err, playtestSoftDelete(pt), "fetching playtest"); e != nil {
		return nil, e
	}
	if pt.Status == statusDraft {
		return nil, status.Error(codes.FailedPrecondition, errMsgPlaytestDraft)
	}
	if pt.Status == statusClosed {
		return nil, status.Error(codes.FailedPrecondition, errMsgPlaytestClosed)
	}
	return pt, nil
}

// runApproveTx executes the Reserve → FencedFinalize → ApproveCAS
// pipeline inside one DB tx and returns the updated applicant + the
// granted code id, or a sentinel error the caller maps via
// mapApproveTxError.
func (s *PlaytesthubServiceServer) runApproveTx(ctx context.Context, a *repo.Applicant, pt *repo.Playtest) (*repo.Applicant, uuid.UUID, error) {
	var (
		updated       *repo.Applicant
		grantedCodeID uuid.UUID
	)
	txErr := s.txRunner.InTx(ctx, func(q repo.Querier) error {
		code, e := s.code.Reserve(ctx, q, pt.ID, a.UserID)
		if e != nil {
			return e
		}
		rows, e := s.code.FencedFinalize(ctx, q, code.ID, a.UserID, *code.ReservedAt)
		if e != nil {
			return e
		}
		if rows == 0 {
			s.recordOrphanedGrant(ctx, pt, a, code)
			return errFinalizeOrphaned
		}
		upd, e := s.applicant.ApproveCAS(ctx, q, a.ID, code.ID, time.Now().UTC(), false)
		if e != nil {
			return e
		}
		updated = upd
		grantedCodeID = code.ID
		return nil
	})
	return updated, grantedCodeID, txErr
}

// recordOrphanedGrant writes the system-emitted code.grant_orphaned
// audit row outside the rolled-back tx so the audit trail survives
// the tx rollback (PRD §4.1 step 6b). Best-effort — a write failure
// here only loses the audit row, not the user-visible Aborted error.
func (s *PlaytesthubServiceServer) recordOrphanedGrant(ctx context.Context, pt *repo.Playtest, a *repo.Applicant, code *repo.Code) {
	if s.audit == nil || code == nil || code.ReservedAt == nil {
		return
	}
	_ = repo.AppendCodeGrantOrphaned(ctx, s.audit, s.namespace, pt.ID, a.ID, code.ID, a.UserID, *code.ReservedAt)
}

func (s *PlaytesthubServiceServer) mapApproveTxError(_ context.Context, txErr error, _ *repo.Applicant, pt *repo.Playtest) error {
	switch {
	case errors.Is(txErr, repo.ErrPoolEmpty):
		return poolEmptyError(pt.DistributionModel)
	case errors.Is(txErr, errFinalizeOrphaned):
		return status.Error(codes.Aborted, errMsgReservationExpired)
	case errors.Is(txErr, repo.ErrStatusCASMismatch):
		return status.Error(codes.FailedPrecondition, errMsgApplicantAlreadyApproved)
	}
	return status.Errorf(codes.Internal, "approve tx: %v", txErr)
}

// RejectApplicant marks a PENDING applicant REJECTED. Terminal — re-
// reject on an already-REJECTED row returns the existing row
// (idempotent natural-key replay per the proto comment); attempt to
// reject an APPROVED applicant is FailedPrecondition.
func (s *PlaytesthubServiceServer) RejectApplicant(ctx context.Context, req *pb.RejectApplicantRequest) (*pb.RejectApplicantResponse, error) {
	actorID, err := requireActor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	applicantID, err := parseReqUUID("applicant_id", req.GetApplicantId())
	if err != nil {
		return nil, err
	}

	a, err := s.applicant.GetByID(ctx, applicantID)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "applicant not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "fetching applicant: %v", err)
	}
	if a.Status == applicantStatusRejected {
		return &pb.RejectApplicantResponse{Applicant: adminApplicantToProto(a)}, nil
	}
	if a.Status == applicantStatusApproved {
		return nil, status.Error(codes.FailedPrecondition, "applicant is approved and cannot be rejected")
	}

	pt, err := s.playtest.GetByID(ctx, s.namespace, a.PlaytestID)
	if e := mapPlaytestLookupErr(err, playtestSoftDelete(pt), "fetching playtest"); e != nil {
		return nil, e
	}
	if pt.Status == statusDraft {
		return nil, status.Error(codes.FailedPrecondition, errMsgPlaytestDraft)
	}
	if pt.Status == statusClosed {
		return nil, status.Error(codes.FailedPrecondition, errMsgPlaytestClosed)
	}

	if s.txRunner == nil {
		return nil, status.Error(codes.Internal, "tx runner not wired")
	}
	var reason *string
	if req.RejectionReason != nil {
		v := req.GetRejectionReason()
		reason = &v
	}

	var updated *repo.Applicant
	txErr := s.txRunner.InTx(ctx, func(q repo.Querier) error {
		upd, e := s.applicant.RejectCAS(ctx, q, a.ID, reason)
		if e != nil {
			return e
		}
		updated = upd
		return nil
	})
	if errors.Is(txErr, repo.ErrStatusCASMismatch) {
		return nil, status.Error(codes.FailedPrecondition, errMsgRejectRaced)
	}
	if txErr != nil {
		return nil, status.Errorf(codes.Internal, "rejecting applicant: %v", txErr)
	}

	if s.audit != nil {
		reasonStr := ""
		if reason != nil {
			reasonStr = *reason
		}
		if auditErr := repo.AppendApplicantReject(ctx, s.audit, s.namespace, pt.ID, actorID, updated.ID, reasonStr); auditErr != nil {
			return nil, status.Errorf(codes.Internal, "appending applicant.reject audit: %v", auditErr)
		}
	}

	return &pb.RejectApplicantResponse{Applicant: adminApplicantToProto(updated)}, nil
}

// ListApplicants returns a page of admin-visible applicant rows for a
// playtest. Cursor pagination on (created_at DESC, id DESC); page 50
// default, server-capped at 200. UNSPECIFIED status_filter and
// dm_failed_filter=false return the full set.
func (s *PlaytesthubServiceServer) ListApplicants(ctx context.Context, req *pb.ListApplicantsRequest) (*pb.ListApplicantsResponse, error) {
	if _, err := requireActor(ctx); err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	playtestID, err := parseReqUUID("playtest_id", req.GetPlaytestId())
	if err != nil {
		return nil, err
	}

	pt, err := s.playtest.GetByID(ctx, s.namespace, playtestID)
	if e := mapPlaytestLookupErr(err, playtestSoftDelete(pt), "fetching playtest"); e != nil {
		return nil, e
	}

	page, err := s.applicant.ListPaged(ctx, repo.ApplicantPageQuery{
		PlaytestID:   pt.ID,
		Status:       applicantStatusFilterFromPb(req.GetStatusFilter()),
		DMFailedOnly: req.GetDmFailedFilter(),
		PageToken:    req.GetPageToken(),
		Limit:        int(req.GetPageSize()),
	})
	if errors.Is(err, repo.ErrInvalidPageToken) {
		return nil, status.Error(codes.InvalidArgument, "page_token is malformed")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "listing applicants: %v", err)
	}

	out := make([]*pb.Applicant, 0, len(page.Rows))
	for _, r := range page.Rows {
		out = append(out, adminApplicantToProto(r))
	}
	return &pb.ListApplicantsResponse{Applicants: out, NextPageToken: page.NextPageToken}, nil
}

// GetGrantedCode returns the raw redemption value for the caller's
// own approved applicant row. Soft-deleted playtests return NotFound
// regardless of applicant state per errors.md row 30. Player-only —
// admin reads the same data via GetCodePool.
func (s *PlaytesthubServiceServer) GetGrantedCode(ctx context.Context, req *pb.GetGrantedCodeRequest) (*pb.GetGrantedCodeResponse, error) {
	userID, err := requireActor(ctx)
	if err != nil {
		return nil, err
	}
	playtestID, err := parseReqUUID("playtest_id", req.GetPlaytestId())
	if err != nil {
		return nil, err
	}

	pt, err := s.playtest.GetByID(ctx, s.namespace, playtestID)
	if e := mapPlaytestLookupErr(err, playtestSoftDelete(pt), "fetching playtest"); e != nil {
		return nil, e
	}
	if pt.Status == statusDraft {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}
	if pt.DistributionModel == distModelADT {
		return nil, status.Error(codes.FailedPrecondition, "ADT playtest has no code pool; use GetADTDownloadInfo")
	}
	if s.code == nil {
		return nil, status.Error(codes.Internal, "code store not wired")
	}

	a, err := s.applicant.GetByPlaytestUser(ctx, pt.ID, userID)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "applicant not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "fetching applicant: %v", err)
	}
	if a.Status != applicantStatusApproved || a.GrantedCodeID == nil {
		return nil, status.Error(codes.NotFound, "no granted code")
	}

	code, err := s.code.GetByID(ctx, *a.GrantedCodeID)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "no granted code")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "fetching code: %v", err)
	}
	return &pb.GetGrantedCodeResponse{
		Value:             code.Value,
		DistributionModel: distModelStringToEnum(pt.DistributionModel),
	}, nil
}

// GetADTDownloadInfo is the player-side ADT-distribution equivalent of
// GetGrantedCode (M5.B / PRD §4.8). Gated on APPROVED applicant row;
// returns a fresh URL minted via adt.Client.IssueDownloadURL or the
// playtest's static fallback. FailedPrecondition for non-ADT playtests.
func (s *PlaytesthubServiceServer) GetADTDownloadInfo(ctx context.Context, req *pb.GetADTDownloadInfoRequest) (*pb.GetADTDownloadInfoResponse, error) {
	userID, err := requireActor(ctx)
	if err != nil {
		return nil, err
	}
	playtestID, err := parseReqUUID("playtest_id", req.GetPlaytestId())
	if err != nil {
		return nil, err
	}
	pt, err := s.playtest.GetByID(ctx, s.namespace, playtestID)
	if e := mapPlaytestLookupErr(err, playtestSoftDelete(pt), "fetching playtest"); e != nil {
		return nil, e
	}
	if pt.Status == statusDraft {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}
	if pt.DistributionModel != distModelADT {
		return nil, status.Error(codes.FailedPrecondition, "playtest is not ADT-distribution; use GetGrantedCode")
	}
	a, err := s.applicant.GetByPlaytestUser(ctx, pt.ID, userID)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "applicant not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "fetching applicant: %v", err)
	}
	if a.Status != applicantStatusApproved {
		return nil, status.Error(codes.NotFound, "no granted download")
	}
	urls, source, resolveErr := s.resolveADTDownloadURL(ctx, pt, a)
	if resolveErr != nil {
		return nil, resolveErr
	}
	out := &pb.GetADTDownloadInfoResponse{
		Urls:   urls,
		Source: source,
	}
	return out, nil
}

// poolEmptyError selects the byte-exact errors.md message for the
// playtest's distribution model. The two strings differ by a single
// noun ("Upload more" vs "Generate more") because the admin remediation
// is different per model.
func poolEmptyError(distModel string) error {
	switch distModel {
	case distModelSteamKeys:
		return status.Error(codes.ResourceExhausted, errMsgPoolEmptySteamKeys)
	case distModelAGSCampaign:
		return status.Error(codes.ResourceExhausted, errMsgPoolEmptyAGSCampaign)
	}
	return status.Error(codes.ResourceExhausted, errMsgPoolEmptySteamKeys)
}

// DM-status string values persisted in applicant.last_dm_status. The
// real consumer lands in M2 phase 7 (DM queue + RetryDM); the proto
// enum mapper here is the read-side adapter for the admin response.
const (
	dmStatusSent   = "sent"
	dmStatusFailed = "failed"
)

func dmStatusStringToEnum(s string) pb.DmStatus {
	switch s {
	case dmStatusSent:
		return pb.DmStatus_DM_STATUS_SENT
	case dmStatusFailed:
		return pb.DmStatus_DM_STATUS_FAILED
	}
	return pb.DmStatus_DM_STATUS_UNSPECIFIED
}

// adminApplicantToProto renders the full admin-visible field set
// (schema.md L86) — every column is exposed to the admin, in contrast
// to playerApplicantToProto which strips DM state, discord handle,
// platforms, and rejection reason.
func adminApplicantToProto(a *repo.Applicant) *pb.Applicant {
	out := &pb.Applicant{
		Id:            a.ID.String(),
		PlaytestId:    a.PlaytestID.String(),
		UserId:        agsid.Format(a.UserID),
		DiscordHandle: a.DiscordHandle,
		Platforms:     stringsToPlatforms(a.Platforms),
		Status:        applicantStatusStringToEnum(a.Status),
		CreatedAt:     timestamppb.New(a.CreatedAt),
	}
	if a.NDAVersionHash != nil {
		v := *a.NDAVersionHash
		out.NdaVersionHash = &v
	}
	if a.GrantedCodeID != nil {
		v := a.GrantedCodeID.String()
		out.GrantedCodeId = &v
	}
	if a.ApprovedAt != nil {
		out.ApprovedAt = timestamppb.New(*a.ApprovedAt)
	}
	if a.RejectionReason != nil {
		v := *a.RejectionReason
		out.RejectionReason = &v
	}
	if a.LastDMStatus != nil {
		out.LastDmStatus = dmStatusStringToEnum(*a.LastDMStatus)
	}
	if a.LastDMAttemptAt != nil {
		out.LastDmAttemptAt = timestamppb.New(*a.LastDMAttemptAt)
	}
	if a.LastDMError != nil {
		v := *a.LastDMError
		out.LastDmError = &v
	}
	out.AutoApproved = a.AutoApproved
	return out
}
