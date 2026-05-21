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
		return nil, status.Error(codes.FailedPrecondition, "ADT_BASE_URL is not configured on this backend; ADT linkage is disabled")
	}
	if s.adtLinkConfig.RedirectBaseURL == "" {
		return nil, status.Error(codes.FailedPrecondition, "ADT_REDIRECT_BASE_URL is not configured on this backend; ADT linkage is disabled")
	}
	store, err := s.requireADTLinkageStore()
	if err != nil {
		return nil, err
	}
	studio, err := s.resolveStudioNamespace(ctx)
	if err != nil {
		return nil, err
	}

	state, err := mintState()
	if err != nil {
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
		return nil, status.Error(codes.InvalidArgument, "linking state is invalid or expired")
	}
	if req.GetAdtNamespace() == "" {
		return nil, status.Error(codes.InvalidArgument, "adt_namespace is required on the callback")
	}
	store, err := s.requireADTLinkageStore()
	if err != nil {
		return nil, err
	}
	pending, err := store.ConsumePending(ctx, req.GetState(), time.Now())
	if errors.Is(err, repo.ErrNotFound) {
		return nil, status.Error(codes.InvalidArgument, "linking state is invalid or expired")
	}
	if err != nil {
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
			return nil, status.Errorf(codes.Internal, "loading existing adt_linkage: %v", lookupErr)
		}
		return &pb.CompleteADTLinkResponse{Linkage: adtLinkageToProto(existing)}, nil
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "inserting adt_linkage: %v", err)
	}

	if s.audit != nil {
		if auditErr := repo.AppendADTLinkageCreate(ctx, s.audit, s.namespace, pending.StartedByUserID, got.ID, got.StudioNamespace, got.ADTNamespace); auditErr != nil {
			s.loggerOrDefault().Warn("audit append failed", "action", repo.ActionADTLinkageCreate, "err", auditErr)
		}
	}
	return &pb.CompleteADTLinkResponse{Linkage: adtLinkageToProto(got)}, nil
}

// UnlinkADT soft-deletes the linkage row. PRD §4.8. Idempotent.
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
	// success, no audit row (schema.md §"adt_linkage.delete" — only
	// rows whose underlying SoftDelete affected a row emit the audit
	// event).
	if existing.DeletedAt != nil {
		return &pb.UnlinkADTResponse{}, nil
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
