package service

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
)

// DiscordLoginProxy carries the dependencies the GetDiscordLoginUrl
// handler needs to perform the server-side `/iam/v3/oauth/authorize`
// hop. The struct is set once at server construction (main.go) — tests
// inject a fake HTTPClient with a custom RoundTripper.
//
// The HTTP client MUST disable automatic redirect following: the
// `Location` header on the 302 from `/oauth/authorize` is the entire
// payload, and a follower would resolve it into the AGS-hosted SPA's
// HTML page and lose the `request_id` we need to extract.
type DiscordLoginProxy struct {
	AGSBaseURL     string
	PlayerClientID string
	HTTPClient     *http.Client
}

// WithDiscordLoginProxy wires the GetDiscordLoginUrl dependencies.
// Passing a zero-value DiscordLoginProxy (or never calling this) leaves
// the handler returning Unavailable, mirroring the WithDiscordLookup
// convention.
func (s *PlaytesthubServiceServer) WithDiscordLoginProxy(p DiscordLoginProxy) *PlaytesthubServiceServer {
	s.discordLogin = p
	return s
}

// GetDiscordLoginUrl performs the first hop of AGS IAM's Discord
// federation flow server-side and returns the second-hop URL the player
// should navigate to. AGS IAM's hosted `/auth/` SPA does not render the
// Discord button on shared cloud, so the player cannot rely on the
// hosted login page — it must drive `/iam/v3/oauth/platforms/discord/authorize`
// directly. That endpoint requires a `request_id` from a prior
// `/iam/v3/oauth/authorize`, whose 302 is opaque cross-origin (the
// browser cannot read the Location header). This handler is therefore
// the minimum server-side surface needed to keep the player a static
// SPA.
//
// PKCE verifier and state never traverse this RPC — the player stores
// them in sessionStorage and presents them only on the final
// `/iam/v3/oauth/token` exchange.
func (s *PlaytesthubServiceServer) GetDiscordLoginUrl(ctx context.Context, req *pb.GetDiscordLoginUrlRequest) (*pb.GetDiscordLoginUrlResponse, error) {
	if s.discordLogin.HTTPClient == nil || s.discordLogin.AGSBaseURL == "" || s.discordLogin.PlayerClientID == "" {
		return nil, status.Error(codes.Unavailable, "discord login proxy not configured")
	}
	if err := validateDiscordLoginRequest(req); err != nil {
		return nil, err
	}

	authorizeURL := buildAuthorizeURL(s.discordLogin.AGSBaseURL, s.discordLogin.PlayerClientID, req)
	requestID, err := fetchRequestID(ctx, s.discordLogin.HTTPClient, authorizeURL)
	if err != nil {
		return nil, err
	}

	loginURL := buildPlatformsDiscordURL(s.discordLogin.AGSBaseURL, s.discordLogin.PlayerClientID, requestID, req.GetRedirectUri())
	return &pb.GetDiscordLoginUrlResponse{LoginUrl: loginURL}, nil
}

func validateDiscordLoginRequest(req *pb.GetDiscordLoginUrlRequest) error {
	if req.GetRedirectUri() == "" {
		return status.Error(codes.InvalidArgument, "redirect_uri is required")
	}
	if u, err := url.Parse(req.GetRedirectUri()); err != nil || u.Scheme == "" || u.Host == "" {
		return status.Error(codes.InvalidArgument, "redirect_uri is not a valid URL")
	}
	if req.GetState() == "" {
		return status.Error(codes.InvalidArgument, "state is required")
	}
	if req.GetCodeChallenge() == "" {
		return status.Error(codes.InvalidArgument, "code_challenge is required")
	}
	if req.GetCodeChallengeMethod() != "S256" {
		return status.Error(codes.InvalidArgument, "code_challenge_method must be S256")
	}
	if req.GetScope() == "" {
		return status.Error(codes.InvalidArgument, "scope is required")
	}
	return nil
}

func buildAuthorizeURL(agsBase, clientID string, req *pb.GetDiscordLoginUrlRequest) string {
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", req.GetRedirectUri())
	q.Set("state", req.GetState())
	q.Set("code_challenge", req.GetCodeChallenge())
	q.Set("code_challenge_method", req.GetCodeChallengeMethod())
	q.Set("scope", req.GetScope())
	return strings.TrimRight(agsBase, "/") + "/iam/v3/oauth/authorize?" + q.Encode()
}

func buildPlatformsDiscordURL(agsBase, clientID, requestID, redirectURI string) string {
	q := url.Values{}
	q.Set("request_id", requestID)
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	return strings.TrimRight(agsBase, "/") + "/iam/v3/oauth/platforms/discord/authorize?" + q.Encode()
}

// fetchRequestID issues the server-side GET against
// /iam/v3/oauth/authorize and extracts the `request_id` query param
// from the 302's Location header. AGS-side errors surface in the
// Location's query as `error=…`; we map them to gRPC codes here so the
// caller can render a precise client message.
func fetchRequestID(ctx context.Context, c *http.Client, authorizeURL string) (string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, authorizeURL, nil)
	if err != nil {
		return "", status.Errorf(codes.Internal, "building authorize request: %v", err)
	}
	resp, err := c.Do(httpReq)
	if err != nil {
		return "", status.Errorf(codes.Unavailable, "AGS IAM authorize unreachable: %v", err)
	}
	defer drain(resp.Body)

	if resp.StatusCode >= 500 {
		return "", status.Errorf(codes.Unavailable, "AGS IAM authorize returned %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusSeeOther {
		return "", status.Errorf(codes.Internal, "AGS IAM authorize returned %d, expected 302", resp.StatusCode)
	}

	location := resp.Header.Get("Location")
	if location == "" {
		return "", status.Error(codes.Internal, "AGS IAM authorize 302 missing Location header")
	}
	locURL, err := url.Parse(location)
	if err != nil {
		return "", status.Errorf(codes.Internal, "parsing Location %q: %v", location, err)
	}
	q := locURL.Query()
	if errCode := q.Get("error"); errCode != "" {
		return "", mapAuthorizeError(errCode, q.Get("error_description"))
	}
	requestID := q.Get("request_id")
	if requestID == "" {
		return "", status.Error(codes.Internal, "AGS IAM authorize Location missing request_id")
	}
	return requestID, nil
}

// mapAuthorizeError translates an OAuth2 `error=` code from the
// Location URL into a gRPC status. `redirect URI invalid` (the message
// AGS IAM emits when the redirect_uri isn't on the client's allowlist)
// is the one operationally interesting case worth surfacing as
// InvalidArgument so the client can fix its inputs; everything else
// is upstream/server-side.
func mapAuthorizeError(code, description string) error {
	if strings.Contains(strings.ToLower(description), "redirect uri") {
		return status.Errorf(codes.InvalidArgument, "AGS IAM rejected redirect URI: %s", description)
	}
	if code == "server_error" {
		return status.Errorf(codes.Unavailable, "AGS IAM server_error: %s", description)
	}
	return status.Errorf(codes.Internal, "AGS IAM authorize error %q: %s", code, description)
}

// drain ensures the http body is fully consumed and closed so the
// connection can be reused. Errors are intentionally ignored — this is
// best-effort cleanup, not a behavioral signal.
func drain(rc io.ReadCloser) {
	if rc == nil {
		return
	}
	_, _ = io.Copy(io.Discard, rc)
	_ = rc.Close()
}
