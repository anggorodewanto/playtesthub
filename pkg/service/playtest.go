package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/anggorodewanto/playtesthub/pkg/discord"
	iampkg "github.com/anggorodewanto/playtesthub/pkg/iam"
	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// Playtest.status TEXT values (migration 0001). Kept in sync with
// docs/schema.md; migration 0001 checks the column against exactly
// these values.
const (
	statusDraft  = "DRAFT"
	statusOpen   = "OPEN"
	statusClosed = "CLOSED"
)

// Applicant.status TEXT values (migration 0001). CHECK constraint on the
// column pins these exact strings.
const (
	applicantStatusPending  = "PENDING"
	applicantStatusApproved = "APPROVED"
	applicantStatusRejected = "REJECTED"
)

// Playtest.distributionModel TEXT values (migration 0001 / PRD §5.1).
const (
	distModelSteamKeys   = "STEAM_KEYS"
	distModelAGSCampaign = "AGS_CAMPAIGN"
)

// PlaytesthubServiceServer is the gRPC handler for the playtesthub.v1
// service. Stores are mocked in pkg/service/*_test.go; the production
// wiring in main.go passes Postgres-backed implementations.
//
// namespace is the single AGS namespace this instance serves (PRD §5.1:
// `Playtest.namespace` is populated from AGS_NAMESPACE — no per-request
// override). Path-param namespaces must match; mismatches return
// PermissionDenied so a token that is valid for a different namespace
// cannot be used to pivot.
type PlaytesthubServiceServer struct {
	pb.UnimplementedPlaytesthubServiceServer

	playtest        repo.PlaytestStore
	applicant       repo.ApplicantStore
	nda             repo.NDAAcceptanceStore
	audit           repo.AuditLogStore
	code            repo.CodeStore
	txRunner        repo.TxRunner
	discord         discord.HandleLookup
	discordExchange DiscordExchangeProxy
	dmQueue         DMEnqueuer
	namespace       string
}

// NewPlaytesthubServiceServer wires a service with real repositories.
// Callers that want the bare skeleton (e.g. pre-phase-6 smoke-harness
// boots) can pass nil for both stores — every RPC will surface Internal
// until a concrete store is wired. The optional nda + audit stores are
// attached via WithNDAStore / WithAuditLogStore so the M1 handler tests
// (which never exercise the M2 click-accept path) keep their existing
// constructor calls.
func NewPlaytesthubServiceServer(playtest repo.PlaytestStore, applicant repo.ApplicantStore, namespace string) *PlaytesthubServiceServer {
	return &PlaytesthubServiceServer{
		playtest:  playtest,
		applicant: applicant,
		namespace: namespace,
	}
}

// WithNDAStore attaches the NDA-acceptance repository required by
// AcceptNDA (M2 phase 4). Optional in M1; AcceptNDA returns Internal
// when called without one wired.
func (s *PlaytesthubServiceServer) WithNDAStore(n repo.NDAAcceptanceStore) *PlaytesthubServiceServer {
	s.nda = n
	return s
}

// WithAuditLogStore attaches the audit-log repository. Required by every
// admin write path that emits an audit row (EditPlaytest's nda.edit and
// every M2 typed writer). Calls fall back to silent-no-op when nil so
// pre-M2 unit tests can still construct the server.
func (s *PlaytesthubServiceServer) WithAuditLogStore(a repo.AuditLogStore) *PlaytesthubServiceServer {
	s.audit = a
	return s
}

// requireActor returns the AGS user id stashed by the auth interceptor,
// or Unauthenticated when the context is missing one. Every admin and
// player RPC short-circuits through this helper.
func requireActor(ctx context.Context) (uuid.UUID, error) {
	sub, ok := iampkg.ActorUserIDFromContext(ctx)
	if !ok {
		return uuid.Nil, status.Error(codes.Unauthenticated, "actor user id missing from context")
	}
	id, err := uuid.Parse(sub)
	if err != nil {
		return uuid.Nil, status.Errorf(codes.Unauthenticated, "actor user id is not a uuid: %v", err)
	}
	return id, nil
}

