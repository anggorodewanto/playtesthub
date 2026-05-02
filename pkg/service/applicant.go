package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/anggorodewanto/playtesthub/pkg/discord"
	iampkg "github.com/anggorodewanto/playtesthub/pkg/iam"
	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// WithDiscordLookup wires a bot-backed handle lookup. main.go calls it
// with a *discord.BotClient; tests inject fakes. Passing nil is valid —
// signup falls back to the raw Discord ID per PRD §10 M1.
func (s *PlaytesthubServiceServer) WithDiscordLookup(l discord.HandleLookup) *PlaytesthubServiceServer {
	s.discord = l
	return s
}

// Signup creates a PENDING applicant for the authenticated player on the
// referenced playtest. Idempotent on (playtestId, userId) per PRD §5.2:
// a re-post by the same user returns the existing applicant rather than
// erroring.
//
// Visibility gate mirrors GetPlaytestForPlayer: DRAFT and soft-deleted
// playtests are indistinguishable from non-existent; CLOSED is visible
// only to already-approved callers (who, by definition, cannot re-signup
// — the handler collapses to returning the existing row).
func (s *PlaytesthubServiceServer) Signup(ctx context.Context, req *pb.SignupRequest) (*pb.SignupResponse, error) {
	userID, err := requireActor(ctx)
	if err != nil {
		return nil, err
	}
	platforms, err := platformsToStrings(req.GetPlatforms())
	if err != nil {
		return nil, wrapPlatformsErr(err)
	}

	pt, err := s.playtest.GetBySlug(ctx, s.namespace, req.GetSlug())
	if errors.Is(err, repo.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "fetching playtest: %v", err)
	}
	if pt.DeletedAt != nil || pt.Status == statusDraft {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}

	if existing, hit, err := s.lookupExistingApplicant(ctx, pt.ID, userID); err != nil {
		return nil, err
	} else if hit {
		return &pb.SignupResponse{Applicant: playerApplicantToProto(existing)}, nil
	}

	if pt.Status == statusClosed {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}

	a := &repo.Applicant{
		PlaytestID:    pt.ID,
		UserID:        userID,
		DiscordHandle: s.resolveDiscordHandle(ctx, userID),
		Platforms:     platforms,
	}
	got, err := s.applicant.Insert(ctx, a)
	if errors.Is(err, repo.ErrUniqueViolation) {
		// Racing signup: another goroutine inserted between our lookup
		// and insert. Resolve idempotently by returning the winning row.
		existing, hit, lookupErr := s.lookupExistingApplicant(ctx, pt.ID, userID)
		if lookupErr != nil {
			return nil, lookupErr
		}
		if !hit {
			return nil, status.Error(codes.Internal, "unique violation but no existing applicant found")
		}
		return &pb.SignupResponse{Applicant: playerApplicantToProto(existing)}, nil
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "inserting applicant: %v", err)
	}
	return &pb.SignupResponse{Applicant: playerApplicantToProto(got)}, nil
}

// GetApplicantStatus returns the caller's own applicant row for a
// playtest, with the player-visible field subset (schema.md L88). Missing
// applicant → NotFound. Playtest visibility mirrors GetPlaytestForPlayer.
func (s *PlaytesthubServiceServer) GetApplicantStatus(ctx context.Context, req *pb.GetApplicantStatusRequest) (*pb.GetApplicantStatusResponse, error) {
	userID, err := requireActor(ctx)
	if err != nil {
		return nil, err
	}

	pt, err := s.playtest.GetBySlug(ctx, s.namespace, req.GetSlug())
	if errors.Is(err, repo.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "fetching playtest: %v", err)
	}
	if pt.DeletedAt != nil || pt.Status == statusDraft {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}

	existing, hit, err := s.lookupExistingApplicant(ctx, pt.ID, userID)
	if err != nil {
		return nil, err
	}
	if !hit {
		return nil, status.Error(codes.NotFound, "applicant not found")
	}
	if pt.Status == statusClosed && existing.Status != applicantStatusApproved {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}
	return &pb.GetApplicantStatusResponse{Applicant: playerApplicantToProto(existing)}, nil
}

