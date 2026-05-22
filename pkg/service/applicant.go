package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/anggorodewanto/playtesthub/pkg/agsid"
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

// WithPlatformLookup wires the AGS IAM platform-link lookup signup uses
// to fetch the caller's Discord snowflake. AGS does not include the
// snowflake in the JWT (only `ipf=discord`), so this lookup is the
// source of truth for `applicant.discord_user_id`. Passing nil leaves
// signup falling back to the UUID — matches local/dev where AGS is
// unreachable.
func (s *PlaytesthubServiceServer) WithPlatformLookup(p iampkg.PlatformLookup) *PlaytesthubServiceServer {
	s.platformLookup = p
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
	if e := mapPlaytestLookupErr(err, playtestSoftDelete(pt), "fetching playtest"); e != nil {
		return nil, e
	}
	if pt.Status == statusDraft {
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

	ctx = s.resolveDiscordSnowflake(ctx, userID)

	a := &repo.Applicant{
		PlaytestID:    pt.ID,
		UserID:        userID,
		DiscordHandle: s.resolveDiscordHandle(ctx, userID),
		DiscordUserID: discordSnowflakePtr(ctx),
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

	// PRD §5.4 / M5.A auto-approve: chain into the existing M2 approve
	// path under a playtest-scoped advisory lock so concurrent signups
	// cannot over-approve past the cap. Best-effort — any failure leaves
	// the applicant at PENDING and signup still returns success.
	if pt.AutoApprove && pt.AutoApproveLimit != nil {
		if promoted := s.tryAutoApprove(ctx, pt, got); promoted != nil {
			got = promoted
		}
	}

	return &pb.SignupResponse{Applicant: playerApplicantToProto(got)}, nil
}

// tryAutoApprove runs the M5.A signup auto-approve chain. Returns the
// promoted (APPROVED) applicant on success, or nil to leave the caller
// with the PENDING row from Insert. Any failure inside the chain —
// pool empty, CAS race, DB error, missing wiring — is swallowed and
// surfaces as PENDING-fallback per PRD §5.4: signup itself is never
// failed by the auto-approve attempt.
func (s *PlaytesthubServiceServer) tryAutoApprove(ctx context.Context, pt *repo.Playtest, a *repo.Applicant) *repo.Applicant {
	if s.txRunner == nil || pt.AutoApproveLimit == nil {
		return nil
	}
	// STEAM_KEYS / AGS_CAMPAIGN auto-approve still needs the code store
	// wired; ADT skips it entirely (no code pool — PRD §5.5).
	if pt.DistributionModel != distModelADT && s.code == nil {
		return nil
	}
	updated, grantedCodeID, txErr := s.runAutoApproveTx(ctx, pt, a)
	if txErr != nil {
		// PRD §5.4: silent PENDING fallback — log at info so operators
		// can see auto-approve attempts that hit the cap or the pool
		// without polluting the audit timeline.
		slog.InfoContext(ctx, "auto-approve skipped, applicant stays PENDING",
			"playtestId", pt.ID.String(),
			"applicantId", a.ID.String(),
			"reason", txErr.Error())
		return nil
	}
	if updated == nil {
		return nil
	}
	if s.audit != nil {
		if auditErr := repo.AppendApplicantAutoApproved(ctx, s.audit, s.namespace, pt.ID, updated.ID, grantedCodeID, *updated.ApprovedAt); auditErr != nil {
			slog.WarnContext(ctx, "appending applicant.auto_approved audit failed",
				"playtestId", pt.ID.String(),
				"applicantId", updated.ID.String(),
				"error", auditErr.Error())
		}
	}
	s.enqueueAutoApproveDM(ctx, pt, updated)
	return updated
}

// runAutoApproveTx executes the lock + count + (ADT no-code OR pool
// reserve/finalize) + CAS pipeline inside one tx. Returns the updated
// applicant, the granted code id (zero for ADT), and the tx error.
func (s *PlaytesthubServiceServer) runAutoApproveTx(ctx context.Context, pt *repo.Playtest, a *repo.Applicant) (*repo.Applicant, uuid.UUID, error) {
	limit := int(*pt.AutoApproveLimit)
	var (
		updated       *repo.Applicant
		grantedCodeID uuid.UUID
	)
	// Mirror UploadAtomic's per-playtest advisory key pattern. The
	// `autoapprove:` prefix keeps the lock space distinct from
	// upload/topup locks on the same playtest id so they do not
	// serialise against each other.
	lockKey := "autoapprove:" + pt.ID.String()
	txErr := s.txRunner.InTx(ctx, func(q repo.Querier) error {
		// Real PgTxRunner passes a live pgx.Tx; in-memory unit tests pass
		// nil and don't need a real lock — concurrency is exercised by
		// pkg/service/auto_approve_concurrency_test.go against a live DB.
		if q != nil {
			if _, lockErr := q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, lockKey); lockErr != nil {
				return lockErr
			}
		}
		count, countErr := s.applicant.CountAutoApprovedByPlaytest(ctx, q, pt.ID)
		if countErr != nil {
			return countErr
		}
		if count >= limit {
			return errAutoApproveCapHit
		}
		if pt.DistributionModel == distModelADT {
			upd, casErr := s.applicant.ApproveCASNoCode(ctx, q, a.ID, time.Now().UTC(), true)
			if casErr != nil {
				return casErr
			}
			updated = upd
			return nil
		}
		code, reserveErr := s.code.Reserve(ctx, q, pt.ID, a.UserID)
		if reserveErr != nil {
			return reserveErr
		}
		rows, finErr := s.code.FencedFinalize(ctx, q, code.ID, a.UserID, *code.ReservedAt)
		if finErr != nil {
			return finErr
		}
		if rows == 0 {
			return errFinalizeOrphaned
		}
		upd, casErr := s.applicant.ApproveCAS(ctx, q, a.ID, code.ID, time.Now().UTC(), true)
		if casErr != nil {
			return casErr
		}
		updated = upd
		grantedCodeID = code.ID
		return nil
	})
	return updated, grantedCodeID, txErr
}

// enqueueAutoApproveDM resolves the ADT download URL when needed and
// enqueues the welcome DM. Failure to resolve the URL skips the DM (the
// applicant is still APPROVED + auto_approved=true; operator can retry
// via RetryDM once the ADT side recovers).
func (s *PlaytesthubServiceServer) enqueueAutoApproveDM(ctx context.Context, pt *repo.Playtest, updated *repo.Applicant) {
	if s.dmQueue == nil {
		return
	}
	var adtURLs []string
	if pt.DistributionModel == distModelADT {
		urls, _, urlErr := s.resolveADTDownloadURL(ctx, pt, updated)
		if urlErr != nil {
			slog.WarnContext(ctx, "auto-approve DM skipped: ADT URL resolution failed",
				"playtestId", pt.ID.String(),
				"applicantId", updated.ID.String(),
				"error", urlErr.Error())
			return
		}
		adtURLs = urls
	}
	_ = s.dmQueue.Enqueue(ctx, buildDMJob(updated, pt, false, s.playerBaseURL, adtURLs))
}

// errAutoApproveCapHit is the in-tx sentinel raised when the
// auto_approved count has already reached auto_approve_limit. Treated
// the same as ErrPoolEmpty by the caller: silent PENDING fallback,
// no audit row, no DM.
var errAutoApproveCapHit = errors.New("service: auto-approve cap reached")

// GetApplicantStatus returns the caller's own applicant row for a
// playtest, with the player-visible field subset (schema.md L88). Missing
// applicant → NotFound. Playtest visibility mirrors GetPlaytestForPlayer.
func (s *PlaytesthubServiceServer) GetApplicantStatus(ctx context.Context, req *pb.GetApplicantStatusRequest) (*pb.GetApplicantStatusResponse, error) {
	userID, err := requireActor(ctx)
	if err != nil {
		return nil, err
	}

	pt, err := s.playtest.GetBySlug(ctx, s.namespace, req.GetSlug())
	if e := mapPlaytestLookupErr(err, playtestSoftDelete(pt), "fetching playtest"); e != nil {
		return nil, e
	}
	if pt.Status == statusDraft {
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
	out := playerApplicantToProto(existing)
	// PRD §5.6 one-shot survey: surface the submission timestamp so the
	// Pending-page CTA can render either "Submit feedback" or "Feedback
	// submitted ✓" without an extra round-trip. Best-effort — the store
	// may be unwired in earlier-milestone boots (M1/M2 lack the survey
	// pipeline); on lookup error, fall through with the timestamp unset
	// rather than failing the whole status read.
	if s.surveyResponse != nil {
		resp, err := s.surveyResponse.GetByPlaytestUser(ctx, pt.ID, userID)
		if err == nil && resp != nil {
			out.SurveyResponseSubmittedAt = timestamppb.New(resp.SubmittedAt)
		} else if err != nil && !errors.Is(err, repo.ErrNotFound) {
			slog.WarnContext(ctx, "fetching survey response for applicant status failed; leaving timestamp unset",
				"playtestId", pt.ID.String(),
				"userId", agsid.Format(userID),
				"error", err.Error())
		}
	}
	return &pb.GetApplicantStatusResponse{Applicant: out}, nil
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
		UserId:         agsid.Format(a.UserID),
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

// discordSnowflakePtr returns the Discord snowflake stashed on ctx by
// resolveDiscordSnowflake, or nil when the caller is not
// Discord-federated or the AGS platform lookup failed. Persisted as
// applicant.discord_user_id (migration 0004) so the DM worker has a
// routable identifier independent of the human-readable
// applicant.discord_handle. Per docs/dm-queue.md: NULL → the queue
// records `lastDmError='missing_recipient'` without invoking Discord.
func discordSnowflakePtr(ctx context.Context) *string {
	id, ok := iampkg.DiscordIDFromContext(ctx)
	if !ok || id == "" {
		return nil
	}
	return &id
}

// resolveDiscordSnowflake fetches the caller's Discord snowflake from
// AGS IAM and stashes it on ctx via WithDiscordID, so downstream
// helpers (resolveDiscordHandle + discordSnowflakePtr) consume a single
// source. AGS does not surface the snowflake in the JWT — only the
// `ipf=discord` claim — so without this lookup the DM worker has no
// recipient and approve fails with `missing_recipient`.
//
// No-op when the caller did not federate via Discord, the lookup is
// not wired (dev / smoke), or AGS errors. On lookup failure we log a
// warn and proceed with the UUID fallback (PRD §10 M1).
func (s *PlaytesthubServiceServer) resolveDiscordSnowflake(ctx context.Context, userID uuid.UUID) context.Context {
	if !iampkg.IsDiscordFederatedFromContext(ctx) {
		return ctx
	}
	if s.platformLookup == nil {
		return ctx
	}
	id, err := s.platformLookup.GetDiscordID(ctx, userID.String())
	if err != nil {
		slog.WarnContext(ctx, "ags platform lookup failed; signup proceeds without discord snowflake",
			"userId", agsid.Format(userID), "error", err.Error())
		return ctx
	}
	if id == "" {
		return ctx
	}
	return iampkg.WithDiscordID(ctx, id)
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
		fallback = agsid.Format(userID)
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
		UserId:     agsid.Format(a.UserID),
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