// CreatePlaytest inserts a new playtest row. STEAM_KEYS only in M1 —
// AGS_CAMPAIGN returns Unimplemented until M2 (errors.md).
func (s *PlaytesthubServiceServer) CreatePlaytest(ctx context.Context, req *pb.CreatePlaytestRequest) (*pb.CreatePlaytestResponse, error) {
	if _, err := requireActor(ctx); err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	if req.GetDistributionModel() == pb.DistributionModel_DISTRIBUTION_MODEL_AGS_CAMPAIGN {
		return nil, status.Error(codes.Unimplemented, "distribution_model=AGS_CAMPAIGN lands in M2")
	}
	if req.GetDistributionModel() != pb.DistributionModel_DISTRIBUTION_MODEL_STEAM_KEYS {
		return nil, status.Error(codes.InvalidArgument, "distribution_model is required")
	}
	// STEAM_KEYS sources codes from admin CSV upload (M2), not AGS Campaign
	// generation — initialCodeQuantity has no meaning here and silently
	// dropping it hides client bugs. PRD §5.1 / §4.6.
	if req.InitialCodeQuantity != nil {
		return nil, status.Error(codes.InvalidArgument, "initial_code_quantity must not be set for STEAM_KEYS (only AGS_CAMPAIGN uses it; PRD §5.1)")
	}
	if err := validateSlug(req.GetSlug()); err != nil {
		return nil, err
	}
	if err := validateTitle(req.GetTitle()); err != nil {
		return nil, err
	}
	if err := validateDescription(req.GetDescription()); err != nil {
		return nil, err
	}
	if err := validateBannerURL(req.GetBannerImageUrl()); err != nil {
		return nil, err
	}
	if err := validateNDA(req.GetNdaRequired(), req.GetNdaText()); err != nil {
		return nil, err
	}
	platforms, err := platformsToStrings(req.GetPlatforms())
	if err != nil {
		return nil, wrapPlatformsErr(err)
	}

	// PRD §5.1: slugs stay reserved across soft-deletes — the 100-playtest
	// cap therefore counts live + soft-deleted rows. Counting only live
	// rows would let create-then-soft-delete churn bypass the cap while
	// still burning the slug namespace.
	existing, err := s.playtest.List(ctx, s.namespace, true)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "listing playtests for cap check: %v", err)
	}
	if len(existing) >= maxNamespacePlayt {
		return nil, status.Errorf(codes.ResourceExhausted, "namespace %q has reached the %d-playtest soft cap", s.namespace, maxNamespacePlayt)
	}

	ndaHash := hashNDA(req.GetNdaText())
	p := &repo.Playtest{
		Namespace:             s.namespace,
		Slug:                  req.GetSlug(),
		Title:                 req.GetTitle(),
		Description:           req.GetDescription(),
		BannerImageURL:        req.GetBannerImageUrl(),
		Platforms:             platforms,
		StartsAt:              timestampToTime(req.GetStartsAt()),
		EndsAt:                timestampToTime(req.GetEndsAt()),
		Status:                statusDraft,
		NDARequired:           req.GetNdaRequired(),
		NDAText:               req.GetNdaText(),
		CurrentNDAVersionHash: ndaHash,
		DistributionModel:     distModelSteamKeys,
	}

	got, err := s.playtest.Create(ctx, p)
	if errors.Is(err, repo.ErrUniqueViolation) {
		return nil, status.Errorf(codes.AlreadyExists, "slug %q already exists in namespace %q", req.GetSlug(), s.namespace)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "creating playtest: %v", err)
	}
	return &pb.CreatePlaytestResponse{Playtest: playtestToProto(got)}, nil
}

