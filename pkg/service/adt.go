package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/anggorodewanto/playtesthub/pkg/adt"
	"github.com/anggorodewanto/playtesthub/pkg/agsid"
	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// StudioNamespaceResolver returns the playtesthub backend's studio
// identity for ADT-linkage purposes. The studio is derived from the
// claims on the *backend's* service IAM JWT (`union_namespace ??
// namespace`) — NOT the calling admin's request token — because every
// downstream ADT API call carries the backend service JWT and ADT
// records its linkage flag keyed on what that token represents
// (PRD §4.8.2). Returns an error when neither claim is set on the
// service token; service handlers map that to FailedPrecondition per
// errors.md.
type StudioNamespaceResolver func(ctx context.Context) (string, error)

// ADTLinkConfig wires StartADTLink with the runtime knobs declared in
// PRD §5.9: the ADT base URL the redirect targets, the admin-UI base
// URL the callback returns to, and the TTL of the adt_link_pending row.
// Zero PendingTTL falls back to 600s to match the documented default.
type ADTLinkConfig struct {
	ADTBaseURL        string
	RedirectBaseURL   string
	PendingTTLSeconds int
}

// WithADTLinkageStore attaches the adt_linkage / adt_link_pending
// repository required by the ADT linkage RPCs (PRD §4.8 / M5.B).
// Optional — handlers surface Internal when called without one wired.
func (s *PlaytesthubServiceServer) WithADTLinkageStore(store repo.ADTLinkageStore) *PlaytesthubServiceServer {
	s.adtLinkage = store
	return s
}

// WithADTClient attaches the ADT API client (in-memory by default per
// STATUS_M5.md B3). Used by ListADTBuilds today; ApproveApplicant ADT
// branch consumes it once B6 lands.
func (s *PlaytesthubServiceServer) WithADTClient(c adt.Client) *PlaytesthubServiceServer {
	s.adtClient = c
	return s
}

// WithADTLinkConfig attaches the linking-flow URL + TTL knobs.
func (s *PlaytesthubServiceServer) WithADTLinkConfig(cfg ADTLinkConfig) *PlaytesthubServiceServer {
	s.adtLinkConfig = cfg
	return s
}

// ADTDiagnostics is the snapshot the bootapp hands to the service so
// GetADTClientDiagnostics can report which adt.Client kind was wired
// and which env vars fed the gate. Values are presence booleans only —
// the AGS IAM client id + secret are NEVER held here as plaintext.
type ADTDiagnostics struct {
	// ClientKind is "http" when adt.NewHTTPClient was wired and "mem"
	// when adt.NewMemClient was wired. Empty when the diagnostic was
	// not provided (treated as "unknown" by the RPC).
	ClientKind            string
	AuthEnabled           bool
	ADTBaseURLSet         bool
	AGSBaseURLSet         bool
	AGSIAMClientIDSet     bool
	AGSIAMClientSecretSet bool
}

// WithADTDiagnostics records the bootapp's gate decision so
// GetADTClientDiagnostics can surface it without re-deriving from the
// runtime adtClient interface. Wired in bootapp.New right after the
// gate evaluation; tests that exercise the RPC directly call it too.
func (s *PlaytesthubServiceServer) WithADTDiagnostics(d ADTDiagnostics) *PlaytesthubServiceServer {
	s.adtDiagnostics = d
	return s
}

// WithStudioNamespaceResolver attaches the resolver that returns the
// backend's studio identity from its service IAM JWT. The ADT linkage
// handlers call it to scope every linkage row + every ADT API call.
func (s *PlaytesthubServiceServer) WithStudioNamespaceResolver(r StudioNamespaceResolver) *PlaytesthubServiceServer {
	s.studioNamespace = r
	return s
}

// resolveStudioNamespace runs the configured resolver. Empty result or
// error → FailedPrecondition per errors.md StartADTLink rows. Handlers
// that need the studio identity for read-only scoping (ListADTLinkages,
// UnlinkADT, ListADTBuilds) reuse the same path so the precondition
// surface is uniform.
func (s *PlaytesthubServiceServer) resolveStudioNamespace(ctx context.Context) (string, error) {
	if s.studioNamespace == nil {
		return "", status.Error(codes.FailedPrecondition, "ADT linkage is not configured on this backend (studio namespace resolver missing)")
	}
	studio, err := s.studioNamespace(ctx)
	if err != nil {
		return "", status.Errorf(codes.FailedPrecondition, "resolving studio namespace from backend service token: %v", err)
	}
	if studio == "" {
		return "", status.Error(codes.FailedPrecondition, "backend service token carries neither union_namespace nor namespace claim; ADT linkage cannot be scoped to a studio")
	}
	return studio, nil
}

