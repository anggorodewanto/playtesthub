package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	"github.com/anggorodewanto/playtesthub/pkg/discord"
	iampkg "github.com/anggorodewanto/playtesthub/pkg/iam"
	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// fakeHandleLookup lets tests force a Discord lookup outcome without an
// HTTP round trip. handle is returned when err is nil; err is returned
// verbatim otherwise.
type fakeHandleLookup struct {
	called bool
	withID string
	handle string
	err    error
}

func (f *fakeHandleLookup) LookupHandle(_ context.Context, discordID string) (string, error) {
	f.called = true
	f.withID = discordID
	if f.err != nil {
		return "", f.err
	}
	return f.handle, nil
}

var _ discord.HandleLookup = (*fakeHandleLookup)(nil)

// signupCtx wires both the AGS actor id and the Discord snowflake the
// auth interceptor would normally plumb for a Discord-federated player.
func signupCtx(userID uuid.UUID, discordID string) context.Context {
	ctx := iampkg.WithActorUserID(context.Background(), userID.String())
	return iampkg.WithDiscordID(ctx, discordID)
}

func openPlaytest(slug string) *repo.Playtest {
	return &repo.Playtest{
		ID:                uuid.New(),
		Namespace:         testNamespace,
		Slug:              slug,
		Title:             "t",
		DistributionModel: "STEAM_KEYS",
		Status:            "OPEN",
	}
}

// ---------------- Signup ----------------------------------------------------