// EditPlaytest updates the mutable field set on an existing playtest.
// The proto request shape enforces the PRD §5.1 whitelist — immutable
// fields are not representable on the wire.
//
// Semantics: full-replace of the mutable set. Clients must fetch the
// current row (AdminGetPlaytest), mutate the fields they intend to
// change, and send the complete message back. Omitted scalars come in
// as their proto3 zero value and overwrite existing data — `{"title":
// "new"}` is a destructive edit, not a PATCH. The Admin Portal UI
// always sends the complete mutable set, so this is not a UX cliff in
// practice. PRD §5.3 NDA hash is only recomputed when nda_text
// actually differs from the stored value, so a no-op re-send does not
// force every approved applicant into re-accept.
func (s *PlaytesthubServiceServer) EditPlaytest(ctx context.Context, req *pb.EditPlaytestRequest) (*pb.EditPlaytestResponse, error) {
	actorID, err := requireActor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(req.GetPlaytestId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "playtest_id is not a uuid: %v", err)
	}
	if err := validateTitle(req.GetTitle()); err != nil {
		return nil, err
	}
	if err := validateDescription(req.GetDescription()); err != nil {
		return nil, err
	}
	if err := validateBannerURL(req.GetBannerImageUrl()); err != nil {
		return nil, err
	}
	if err := validateNDA(req.GetNdaRequired(), req.GetNdaText()); err != nil {
		return nil, err
	}
	platforms, err := platformsToStrings(req.GetPlatforms())
	if err != nil {
		return nil, wrapPlatformsErr(err)
	}

	current, err := s.playtest.GetByID(ctx, s.namespace, id)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "fetching playtest: %v", err)
	}
	if current.DeletedAt != nil {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}

	previousNDAText := current.NDAText
	ndaTextChanged := req.GetNdaText() != current.NDAText

	current.Title = req.GetTitle()
	current.Description = req.GetDescription()
	current.BannerImageURL = req.GetBannerImageUrl()
	current.Platforms = platforms
	current.StartsAt = timestampToTime(req.GetStartsAt())
	current.EndsAt = timestampToTime(req.GetEndsAt())
	current.NDARequired = req.GetNdaRequired()
	// PRD §5.3: changing NDA text forces every approved applicant back
	// to re-accept. Only recompute the version hash when the text has
	// actually changed so clients can edit cosmetic fields without
	// churning the acceptance workflow.
	if ndaTextChanged {
		current.NDAText = req.GetNdaText()
		current.CurrentNDAVersionHash = hashNDA(req.GetNdaText())
	}

	got, err := s.playtest.Update(ctx, current)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "updating playtest: %v", err)
	}
	// PRD §5.3 / schema.md L42: NDA-text edits are the one audit row
	// where the full free-text payload is intentionally preserved (every
	// other audited action redacts free-text). Skip when audit store is
	// unset (M1 unit tests construct without it).
	if ndaTextChanged && s.audit != nil {
		if auditErr := repo.AppendNDAEdit(ctx, s.audit, s.namespace, got.ID, actorID, previousNDAText, got.NDAText); auditErr != nil {
			return nil, status.Errorf(codes.Internal, "appending nda.edit audit: %v", auditErr)
		}
	}
	return &pb.EditPlaytestResponse{Playtest: playtestToProto(got)}, nil
}

// SoftDeletePlaytest sets deletedAt. Idempotent in intent — a second
// call on an already-deleted row returns NotFound, which clients can
// treat as "already done" (PRD §5.1: soft-delete is one-way and final).
func (s *PlaytesthubServiceServer) SoftDeletePlaytest(ctx context.Context, req *pb.SoftDeletePlaytestRequest) (*pb.SoftDeletePlaytestResponse, error) {
	if _, err := requireActor(ctx); err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(req.GetPlaytestId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "playtest_id is not a uuid: %v", err)
	}
	if err := s.playtest.SoftDelete(ctx, s.namespace, id); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "playtest not found")
		}
		return nil, status.Errorf(codes.Internal, "soft-deleting playtest: %v", err)
	}
	return &pb.SoftDeletePlaytestResponse{}, nil
}

// TransitionPlaytestStatus advances status through the PRD §5.1 strict
// linear state machine: DRAFT → OPEN → CLOSED. Any other transition
// (including same-state and backward) is FailedPrecondition.
func (s *PlaytesthubServiceServer) TransitionPlaytestStatus(ctx context.Context, req *pb.TransitionPlaytestStatusRequest) (*pb.TransitionPlaytestStatusResponse, error) {
	if _, err := requireActor(ctx); err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(req.GetPlaytestId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "playtest_id is not a uuid: %v", err)
	}
	target, err := statusEnumToString(req.GetTargetStatus())
	if err != nil {
		return nil, err
	}

	current, err := s.playtest.GetByID(ctx, s.namespace, id)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "fetching playtest: %v", err)
	}
	if current.DeletedAt != nil {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}
	if !isValidTransition(current.Status, target) {
		return nil, status.Errorf(codes.FailedPrecondition, "transition %s → %s is not allowed (PRD §5.1: DRAFT → OPEN → CLOSED only)", current.Status, target)
	}

	got, err := s.playtest.TransitionStatus(ctx, s.namespace, id, current.Status, target)
	if errors.Is(err, repo.ErrStatusCASMismatch) {
		return nil, status.Errorf(codes.FailedPrecondition, "transition %s → %s raced another writer, please retry", current.Status, target)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "transitioning playtest status: %v", err)
	}
	return &pb.TransitionPlaytestStatusResponse{Playtest: playtestToProto(got)}, nil
}