// requireADTLinkageStore returns the configured store or Internal when
// the handler ran without one wired. Centralised so every ADT RPC
// surfaces the same misconfiguration message.
func (s *PlaytesthubServiceServer) requireADTLinkageStore() (repo.ADTLinkageStore, error) {
	if s.adtLinkage == nil {
		return nil, status.Error(codes.Internal, "ADT linkage store not configured")
	}
	return s.adtLinkage, nil
}

// ListADTLinkages returns every live linkage row for the caller's
// studio. PRD §4.8 / STATUS_M5.md B4. Identity columns only — no
// credential payload exists.
func (s *PlaytesthubServiceServer) ListADTLinkages(ctx context.Context, req *pb.ListADTLinkagesRequest) (*pb.ListADTLinkagesResponse, error) {
	if _, err := requireActor(ctx); err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	store, err := s.requireADTLinkageStore()
	if err != nil {
		return nil, err
	}
	studio, err := s.resolveStudioNamespace(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := store.ListLive(ctx, studio)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "listing adt_linkages: %v", err)
	}
	out := make([]*pb.ADTLinkage, 0, len(rows))
	for _, r := range rows {
		out = append(out, adtLinkageToProto(r))
	}
	return &pb.ListADTLinkagesResponse{Linkages: out}, nil
}

// StartADTLink mints the CSRF state nonce, persists the pending row,
// and returns the linkUrl the admin UI redirects to. PRD §4.8.2.
func (s *PlaytesthubServiceServer) StartADTLink(ctx context.Context, req *pb.StartADTLinkRequest) (*pb.StartADTLinkResponse, error) {
	actor, err := requireActor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	if s.adtLinkConfig.ADTBaseURL == "" {
		ADTLinkFailures.WithLabelValues("start", "config_missing").Inc()
		return nil, status.Error(codes.FailedPrecondition, "ADT_BASE_URL is not configured on this backend; ADT linkage is disabled")
	}
	if s.adtLinkConfig.RedirectBaseURL == "" {
		ADTLinkFailures.WithLabelValues("start", "config_missing").Inc()
		return nil, status.Error(codes.FailedPrecondition, "ADT_REDIRECT_BASE_URL is not configured on this backend; ADT linkage is disabled")
	}
	store, err := s.requireADTLinkageStore()
	if err != nil {
		return nil, err
	}
	studio, err := s.resolveStudioNamespace(ctx)
	if err != nil {
		ADTLinkFailures.WithLabelValues("start", "studio_unresolved").Inc()
		return nil, err
	}

	state, err := mintState()
	if err != nil {
		ADTLinkFailures.WithLabelValues("start", "store_error").Inc()
		return nil, status.Errorf(codes.Internal, "minting state nonce: %v", err)
	}
	ttl := s.adtLinkConfig.PendingTTLSeconds
	if ttl <= 0 {
		ttl = 600
	}
	expiresAt := time.Now().Add(time.Duration(ttl) * time.Second)
	pending := &repo.ADTLinkPending{
		State:           state,
		StudioNamespace: studio,
		StartedByUserID: actor,
		ExpiresAt:       expiresAt,
	}
	if err := store.InsertPending(ctx, pending); err != nil {
		ADTLinkFailures.WithLabelValues("start", "store_error").Inc()
		return nil, status.Errorf(codes.Internal, "persisting adt_link_pending: %v", err)
	}
	linkURL := buildADTLinkURL(s.adtLinkConfig.ADTBaseURL, s.adtLinkConfig.RedirectBaseURL, state, studio)
	return &pb.StartADTLinkResponse{
		LinkUrl:   linkURL,
		State:     state,
		ExpiresAt: timestamppb.New(expiresAt),
	}, nil
}

