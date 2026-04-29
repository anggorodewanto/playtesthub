package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
)

const bearerTokenType = "Bearer"

// DiscordExchangeProxy carries the dependencies the ExchangeDiscordCode
// handler needs to drive AGS IAM's platform-token grant. The struct is
// set once at server construction (main.go); tests inject a fake
// HTTPClient with a custom RoundTripper.
//
// Unlike the phase 9.2 DiscordLoginProxy, this client follows redirects
// normally — there's no opaque-302 trick on the platform-token grant.
type DiscordExchangeProxy struct {
	AGSBaseURL   string
	ClientID     string
	ClientSecret string
	HTTPClient   *http.Client
}

// WithDiscordExchangeProxy wires the ExchangeDiscordCode dependencies.
// Passing a zero-value DiscordExchangeProxy (or never calling this)
// leaves the handler returning Unavailable, mirroring WithDiscordLookup.
func (s *PlaytesthubServiceServer) WithDiscordExchangeProxy(p DiscordExchangeProxy) *PlaytesthubServiceServer {
	s.discordExchange = p
	return s
}

// ExchangeDiscordCode posts a Discord OAuth authorization code to AGS
// IAM's platform-token grant and forwards the resulting AGS access /
// refresh tokens to the player. AGS auto-creates the federated user's
// Justice platform account on first call — the failure mode the phase
// 9.2 auth-code flow hit (game-namespace `LoadAuthorize` lookup of an
// account that was never created) does not exist on this path.
//
// Request authentication uses the confidential server-side IAM client
// (HTTP Basic): a public/PKCE client cannot drive the platform-token
// grant on shared cloud.
func (s *PlaytesthubServiceServer) ExchangeDiscordCode(ctx context.Context, req *pb.ExchangeDiscordCodeRequest) (*pb.ExchangeDiscordCodeResponse, error) {
	if s.discordExchange.HTTPClient == nil ||
		s.discordExchange.AGSBaseURL == "" ||
		s.discordExchange.ClientID == "" ||
		s.discordExchange.ClientSecret == "" {
		return nil, status.Error(codes.Unavailable, "discord exchange proxy not configured")
	}
	if err := validateDiscordExchangeRequest(req); err != nil {
		return nil, err
	}

	tokenURL := strings.TrimRight(s.discordExchange.AGSBaseURL, "/") + "/iam/v3/oauth/platforms/discord/token"
	form := url.Values{}
	form.Set("platform_token", req.GetCode())
	form.Set("redirect_uri", req.GetRedirectUri())

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "building token request: %v", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.SetBasicAuth(s.discordExchange.ClientID, s.discordExchange.ClientSecret)

	resp, err := s.discordExchange.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "AGS IAM unreachable: %v", err)
	}
	defer drainExchangeBody(resp.Body)

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, status.Errorf(codes.Unavailable, "reading AGS IAM response: %v", readErr)
	}

	if resp.StatusCode >= 400 {
		return nil, mapAGSExchangeError(resp.StatusCode, body)
	}

	var token agsTokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return nil, status.Errorf(codes.Internal, "decoding AGS IAM token response: %v", err)
	}
	if token.AccessToken == "" {
		return nil, status.Error(codes.Internal, "AGS IAM token response missing access_token")
	}

	tokenType := token.TokenType
	if tokenType == "" {
		tokenType = bearerTokenType
	}
	return &pb.ExchangeDiscordCodeResponse{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		ExpiresIn:    int32(token.ExpiresIn),
		TokenType:    tokenType,
	}, nil
}

func validateDiscordExchangeRequest(req *pb.ExchangeDiscordCodeRequest) error {
	if req.GetCode() == "" {
		return status.Error(codes.InvalidArgument, "code is required")
	}
	if req.GetRedirectUri() == "" {
		return status.Error(codes.InvalidArgument, "redirect_uri is required")
	}
	if u, err := url.Parse(req.GetRedirectUri()); err != nil || u.Scheme == "" || u.Host == "" {
		return status.Error(codes.InvalidArgument, "redirect_uri is not a valid URL")
	}
	return nil
}

// agsTokenResponse mirrors the subset of AGS IAM's token response we
// forward. AGS may include extra fields (`namespace`, `user_id`, etc.)
// — they're ignored here since the player Bearer-attaches the access
// token and AGS re-derives identity from the JWT on every call.
type agsTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// agsErrorBody covers AGS IAM's two error response shapes — the OAuth2
// {error, error_description} form and AGS's own {errorCode, errorMessage}
// — so we can map either reliably without needing to know in advance
// which one the upstream picked.
type agsErrorBody struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
	ErrorCode        int    `json:"errorCode"`
	ErrorMessage     string `json:"errorMessage"`
}

// mapAGSExchangeError translates an AGS IAM error response into a gRPC
// status. invalid_grant is the player-actionable case (Discord code
// expired, already used, or rejected) — surfaced as InvalidArgument
// with the AGS description so the client can render something useful.
// Everything else is an upstream/config problem and gets a generic
// Internal/Unavailable so we don't leak server-side detail to the
// player.
//
// AGS quirk: when Discord returns a 4xx invalid_grant, AGS does NOT
// pass that through as a 4xx — it wraps it as HTTP 5xx with
// `error=server_error` and the Discord error embedded as a substring of
// `error_description` (e.g. `platform server error: unexpected HTTP
// status code response -- 6s -- https://discord.com/api/oauth2/token
// 400 {"error": "invalid_grant", ...}`). Treat that case as an
// InvalidArgument too so retries don't look like an outage to the
// player. Genuine AGS 5xx (no Discord-invalid_grant marker) stays
// Unavailable.
func mapAGSExchangeError(statusCode int, body []byte) error {
	var e agsErrorBody
	_ = json.Unmarshal(body, &e)

	if e.Error == "invalid_grant" || isWrappedDiscordInvalidGrant(e) {
		desc := e.ErrorDescription
		if desc == "" {
			desc = "Discord authorization code rejected by AGS IAM"
		}
		return status.Error(codes.InvalidArgument, desc)
	}

	if statusCode >= 500 {
		return status.Errorf(codes.Unavailable, "AGS IAM returned %d", statusCode)
	}

	if e.Error == "unauthorized_client" {
		return status.Error(codes.Internal, "backend Discord federation misconfigured")
	}

	if e.ErrorMessage != "" {
		return status.Error(codes.Internal, fmt.Sprintf("AGS IAM error: %s", e.ErrorMessage))
	}
	return status.Error(codes.Internal, "AGS IAM token exchange failed")
}

func isWrappedDiscordInvalidGrant(e agsErrorBody) bool {
	if e.Error != "server_error" {
		return false
	}
	desc := e.ErrorDescription
	return strings.Contains(desc, "discord.com") && strings.Contains(desc, "invalid_grant")
}

func drainExchangeBody(rc io.ReadCloser) {
	if rc == nil {
		return
	}
	_, _ = io.Copy(io.Discard, rc)
	_ = rc.Close()
}