// AcceptNDA records a click-accept of the current NDA text on a
// playtest, scoped to the authenticated player. Idempotent on the
// natural key (userId, playtestId, ndaVersionHash) per PRD §4.7 — a
// second accept on the same tuple returns the existing row and writes
// no new audit. The hash always comes from the live playtest row, never
// from the client; that is what makes the post-edit re-accept flow
// (PRD §5.3) work without a client-side hash field on the request.
//
// Visibility mirrors GetApplicantStatus: DRAFT and soft-deleted hide as
// NotFound; CLOSED hides for non-approved callers (an already-approved
// player can re-accept after an NDA edit, since the granted code stays
// visible per PRD §5.3).
func (s *PlaytesthubServiceServer) AcceptNDA(ctx context.Context, req *pb.AcceptNDARequest) (*pb.AcceptNDAResponse, error) {
	userID, err := requireActor(ctx)
	if err != nil {
		return nil, err
	}
	if s.nda == nil {
		return nil, status.Error(codes.Internal, "nda store not wired")
	}
	playtestID, err := uuid.Parse(req.GetPlaytestId())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "playtest_id is not a uuid: %v", err)
	}

	pt, err := s.playtest.GetByID(ctx, s.namespace, playtestID)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "fetching playtest: %v", err)
	}
	if pt.DeletedAt != nil || pt.Status == statusDraft {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}
	if !pt.NDARequired {
		return nil, status.Error(codes.InvalidArgument, "playtest does not require an nda")
	}
	if pt.CurrentNDAVersionHash == "" {
		return nil, status.Error(codes.Internal, "playtest nda hash is empty; reopen and re-save the nda text")
	}

	applicant, hit, err := s.lookupExistingApplicant(ctx, pt.ID, userID)
	if err != nil {
		return nil, err
	}
	if !hit {
		return nil, status.Error(codes.FailedPrecondition, "must signup before accepting nda")
	}
	if pt.Status == statusClosed && applicant.Status != applicantStatusApproved {
		return nil, status.Error(codes.NotFound, "playtest not found")
	}

	acceptance, replay, err := s.nda.AcceptIdempotent(ctx, &repo.NDAAcceptance{
		UserID:         userID,
		PlaytestID:     pt.ID,
		NDAVersionHash: pt.CurrentNDAVersionHash,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "recording nda acceptance: %v", err)
	}

	// Stamp the latest accepted hash on the applicant so the §5.3
	// derived `NdaReacceptRequired` boolean (computed client-side as
	// `playtest.currentNdaVersionHash != applicant.ndaVersionHash`)
	// flips off after a successful accept. Idempotent on the same hash.
	if applicant.NDAVersionHash == nil || *applicant.NDAVersionHash != pt.CurrentNDAVersionHash {
		if _, err := s.applicant.SetNDAVersionHash(ctx, applicant.ID, pt.CurrentNDAVersionHash); err != nil {
			return nil, status.Errorf(codes.Internal, "stamping applicant nda hash: %v", err)
		}
	}

	// PRD §4.7 first-accept-only audit: replay never re-emits, so a
	// stuck client retrying does not pollute the audit timeline.
	if !replay && s.audit != nil {
		if auditErr := repo.AppendNDAAccept(ctx, s.audit, s.namespace, pt.ID, &userID, applicant.ID, pt.CurrentNDAVersionHash); auditErr != nil {
			return nil, status.Errorf(codes.Internal, "appending nda.accept audit: %v", auditErr)
		}
	}

	return &pb.AcceptNDAResponse{Acceptance: ndaAcceptanceToProto(acceptance)}, nil
}

func ndaAcceptanceToProto(a *repo.NDAAcceptance) *pb.NDAAcceptance {
	return &pb.NDAAcceptance{
		UserId:         a.UserID.String(),
		PlaytestId:     a.PlaytestID.String(),
		NdaVersionHash: a.NDAVersionHash,
		AcceptedAt:     timestamppb.New(a.AcceptedAt),
	}
}

// lookupExistingApplicant resolves a (playtestID, userID) pair to an
// applicant row, returning (row, true, nil) on hit, (nil, false, nil) on
// miss, or (nil, false, err) on an unexpected repo failure already
// wrapped as a gRPC status.
func (s *PlaytesthubServiceServer) lookupExistingApplicant(ctx context.Context, playtestID, userID uuid.UUID) (*repo.Applicant, bool, error) {
	got, err := s.applicant.GetByPlaytestUser(ctx, playtestID, userID)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, status.Errorf(codes.Internal, "fetching applicant: %v", err)
	}
	return got, true, nil
}

// resolveDiscordHandle calls the bot-token lookup with the Discord
// snowflake from the JWT claims. On any failure — no Discord ID in
// context, bot client not configured, Discord API error — falls back to
// the raw Discord ID (or the AGS user id if no Discord ID is available).
// PRD §10 M1: "On failure (404 / 5xx / network error): signup proceeds
// with raw Discord user ID stored as `discordHandle`. Fetched once at
// signup, never refreshed."
func (s *PlaytesthubServiceServer) resolveDiscordHandle(ctx context.Context, userID uuid.UUID) string {
	discordID, _ := iampkg.DiscordIDFromContext(ctx)
	fallback := discordID
	if fallback == "" {
		fallback = userID.String()
	}
	if s.discord == nil || discordID == "" {
		return fallback
	}
	handle, err := s.discord.LookupHandle(ctx, discordID)
	if err != nil {
		slog.InfoContext(ctx, "discord handle lookup failed; using raw id",
			"discordId", discordID, "error", err.Error())
		return fallback
	}
	return handle
}

// playerApplicantToProto renders the player-visible field subset of an
// applicant row (schema.md L88). Admin-only fields (discordHandle,
// platforms, rejectionReason, DM state) are deliberately not populated.
// grantedCodeId is surfaced only as presence — the raw uuid is opaque to
// the player, whose flow reads the value via GetGrantedCode (M2).
func playerApplicantToProto(a *repo.Applicant) *pb.Applicant {
	out := &pb.Applicant{
		Id:         a.ID.String(),
		PlaytestId: a.PlaytestID.String(),
		UserId:     a.UserID.String(),
		Status:     applicantStatusStringToEnum(a.Status),
		CreatedAt:  timestamppb.New(a.CreatedAt),
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
	return out
}

func applicantStatusStringToEnum(s string) pb.ApplicantStatus {
	switch s {
	case applicantStatusPending:
		return pb.ApplicantStatus_APPLICANT_STATUS_PENDING
	case applicantStatusApproved:
		return pb.ApplicantStatus_APPLICANT_STATUS_APPROVED
	case applicantStatusRejected:
		return pb.ApplicantStatus_APPLICANT_STATUS_REJECTED
	}
	return pb.ApplicantStatus_APPLICANT_STATUS_UNSPECIFIED
}