// CompleteADTLink validates the state nonce against adt_link_pending,
// inserts the adt_linkage identity row, and emits the audit row.
// PRD §4.8.2 / errors.md.
func (s *PlaytesthubServiceServer) CompleteADTLink(ctx context.Context, req *pb.CompleteADTLinkRequest) (*pb.CompleteADTLinkResponse, error) {
	if _, err := requireActor(ctx); err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	if req.GetState() == "" {
		ADTLinkFailures.WithLabelValues("complete", "state_invalid").Inc()
		return nil, status.Error(codes.InvalidArgument, "linking state is invalid or expired")
	}
	if req.GetAdtNamespace() == "" {
		ADTLinkFailures.WithLabelValues("complete", "adt_namespace_missing").Inc()
		return nil, status.Error(codes.InvalidArgument, "adt_namespace is required on the callback")
	}
	store, err := s.requireADTLinkageStore()
	if err != nil {
		return nil, err
	}
	pending, err := store.ConsumePending(ctx, req.GetState(), time.Now())
	if errors.Is(err, repo.ErrNotFound) {
		ADTLinkFailures.WithLabelValues("complete", "state_invalid").Inc()
		return nil, status.Error(codes.InvalidArgument, "linking state is invalid or expired")
	}
	if err != nil {
		ADTLinkFailures.WithLabelValues("complete", "store_error").Inc()
		return nil, status.Errorf(codes.Internal, "consuming adt_link_pending: %v", err)
	}

	row := &repo.ADTLinkage{
		StudioNamespace: pending.StudioNamespace,
		ADTNamespace:    req.GetAdtNamespace(),
		LinkedByUserID:  pending.StartedByUserID,
	}
	got, err := store.Insert(ctx, row)
	if errors.Is(err, repo.ErrUniqueViolation) {
		// A live linkage for (studio, adt) already exists — return it
		// so CompleteADTLink stays idempotent on operator re-link
		// attempts that overlap.
		existing, lookupErr := store.GetLive(ctx, pending.StudioNamespace, req.GetAdtNamespace())
		if lookupErr != nil {
			ADTLinkFailures.WithLabelValues("complete", "store_error").Inc()
			return nil, status.Errorf(codes.Internal, "loading existing adt_linkage: %v", lookupErr)
		}
		return &pb.CompleteADTLinkResponse{Linkage: adtLinkageToProto(existing)}, nil
	}
	if err != nil {
		ADTLinkFailures.WithLabelValues("complete", "store_error").Inc()
		return nil, status.Errorf(codes.Internal, "inserting adt_linkage: %v", err)
	}

	if s.audit != nil {
		if auditErr := repo.AppendADTLinkageCreate(ctx, s.audit, s.namespace, pending.StartedByUserID, got.ID, got.StudioNamespace, got.ADTNamespace); auditErr != nil {
			ADTLinkFailures.WithLabelValues("complete", "audit_failed").Inc()
			s.loggerOrDefault().Warn("audit append failed", "action", repo.ActionADTLinkageCreate, "err", auditErr)
		}
	}
	return &pb.CompleteADTLinkResponse{Linkage: adtLinkageToProto(got)}, nil
}

// UnlinkADT soft-deletes the local linkage row AND best-effort tells
// ADT to drop its side of the flag. PRD §4.8. Idempotent.
//
// Best-effort propagation: a failed ADT-side DELETE is logged + counted
// (ADTUnlinkADTSideFailures) but does NOT block the local soft-delete.
// Rationale: an ADT outage must never strand an operator who wants to
// drop their own row, and the 2026-05-21 orphan-flag bug proves the
// inverse — soft-deleting locally without propagating leaves the
// operator unable to re-link until the orphan flag is cleaned up
// out-of-band. ErrLinkageMissing from ADT is the desired post-state
// (flag already absent) so it's swallowed alongside the transient
// errors; the metric still fires so we can see "operator-driven
// unlinks that hit a noisy ADT" trends.
func (s *PlaytesthubServiceServer) UnlinkADT(ctx context.Context, req *pb.UnlinkADTRequest) (*pb.UnlinkADTResponse, error) {
	actor, err := requireActor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	id, err := parseReqUUID("adt_linkage_id", req.GetAdtLinkageId())
	if err != nil {
		return nil, err
	}
	store, err := s.requireADTLinkageStore()
	if err != nil {
		return nil, err
	}
	studio, err := s.resolveStudioNamespace(ctx)
	if err != nil {
		return nil, err
	}
	existing, err := store.GetByID(ctx, studio, id)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "loading adt_linkage: %v", err)
	}
	// Idempotent re-unlink against an already soft-deleted row: no-op
	// success, no audit row, no ADT call (schema.md §"adt_linkage.delete"
	// — only rows whose underlying SoftDelete affected a row emit the
	// audit event; mirror the same shape for the ADT-side call so a
	// no-op locally is a no-op everywhere).
	if existing.DeletedAt != nil {
		return &pb.UnlinkADTResponse{}, nil
	}
	if s.adtClient != nil {
		if adtErr := s.adtClient.DeleteLinkage(ctx, studio, existing.ADTNamespace); adtErr != nil {
			s.recordUnlinkADTSideFailure(existing.ADTNamespace, adtErr)
		}
	}
	if err := store.SoftDelete(ctx, studio, id); err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "")
		}
		return nil, status.Errorf(codes.Internal, "soft-deleting adt_linkage: %v", err)
	}
	if s.audit != nil {
		if auditErr := repo.AppendADTLinkageDelete(ctx, s.audit, s.namespace, actor, existing.ID, existing.StudioNamespace, existing.ADTNamespace); auditErr != nil {
			s.loggerOrDefault().Warn("audit append failed", "action", repo.ActionADTLinkageDelete, "err", auditErr)
		}
	}
	return &pb.UnlinkADTResponse{}, nil
}

