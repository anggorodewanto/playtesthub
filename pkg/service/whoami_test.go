package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/anggorodewanto/playtesthub/pkg/agsid"
	iampkg "github.com/anggorodewanto/playtesthub/pkg/iam"
	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
)

// TestWhoAmI_HappyPath_ReturnsHandle covers the Discord-federated player
// case the Signup form depends on: the AGS user id round-trips through
// agsid.Format and the Discord handle is resolved via the same bot
// lookup the signup path uses.
func TestWhoAmI_HappyPath_ReturnsHandle(t *testing.T) {
	svr, _, _ := newTestServer()
	platforms := &fakePlatformLookup{discordID: "999"}
	handles := &fakeHandleLookup{handle: "shadowrealm#4421"}
	svr = svr.WithPlatformLookup(platforms).WithDiscordLookup(handles)

	userID := uuid.New()
	ctx := iampkg.WithActorUserID(context.Background(), userID.String())
	ctx = iampkg.WithDiscordFederation(ctx)

	resp, err := svr.WhoAmI(ctx, &pb.WhoAmIRequest{})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got, want := resp.GetUserId(), agsid.Format(userID); got != want {
		t.Errorf("user_id = %q, want %q", got, want)
	}
	if got := resp.GetDiscordId(); got != "999" {
		t.Errorf("discord_id = %q, want 999", got)
	}
	if got := resp.GetDiscordHandle(); got != "shadowrealm#4421" {
		t.Errorf("discord_handle = %q, want shadowrealm#4421", got)
	}
}

// TestWhoAmI_NonDiscordFederated_NoLookup verifies the resolveDiscord*
// helpers stay quiet when the caller is not federated via Discord — no
// bot call, empty discord_* fields, but user_id still surfaces so the
// player UI can still address the user.
func TestWhoAmI_NonDiscordFederated_NoLookup(t *testing.T) {
	svr, _, _ := newTestServer()
	platforms := &fakePlatformLookup{discordID: "999"}
	handles := &fakeHandleLookup{handle: "should-not-be-used"}
	svr = svr.WithPlatformLookup(platforms).WithDiscordLookup(handles)

	userID := uuid.New()
	ctx := iampkg.WithActorUserID(context.Background(), userID.String())

	resp, err := svr.WhoAmI(ctx, &pb.WhoAmIRequest{})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got := resp.GetDiscordHandle(); got != "" {
		t.Errorf("discord_handle = %q, want empty for non-Discord caller", got)
	}
	if got := resp.GetDiscordId(); got != "" {
		t.Errorf("discord_id = %q, want empty for non-Discord caller", got)
	}
	if platforms.called {
		t.Error("platform lookup must stay quiet for non-Discord callers")
	}
	if handles.called {
		t.Error("handle lookup must stay quiet for non-Discord callers")
	}
	if got, want := resp.GetUserId(), agsid.Format(userID); got != want {
		t.Errorf("user_id = %q, want %q", got, want)
	}
}

// TestWhoAmI_HandleLookupFails_FallsBackToRawID confirms a Discord-side
// outage degrades to the snowflake rather than failing the RPC — the
// caller is authenticated and the field is informational.
func TestWhoAmI_HandleLookupFails_FallsBackToRawID(t *testing.T) {
	svr, _, _ := newTestServer()
	platforms := &fakePlatformLookup{discordID: "42"}
	handles := &fakeHandleLookup{err: errors.New("discord 503")}
	svr = svr.WithPlatformLookup(platforms).WithDiscordLookup(handles)

	userID := uuid.New()
	ctx := iampkg.WithActorUserID(context.Background(), userID.String())
	ctx = iampkg.WithDiscordFederation(ctx)

	resp, err := svr.WhoAmI(ctx, &pb.WhoAmIRequest{})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got := resp.GetDiscordHandle(); got != "42" {
		t.Errorf("discord_handle = %q, want 42 (snowflake fallback)", got)
	}
	if got := resp.GetDiscordId(); got != "42" {
		t.Errorf("discord_id = %q, want 42", got)
	}
}

// TestWhoAmI_NoActor_Unauthenticated guards the requireActor gate — an
// anonymous caller must receive Unauthenticated, never a partial profile.
func TestWhoAmI_NoActor_Unauthenticated(t *testing.T) {
	svr, _, _ := newTestServer()
	_, err := svr.WhoAmI(context.Background(), &pb.WhoAmIRequest{})
	if err == nil {
		t.Fatal("expected error")
	}
	if got, want := status.Code(err), codes.Unauthenticated; got != want {
		t.Errorf("code = %s, want %s", got, want)
	}
}