// AdminGetPlaytest returns the full playtest row by ID for admin view.
// Soft-deleted rows remain visible so admins can audit past playtests;
// this mirrors list-view policy not being relevant here (PRD §5.1).
func (s *PlaytesthubServiceServer) AdminGetPlaytest(ctx context.Context, req *pb.AdminGetPlaytestRequest) (*pb.AdminGetPlaytestResponse, error) {
	if _, err := requireActor(ctx); err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(req.GetPlaytestId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "playtest_id is not a uuid: %v", err)
	}
	got, err := s.playtest.GetByID(ctx, s.namespace, id)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "fetching playtest: %v", err)
	}
	return &pb.AdminGetPlaytestResponse{Playtest: playtestToProto(got)}, nil
}

// ListPlaytests returns every non-deleted playtest in the namespace.
// Unpaginated per PRD §6 Pagination; soft cap of 100 is enforced at
// Create time.
func (s *PlaytesthubServiceServer) ListPlaytests(ctx context.Context, req *pb.ListPlaytestsRequest) (*pb.ListPlaytestsResponse, error) {
	if _, err := requireActor(ctx); err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	rows, err := s.playtest.List(ctx, s.namespace, false)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "listing playtests: %v", err)
	}
	out := make([]*pb.Playtest, 0, len(rows))
	for _, r := range rows {
		out = append(out, playtestToProto(r))
	}
	return &pb.ListPlaytestsResponse{Playtests: out}, nil
}

// GetPublicPlaytest returns the unauthenticated field subset. DRAFT,
// CLOSED, and soft-deleted rows are indistinguishable from missing per
// PRD §5.1 visibility — all return NotFound.
func (s *PlaytesthubServiceServer) GetPublicPlaytest(ctx context.Context, req *pb.GetPublicPlaytestRequest) (*pb.GetPublicPlaytestResponse, error) {
	got, err := s.playtest.GetBySlug(ctx, s.namespace, req.GetSlug())
	if errors.Is(err, repo.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "fetching playtest: %v", err)
	}
	if got.DeletedAt != nil || got.Status != statusOpen {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}
	return &pb.GetPublicPlaytestResponse{Playtest: playtestToPublic(got)}, nil
}

// GetPlaytestForPlayer is the authenticated player view. CLOSED is
// visible only to an applicant who has already been approved; DRAFT and
// soft-deleted are always NotFound (PRD §5.1 visibility).
func (s *PlaytesthubServiceServer) GetPlaytestForPlayer(ctx context.Context, req *pb.GetPlaytestForPlayerRequest) (*pb.GetPlaytestForPlayerResponse, error) {
	userID, err := requireActor(ctx)
	if err != nil {
		return nil, err
	}
	got, err := s.playtest.GetBySlug(ctx, s.namespace, req.GetSlug())
	if errors.Is(err, repo.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "fetching playtest: %v", err)
	}
	if got.DeletedAt != nil || got.Status == statusDraft {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}
	if got.Status == statusClosed {
		approved, err := s.isApprovedApplicant(ctx, got.ID, userID)
		if err != nil {
			return nil, err
		}
		if !approved {
			return nil, status.Error(codes.NotFound, "playtest not found")
		}
	}
	return &pb.GetPlaytestForPlayerResponse{Playtest: playtestToPlayer(got)}, nil
}

// isApprovedApplicant checks whether the caller has an APPROVED applicant
// row for the given playtest — the CLOSED-visibility gate for players.
func (s *PlaytesthubServiceServer) isApprovedApplicant(ctx context.Context, playtestID, userID uuid.UUID) (bool, error) {
	got, err := s.applicant.GetByPlaytestUser(ctx, playtestID, userID)
	if errors.Is(err, repo.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, status.Errorf(codes.Internal, "fetching applicant: %v", err)
	}
	return got.Status == applicantStatusApproved, nil
}

// checkNamespace enforces PRD §5.1: the path-param namespace must match
// the server's configured AGS_NAMESPACE. A mismatch is surfaced as
// PermissionDenied rather than NotFound to avoid leaking existence of
// other-namespace playtests.
func (s *PlaytesthubServiceServer) checkNamespace(ns string) error {
	if ns != s.namespace {
		return status.Errorf(codes.PermissionDenied, "namespace %q is not served by this instance", ns)
	}
	return nil
}

// statusEnumToString converts the proto target enum into the TEXT value
// stored on disk. UNSPECIFIED is rejected — callers must name a target.
func statusEnumToString(s pb.PlaytestStatus) (string, error) {
	switch s {
	case pb.PlaytestStatus_PLAYTEST_STATUS_DRAFT:
		return statusDraft, nil
	case pb.PlaytestStatus_PLAYTEST_STATUS_OPEN:
		return statusOpen, nil
	case pb.PlaytestStatus_PLAYTEST_STATUS_CLOSED:
		return statusClosed, nil
	}
	return "", status.Errorf(codes.InvalidArgument, "target_status %q is unspecified or unknown", s.String())
}