// recordUnlinkADTSideFailure classifies the best-effort ADT-side
// DELETE error, logs at warn level (structured, no PII — adt_namespace
// is operator-supplied identifier; PRD §6 redacts NDA/survey/code, not
// linkage identifiers), and increments ADTUnlinkADTSideFailures.
//
// New reason labels per Bug 4 / 2026-05-21 probe:
//   - "unauthenticated"   — bearer broken (ADT errorCode=401)
//   - "permission_denied" — token valid, route perm missing (errorCode=20001)
//
// These join "linkage_missing", "transient", and "unknown" so SRE can
// see which class of failure is trending without diffing the existing
// labels.
func (s *PlaytesthubServiceServer) recordUnlinkADTSideFailure(adtNamespace string, err error) {
	reason := "unknown"
	switch {
	case adt.IsLinkageMissing(err):
		reason = "linkage_missing"
	case adt.IsUnauthenticated(err):
		reason = "unauthenticated"
	case adt.IsPermissionDenied(err):
		reason = "permission_denied"
	case adt.IsUnavailable(err), adt.IsRateLimited(err):
		reason = "transient"
	}
	ADTUnlinkADTSideFailures.WithLabelValues(reason).Inc()
	s.loggerOrDefault().Warn(
		"UnlinkADT: ADT-side DELETE failed; proceeding with local soft-delete",
		"action", repo.ActionADTLinkageDelete,
		"adt_namespace", adtNamespace,
		"reason", reason,
		"err", err,
	)
}

