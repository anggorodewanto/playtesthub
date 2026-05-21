package service

import (
	"context"

	"github.com/anggorodewanto/playtesthub/pkg/agsid"
	iampkg "github.com/anggorodewanto/playtesthub/pkg/iam"
	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
)

// WhoAmI surfaces the authenticated caller's identity to the player
// frontend so the signup form can render the Discord handle inline
// without bouncing through a second AGS round-trip. The Discord-handle
// resolution reuses the same best-effort path Signup walks
// (resolveDiscordSnowflake -> resolveDiscordHandle); failures degrade
// to an empty discord_handle rather than failing the RPC, since the
// caller is already authenticated and the field is informational.
func (s *PlaytesthubServiceServer) WhoAmI(ctx context.Context, _ *pb.WhoAmIRequest) (*pb.WhoAmIResponse, error) {
	userID, err := requireActor(ctx)
	if err != nil {
		return nil, err
	}
	ctx = s.resolveDiscordSnowflake(ctx, userID)
	resp := &pb.WhoAmIResponse{UserId: agsid.Format(userID)}
	if id, ok := iampkg.DiscordIDFromContext(ctx); ok {
		resp.DiscordId = id
	}
	if iampkg.IsDiscordFederatedFromContext(ctx) {
		resp.DiscordHandle = s.resolveDiscordHandle(ctx, userID)
	}
	return resp, nil
}