// isValidTransition encodes PRD §5.1: DRAFT → OPEN → CLOSED, one step,
// forward only. Same-state is not a valid transition.
func isValidTransition(from, to string) bool {
	switch from {
	case statusDraft:
		return to == statusOpen
	case statusOpen:
		return to == statusClosed
	}
	return false
}

// ---- proto marshallers -----------------------------------------------------

func playtestToProto(p *repo.Playtest) *pb.Playtest {
	out := &pb.Playtest{
		Id:                    p.ID.String(),
		Namespace:             p.Namespace,
		Slug:                  p.Slug,
		Title:                 p.Title,
		Description:           p.Description,
		BannerImageUrl:        p.BannerImageURL,
		Platforms:             stringsToPlatforms(p.Platforms),
		StartsAt:              timeToTimestamp(p.StartsAt),
		EndsAt:                timeToTimestamp(p.EndsAt),
		Status:                statusStringToEnum(p.Status),
		NdaRequired:           p.NDARequired,
		NdaText:               p.NDAText,
		CurrentNdaVersionHash: p.CurrentNDAVersionHash,
		DistributionModel:     distModelStringToEnum(p.DistributionModel),
		CreatedAt:             timestamppb.New(p.CreatedAt),
		UpdatedAt:             timestamppb.New(p.UpdatedAt),
	}
	if p.SurveyID != nil {
		v := p.SurveyID.String()
		out.SurveyId = &v
	}
	if p.AGSItemID != nil {
		v := *p.AGSItemID
		out.AgsItemId = &v
	}
	if p.AGSCampaignID != nil {
		v := *p.AGSCampaignID
		out.AgsCampaignId = &v
	}
	if p.InitialCodeQuantity != nil {
		v := *p.InitialCodeQuantity
		out.InitialCodeQuantity = &v
	}
	if p.DeletedAt != nil {
		out.DeletedAt = timestamppb.New(*p.DeletedAt)
	}
	return out
}

func playtestToPublic(p *repo.Playtest) *pb.PublicPlaytest {
	return &pb.PublicPlaytest{
		Slug:           p.Slug,
		Title:          p.Title,
		Description:    p.Description,
		BannerImageUrl: p.BannerImageURL,
		Platforms:      stringsToPlatforms(p.Platforms),
		StartsAt:       timeToTimestamp(p.StartsAt),
		EndsAt:         timeToTimestamp(p.EndsAt),
	}
}

func playtestToPlayer(p *repo.Playtest) *pb.PlayerPlaytest {
	out := &pb.PlayerPlaytest{
		Slug:                  p.Slug,
		Title:                 p.Title,
		Description:           p.Description,
		BannerImageUrl:        p.BannerImageURL,
		Platforms:             stringsToPlatforms(p.Platforms),
		StartsAt:              timeToTimestamp(p.StartsAt),
		EndsAt:                timeToTimestamp(p.EndsAt),
		Status:                statusStringToEnum(p.Status),
		NdaRequired:           p.NDARequired,
		NdaText:               p.NDAText,
		CurrentNdaVersionHash: p.CurrentNDAVersionHash,
		DistributionModel:     distModelStringToEnum(p.DistributionModel),
	}
	if p.SurveyID != nil {
		v := p.SurveyID.String()
		out.SurveyId = &v
	}
	return out
}

func statusStringToEnum(s string) pb.PlaytestStatus {
	switch s {
	case statusDraft:
		return pb.PlaytestStatus_PLAYTEST_STATUS_DRAFT
	case statusOpen:
		return pb.PlaytestStatus_PLAYTEST_STATUS_OPEN
	case statusClosed:
		return pb.PlaytestStatus_PLAYTEST_STATUS_CLOSED
	}
	return pb.PlaytestStatus_PLAYTEST_STATUS_UNSPECIFIED
}

func distModelStringToEnum(s string) pb.DistributionModel {
	switch s {
	case distModelSteamKeys:
		return pb.DistributionModel_DISTRIBUTION_MODEL_STEAM_KEYS
	case distModelAGSCampaign:
		return pb.DistributionModel_DISTRIBUTION_MODEL_AGS_CAMPAIGN
	}
	return pb.DistributionModel_DISTRIBUTION_MODEL_UNSPECIFIED
}

func timestampToTime(ts *timestamppb.Timestamp) *time.Time {
	if ts == nil || !ts.IsValid() {
		return nil
	}
	t := ts.AsTime()
	return &t
}

func timeToTimestamp(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return timestamppb.New(*t)
}