// RecoverADTLinkage is the operator-recovery surface for the 2026-05-21
// orphan-flag bug: an ADT-side linkage flag with no matching local row
// makes StartADTLink fail with 409 / already_linked, leaving the
// operator stuck. RecoverADTLinkage probes ADT to confirm the orphan
// flag (via ListGames — the cheapest call that exercises the same
// linkage-flag check ADT enforces on every other endpoint) and inserts
// the local row directly. No OAuth round-trip required.
//
// PRD §4.8 / errors.md rows for "adt linkage already exists for that
// namespace" + "no ADT-side linkage found for that namespace; use
// StartADTLink to create one".
func (s *PlaytesthubServiceServer) RecoverADTLinkage(ctx context.Context, req *pb.RecoverADTLinkageRequest) (*pb.RecoverADTLinkageResponse, error) {
	actor, err := requireActor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	if req.GetAdtNamespace() == "" {
		return nil, status.Error(codes.InvalidArgument, "adt_namespace is required")
	}
	store, err := s.requireADTLinkageStore()
	if err != nil {
		return nil, err
	}
	if s.adtClient == nil {
		return nil, status.Error(codes.Internal, "ADT client not configured")
	}
	studio, err := s.resolveStudioNamespace(ctx)
	if err != nil {
		return nil, err
	}

	existing, err := store.GetLive(ctx, studio, req.GetAdtNamespace())
	if err != nil && !errors.Is(err, repo.ErrNotFound) {
		return nil, status.Errorf(codes.Internal, "loading adt_linkage: %v", err)
	}
	if existing != nil {
		return nil, status.Error(codes.AlreadyExists, "adt linkage already exists for that namespace")
	}

	if _, err := s.adtClient.ListGames(ctx, studio, req.GetAdtNamespace()); err != nil {
		if errors.Is(err, adt.ErrLinkageMissing) {
			return nil, status.Error(codes.FailedPrecondition, "no ADT-side linkage found for that namespace; use StartADTLink to create one")
		}
		if errors.Is(err, adt.ErrUnauthenticated) {
			return nil, status.Error(codes.FailedPrecondition, "ADT rejected the backend service token; rotate AGS IAM client credentials and restart the backend")
		}
		if errors.Is(err, adt.ErrPermissionDenied) {
			return nil, status.Error(codes.FailedPrecondition, "backend service token lacks required ADT permission scope; ask ADT-eng to grant the missing permission")
		}
		return nil, status.Errorf(codes.Unavailable, "ADT temporarily unavailable while probing linkage: %v", err)
	}

	row := &repo.ADTLinkage{
		StudioNamespace: studio,
		ADTNamespace:    req.GetAdtNamespace(),
		LinkedByUserID:  actor,
	}
	got, err := store.Insert(ctx, row)
	if errors.Is(err, repo.ErrUniqueViolation) {
		// Race: another admin concurrently called Recover (or
		// CompleteADTLink finished the redirect dance) between our
		// GetLive and Insert. Surface AlreadyExists with the same
		// byte-exact message so the operator-recovery contract is
		// uniform regardless of which side won.
		return nil, status.Error(codes.AlreadyExists, "adt linkage already exists for that namespace")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "inserting adt_linkage: %v", err)
	}

	if s.audit != nil {
		if auditErr := repo.AppendADTLinkageRecover(ctx, s.audit, s.namespace, actor, got.ID, got.StudioNamespace, got.ADTNamespace); auditErr != nil {
			s.loggerOrDefault().Warn("audit append failed", "action", repo.ActionADTLinkageRecover, "err", auditErr)
		}
	}
	return &pb.RecoverADTLinkageResponse{Linkage: adtLinkageToProto(got)}, nil
}

// ListADTBuilds proxies adt.Client.ListBuilds for the linkage. The
// build picker on the create form uses this; CreatePlaytest's ADT
// branch (B5) calls the same path to verify adt_build_id belongs to
// the (adt_namespace, adt_game_id) pair.
func (s *PlaytesthubServiceServer) ListADTBuilds(ctx context.Context, req *pb.ListADTBuildsRequest) (*pb.ListADTBuildsResponse, error) {
	if _, err := requireActor(ctx); err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	if req.GetAdtGameId() == "" {
		return nil, status.Error(codes.InvalidArgument, "adt_game_id is required")
	}
	id, err := parseReqUUID("adt_linkage_id", req.GetAdtLinkageId())
	if err != nil {
		return nil, err
	}
	if s.adtClient == nil {
		return nil, status.Error(codes.Internal, "ADT client not configured")
	}
	store, err := s.requireADTLinkageStore()
	if err != nil {
		return nil, err
	}
	studio, err := s.resolveStudioNamespace(ctx)
	if err != nil {
		return nil, err
	}
	linkage, err := store.GetByID(ctx, studio, id)
	if errors.Is(err, repo.ErrNotFound) || (linkage != nil && linkage.DeletedAt != nil) {
		return nil, status.Error(codes.FailedPrecondition, "no ADT linkage matches this id for the caller's studio; link an ADT namespace first")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "loading adt_linkage: %v", err)
	}
	builds, err := s.adtClient.ListBuilds(ctx, studio, linkage.ADTNamespace, req.GetAdtGameId())
	if errors.Is(err, adt.ErrLinkageMissing) {
		return nil, status.Error(codes.FailedPrecondition, "adt linkage no longer exists or service token rejected, re-link required")
	}
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "calling ADT ListBuilds: %v", err)
	}
	out := make([]*pb.ADTBuild, 0, len(builds))
	for _, b := range builds {
		out = append(out, adtBuildToProto(b))
	}
	return &pb.ListADTBuildsResponse{Builds: out}, nil
}