func TestSignup_HappyPath_CreatesPendingApplicant(t *testing.T) {
	svr, store, applicants := newTestServer()
	lookup := &fakeHandleLookup{handle: "Alice"}
	svr = svr.WithDiscordLookup(lookup)

	pt := openPlaytest("game")
	store.rows = append(store.rows, pt)

	userID := uuid.New()
	resp, err := svr.Signup(signupCtx(userID, "1234567890"), &pb.SignupRequest{
		Slug:      "game",
		Platforms: []pb.Platform{pb.Platform_PLATFORM_STEAM, pb.Platform_PLATFORM_XBOX},
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if resp.GetApplicant().GetStatus() != pb.ApplicantStatus_APPLICANT_STATUS_PENDING {
		t.Errorf("status = %s, want PENDING", resp.GetApplicant().GetStatus())
	}
	if resp.GetApplicant().GetUserId() != userID.String() {
		t.Errorf("user_id = %q, want %q", resp.GetApplicant().GetUserId(), userID.String())
	}
	if !lookup.called {
		t.Error("expected discord lookup to run")
	}
	if lookup.withID != "1234567890" {
		t.Errorf("lookup called with %q, want 1234567890", lookup.withID)
	}

	// Admin-side: the row should persist the resolved handle.
	stored := applicants.rows[0]
	if stored.DiscordHandle != "Alice" {
		t.Errorf("persisted discord_handle = %q, want Alice", stored.DiscordHandle)
	}

	// Player-visible response must NOT leak discord handle or platforms.
	if got := resp.GetApplicant().GetDiscordHandle(); got != "" {
		t.Errorf("discord_handle leaked to player: %q", got)
	}
	if len(resp.GetApplicant().GetPlatforms()) != 0 {
		t.Errorf("platforms leaked to player: %v", resp.GetApplicant().GetPlatforms())
	}
}

func TestSignup_DiscordLookupFails_FallsBackToRawID(t *testing.T) {
	svr, store, applicants := newTestServer()
	lookup := &fakeHandleLookup{err: errors.New("discord 404")}
	svr = svr.WithDiscordLookup(lookup)

	store.rows = append(store.rows, openPlaytest("game"))

	_, err := svr.Signup(signupCtx(uuid.New(), "99"), &pb.SignupRequest{
		Slug:      "game",
		Platforms: []pb.Platform{pb.Platform_PLATFORM_STEAM},
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got := applicants.rows[0].DiscordHandle; got != "99" {
		t.Errorf("fallback handle = %q, want 99 (raw discord id)", got)
	}
}

func TestSignup_NoDiscordIDInCtx_FallsBackToUserID(t *testing.T) {
	svr, store, applicants := newTestServer()
	svr = svr.WithDiscordLookup(&fakeHandleLookup{handle: "shouldnt-run"})

	store.rows = append(store.rows, openPlaytest("game"))

	userID := uuid.New()
	// Skip WithDiscordID — simulates non-Discord-federated token.
	ctx := iampkg.WithActorUserID(context.Background(), userID.String())
	_, err := svr.Signup(ctx, &pb.SignupRequest{
		Slug:      "game",
		Platforms: []pb.Platform{pb.Platform_PLATFORM_STEAM},
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got := applicants.rows[0].DiscordHandle; got != userID.String() {
		t.Errorf("fallback handle = %q, want %s (AGS user id)", got, userID)
	}
}

func TestSignup_Idempotent_ReturnsExistingApplicant(t *testing.T) {
	svr, store, applicants := newTestServer()
	pt := openPlaytest("game")
	store.rows = append(store.rows, pt)

	userID := uuid.New()
	existingID := uuid.New()
	applicants.rows = append(applicants.rows, &repo.Applicant{
		ID:            existingID,
		PlaytestID:    pt.ID,
		UserID:        userID,
		DiscordHandle: "OldHandle",
		Platforms:     []string{"STEAM"},
		Status:        "PENDING",
	})

	resp, err := svr.Signup(signupCtx(userID, "42"), &pb.SignupRequest{
		Slug:      "game",
		Platforms: []pb.Platform{pb.Platform_PLATFORM_XBOX},
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if resp.GetApplicant().GetId() != existingID.String() {
		t.Errorf("returned id = %s, want existing %s", resp.GetApplicant().GetId(), existingID)
	}
	if len(applicants.rows) != 1 {
		t.Errorf("applicant count = %d, want 1 (idempotent)", len(applicants.rows))
	}
}

func TestSignup_UniqueViolationRace_ResolvesToExistingRow(t *testing.T) {
	svr, store, applicants := newTestServer()
	pt := openPlaytest("game")
	store.rows = append(store.rows, pt)

	userID := uuid.New()
	// Simulate the race: lookup before insert sees nothing, but a
	// concurrent writer lands the row before our insert executes. Inject
	// the existing row AND force the fake store to return
	// ErrUniqueViolation on insert.
	existingID := uuid.New()
	applicants.rows = append(applicants.rows, &repo.Applicant{
		ID: existingID, PlaytestID: pt.ID, UserID: userID,
		DiscordHandle: "raceWinner", Status: "PENDING",
	})
	applicants.insertErr = repo.ErrUniqueViolation

	resp, err := svr.Signup(signupCtx(userID, "1"), &pb.SignupRequest{
		Slug:      "game",
		Platforms: []pb.Platform{pb.Platform_PLATFORM_STEAM},
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if resp.GetApplicant().GetId() != existingID.String() {
		t.Errorf("id = %s, want %s", resp.GetApplicant().GetId(), existingID)
	}
}

func TestSignup_DraftPlaytest_NotFound(t *testing.T) {
	svr, store, _ := newTestServer()
	pt := openPlaytest("draft")
	pt.Status = statusDraft
	store.rows = append(store.rows, pt)

	_, err := svr.Signup(signupCtx(uuid.New(), "1"), &pb.SignupRequest{
		Slug:      "draft",
		Platforms: []pb.Platform{pb.Platform_PLATFORM_STEAM},
	})
	requireStatus(t, err, codes.NotFound)
}

func TestSignup_SoftDeletedPlaytest_NotFound(t *testing.T) {
	svr, store, _ := newTestServer()
	pt := openPlaytest("gone")
	now := pt.CreatedAt
	pt.DeletedAt = &now
	store.rows = append(store.rows, pt)

	_, err := svr.Signup(signupCtx(uuid.New(), "1"), &pb.SignupRequest{
		Slug:      "gone",
		Platforms: []pb.Platform{pb.Platform_PLATFORM_STEAM},
	})
	requireStatus(t, err, codes.NotFound)
}

func TestSignup_ClosedPlaytest_NonApproved_NotFound(t *testing.T) {
	svr, store, _ := newTestServer()
	pt := openPlaytest("closed")
	pt.Status = statusClosed
	store.rows = append(store.rows, pt)

	_, err := svr.Signup(signupCtx(uuid.New(), "1"), &pb.SignupRequest{
		Slug:      "closed",
		Platforms: []pb.Platform{pb.Platform_PLATFORM_STEAM},
	})
	requireStatus(t, err, codes.NotFound)
}

func TestSignup_MissingActor_Unauthenticated(t *testing.T) {
	svr, store, _ := newTestServer()
	store.rows = append(store.rows, openPlaytest("game"))

	_, err := svr.Signup(context.Background(), &pb.SignupRequest{
		Slug:      "game",
		Platforms: []pb.Platform{pb.Platform_PLATFORM_STEAM},
	})
	requireStatus(t, err, codes.Unauthenticated)
}

func TestSignup_UnknownPlatformEnum_InvalidArgument(t *testing.T) {
	svr, store, _ := newTestServer()
	store.rows = append(store.rows, openPlaytest("game"))

	_, err := svr.Signup(signupCtx(uuid.New(), "1"), &pb.SignupRequest{
		Slug:      "game",
		Platforms: []pb.Platform{pb.Platform_PLATFORM_UNSPECIFIED},
	})
	requireStatus(t, err, codes.InvalidArgument)
}

// ---------------- GetApplicantStatus ----------------------------------------

func TestGetApplicantStatus_HappyPath_PlayerSubsetOnly(t *testing.T) {
	svr, store, applicants := newTestServer()
	pt := openPlaytest("game")
	store.rows = append(store.rows, pt)

	userID := uuid.New()
	applicants.rows = append(applicants.rows, &repo.Applicant{
		ID: uuid.New(), PlaytestID: pt.ID, UserID: userID,
		DiscordHandle: "privateHandle", Platforms: []string{"STEAM"},
		Status: "PENDING",
	})

	resp, err := svr.GetApplicantStatus(signupCtx(userID, "1"), &pb.GetApplicantStatusRequest{Slug: "game"})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if resp.GetApplicant().GetStatus() != pb.ApplicantStatus_APPLICANT_STATUS_PENDING {
		t.Errorf("status = %s, want PENDING", resp.GetApplicant().GetStatus())
	}
	if got := resp.GetApplicant().GetDiscordHandle(); got != "" {
		t.Errorf("discord_handle leaked: %q", got)
	}
	if got := resp.GetApplicant().GetRejectionReason(); got != "" {
		t.Errorf("rejection_reason leaked: %q", got)
	}
	if len(resp.GetApplicant().GetPlatforms()) != 0 {
		t.Errorf("platforms leaked: %v", resp.GetApplicant().GetPlatforms())
	}
}

func TestGetApplicantStatus_Missing_NotFound(t *testing.T) {
	svr, store, _ := newTestServer()
	store.rows = append(store.rows, openPlaytest("game"))

	_, err := svr.GetApplicantStatus(signupCtx(uuid.New(), "1"), &pb.GetApplicantStatusRequest{Slug: "game"})
	requireStatus(t, err, codes.NotFound)
}

func TestGetApplicantStatus_Draft_NotFound(t *testing.T) {
	svr, store, _ := newTestServer()
	pt := openPlaytest("draft")
	pt.Status = statusDraft
	store.rows = append(store.rows, pt)

	_, err := svr.GetApplicantStatus(signupCtx(uuid.New(), "1"), &pb.GetApplicantStatusRequest{Slug: "draft"})
	requireStatus(t, err, codes.NotFound)
}

func TestGetApplicantStatus_ClosedPlaytest_NonApproved_NotFound(t *testing.T) {
	svr, store, applicants := newTestServer()
	pt := openPlaytest("closed")
	pt.Status = statusClosed
	store.rows = append(store.rows, pt)

	userID := uuid.New()
	applicants.rows = append(applicants.rows, &repo.Applicant{
		ID: uuid.New(), PlaytestID: pt.ID, UserID: userID, Status: "PENDING",
	})

	_, err := svr.GetApplicantStatus(signupCtx(userID, "1"), &pb.GetApplicantStatusRequest{Slug: "closed"})
	requireStatus(t, err, codes.NotFound)
}

func TestGetApplicantStatus_ClosedPlaytest_ApprovedVisible(t *testing.T) {
	svr, store, applicants := newTestServer()
	pt := openPlaytest("closed")
	pt.Status = statusClosed
	store.rows = append(store.rows, pt)

	userID := uuid.New()
	grantedID := uuid.New()
	applicants.rows = append(applicants.rows, &repo.Applicant{
		ID: uuid.New(), PlaytestID: pt.ID, UserID: userID,
		Status: "APPROVED", GrantedCodeID: &grantedID,
	})

	resp, err := svr.GetApplicantStatus(signupCtx(userID, "1"), &pb.GetApplicantStatusRequest{Slug: "closed"})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if resp.GetApplicant().GetStatus() != pb.ApplicantStatus_APPLICANT_STATUS_APPROVED {
		t.Fatal("expected APPROVED status")
	}
	if resp.GetApplicant().GetGrantedCodeId() == "" {
		t.Fatal("expected granted_code_id presence")
	}
}

func TestGetApplicantStatus_MissingActor_Unauthenticated(t *testing.T) {
	svr, _, _ := newTestServer()
	_, err := svr.GetApplicantStatus(context.Background(), &pb.GetApplicantStatusRequest{Slug: "any"})
	requireStatus(t, err, codes.Unauthenticated)
}