// ListADTGames proxies adt.Client.ListGames for the linkage. Drives the
// admin create-playtest build-picker's top-level dropdown (STATUS_M5.md
// B12 + Addendum 2026-05-21) so operators no longer type adt_game_id by
// hand. Mirrors ListADTBuilds: linkage-id resolution → studio scope →
// adt.Client.ListGames → 401 → FailedPrecondition.
func (s *PlaytesthubServiceServer) ListADTGames(ctx context.Context, req *pb.ListADTGamesRequest) (*pb.ListADTGamesResponse, error) {
	if _, err := requireActor(ctx); err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	id, err := parseReqUUID("adt_linkage_id", req.GetAdtLinkageId())
	if err != nil {
		return nil, err
	}
	if s.adtClient == nil {
		return nil, status.Error(codes.Internal, "ADT client not configured")
	}
	store, err := s.requireADTLinkageStore()
	if err != nil {
		return nil, err
	}
	studio, err := s.resolveStudioNamespace(ctx)
	if err != nil {
		return nil, err
	}
	linkage, err := store.GetByID(ctx, studio, id)
	if errors.Is(err, repo.ErrNotFound) || (linkage != nil && linkage.DeletedAt != nil) {
		return nil, status.Error(codes.FailedPrecondition, "no ADT linkage matches this id for the caller's studio; link an ADT namespace first")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "loading adt_linkage: %v", err)
	}
	games, err := s.adtClient.ListGames(ctx, studio, linkage.ADTNamespace)
	if errors.Is(err, adt.ErrLinkageMissing) {
		return nil, status.Error(codes.FailedPrecondition, "adt linkage no longer exists or service token rejected, re-link required")
	}
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "calling ADT ListGames: %v", err)
	}
	out := make([]*pb.ADTGame, 0, len(games))
	for _, g := range games {
		out = append(out, adtGameToProto(g))
	}
	return &pb.ListADTGamesResponse{Games: out}, nil
}

// ChangeADTBuild repoints an ADT playtest at a different (game, build)
// pair under its existing linked ADT namespace. adt_namespace is NOT
// mutable here — it is the studio-linkage scope; re-pointing it is a
// relink, not a build change (PRD §4.8 / §5.1). The new pair is verified
// against the linkage via verifyADTBuild (the same ADT round-trip
// CreatePlaytest's ADT branch uses) before the row is persisted.
// Already-approved applicants keep the download URL already DM'd; future
// approvals + RetryDM re-mint against the new build (PRD §4.8.3).
func (s *PlaytesthubServiceServer) ChangeADTBuild(ctx context.Context, req *pb.ChangeADTBuildRequest) (*pb.ChangeADTBuildResponse, error) {
	actor, err := requireActor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	id, err := parseReqUUID("playtest_id", req.GetPlaytestId())
	if err != nil {
		return nil, err
	}
	if req.GetAdtGameId() == "" || req.GetAdtBuildId() == "" {
		return nil, status.Error(codes.InvalidArgument, "adt_game_id and adt_build_id are required")
	}

	current, err := s.playtest.GetByID(ctx, s.namespace, id)
	if e := mapPlaytestLookupErr(err, playtestSoftDelete(current), "fetching playtest"); e != nil {
		return nil, e
	}
	if current.DistributionModel != distModelADT {
		return nil, status.Error(codes.FailedPrecondition, "playtest does not use ADT distribution; build can only be changed on ADT playtests")
	}
	if current.ADTNamespace == nil || *current.ADTNamespace == "" {
		return nil, status.Error(codes.FailedPrecondition, "playtest has no linked ADT namespace; re-create the playtest")
	}

	if err := s.verifyADTBuild(ctx, *current.ADTNamespace, req.GetAdtGameId(), req.GetAdtBuildId()); err != nil {
		return nil, err
	}

	var beforeGameID, beforeBuildID string
	if current.ADTGameID != nil {
		beforeGameID = *current.ADTGameID
	}
	if current.ADTBuildID != nil {
		beforeBuildID = *current.ADTBuildID
	}
	newGame := req.GetAdtGameId()
	newBuild := req.GetAdtBuildId()
	current.ADTGameID = &newGame
	current.ADTBuildID = &newBuild

	got, err := s.playtest.Update(ctx, current)
	if e := mapPlaytestLookupErr(err, nil, "updating playtest"); e != nil {
		return nil, e
	}

	if s.audit != nil {
		if auditErr := repo.AppendPlaytestADTBuildChange(ctx, s.audit, s.namespace, actor, got.ID, *current.ADTNamespace, beforeGameID, beforeBuildID, newGame, newBuild); auditErr != nil {
			s.loggerOrDefault().Warn("audit append failed", "action", repo.ActionPlaytestADTBuildChange, "err", auditErr)
		}
	}
	return &pb.ChangeADTBuildResponse{Playtest: playtestToProto(got)}, nil
}

// GetADTClientDiagnostics returns the snapshot the bootapp recorded
// when it picked between adt.NewHTTPClient and adt.NewMemClient.
// Booleans only — the secret-bearing env vars are reported as presence
// flags, never as values. Admin-only (auth same as UnlinkADT).
func (s *PlaytesthubServiceServer) GetADTClientDiagnostics(ctx context.Context, req *pb.GetADTClientDiagnosticsRequest) (*pb.GetADTClientDiagnosticsResponse, error) {
	if _, err := requireActor(ctx); err != nil {
		return nil, err
	}
	if err := s.checkNamespace(req.GetNamespace()); err != nil {
		return nil, err
	}
	d := s.adtDiagnostics
	return &pb.GetADTClientDiagnosticsResponse{
		AdtClientKind:         d.ClientKind,
		AuthEnabled:           d.AuthEnabled,
		AdtBaseUrlSet:         d.ADTBaseURLSet,
		AgsBaseUrlSet:         d.AGSBaseURLSet,
		AgsIamClientIdSet:     d.AGSIAMClientIDSet,
		AgsIamClientSecretSet: d.AGSIAMClientSecretSet,
	}, nil
}

func adtGameToProto(g adt.Game) *pb.ADTGame {
	out := &pb.ADTGame{
		Id:   g.ID,
		Name: g.Name,
	}
	if !g.CreatedAt.IsZero() {
		out.CreatedAt = timestamppb.New(g.CreatedAt)
	}
	return out
}

func adtLinkageToProto(r *repo.ADTLinkage) *pb.ADTLinkage {
	out := &pb.ADTLinkage{
		Id:              r.ID.String(),
		StudioNamespace: r.StudioNamespace,
		AdtNamespace:    r.ADTNamespace,
		LinkedByUserId:  agsid.Format(r.LinkedByUserID),
		LinkedAt:        timestamppb.New(r.LinkedAt),
	}
	if r.DeletedAt != nil {
		out.DeletedAt = timestamppb.New(*r.DeletedAt)
	}
	return out
}

func adtBuildToProto(b adt.Build) *pb.ADTBuild {
	out := &pb.ADTBuild{
		Id:       b.ID,
		Name:     b.Name,
		Version:  b.Version,
		Platform: b.Platform,
	}
	if !b.UploadedAt.IsZero() {
		out.UploadedAt = timestamppb.New(b.UploadedAt)
	}
	return out
}

// mintState returns a 32-byte URL-safe base64 nonce. The encoded form
// is the literal `state` string carried on the link redirect; ADT
// round-trips it verbatim.
func mintState() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("rand.Read: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// buildADTLinkURL composes the full URL the admin UI assigns to
// window.location.href. studio_namespace is mandatory per D2 — ADT
// keys its side's linkage flag on (adt_namespace, studio_namespace).
func buildADTLinkURL(adtBase, redirectBase, state, studio string) string {
	u, err := url.Parse(adtBase)
	if err != nil {
		// adtBase is operator-supplied via ADT_BASE_URL; an invalid
		// value would have surfaced at boot-time validation. Fall back
		// to a string-template form so we still return *something* the
		// operator can inspect.
		return fmt.Sprintf("%s/oauth/link?state=%s&redirect_uri=%s/adt-link-callback&studio_namespace=%s",
			adtBase, url.QueryEscape(state), url.QueryEscape(redirectBase), url.QueryEscape(studio))
	}
	u.Path = joinURLPath(u.Path, "/oauth/link")
	q := u.Query()
	q.Set("state", state)
	q.Set("redirect_uri", redirectBase+"/adt-link-callback")
	q.Set("studio_namespace", studio)
	u.RawQuery = q.Encode()
	return u.String()
}

func joinURLPath(prefix, suffix string) string {
	if prefix == "" || prefix == "/" {
		return suffix
	}
	if prefix[len(prefix)-1] == '/' {
		prefix = prefix[:len(prefix)-1]
	}
	if suffix == "" {
		return prefix
	}
	if suffix[0] != '/' {
		suffix = "/" + suffix
	}
	return prefix + suffix
}
