package service

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
)

// fakeRoundTripper short-circuits the http.Client with a function. The
// handler issues exactly one GET to /iam/v3/oauth/authorize per call;
// tests assert the request URL and supply the response.
type fakeRoundTripper struct {
	saw  *http.Request
	resp *http.Response
	err  error
}

func (f *fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	f.saw = req
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

func newRedirectResponse(location string) *http.Response {
	h := http.Header{}
	h.Set("Location", location)
	return &http.Response{
		StatusCode: http.StatusFound,
		Header:     h,
		Body:       io.NopCloser(bytes.NewReader(nil)),
	}
}

func newPlainResponse(code int, body string) *http.Response {
	return &http.Response{
		StatusCode: code,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func discordSvr(rt http.RoundTripper) *PlaytesthubServiceServer {
	svr, _, _ := newTestServer()
	return svr.WithDiscordLoginProxy(DiscordLoginProxy{
		AGSBaseURL:     "https://ags.example.com",
		PlayerClientID: "player-pub-client",
		HTTPClient: &http.Client{
			Transport:     rt,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	})
}

func validDiscordLoginReq() *pb.GetDiscordLoginUrlRequest {
	return &pb.GetDiscordLoginUrlRequest{
		RedirectUri:         "http://localhost:5173/callback",
		State:               "state-xyz",
		CodeChallenge:       "challenge-xyz",
		CodeChallengeMethod: "S256",
		Scope:               "account commerce",
	}
}

// ---------------- happy path -------------------------------------------------

func TestGetDiscordLoginUrl_HappyPath_ReturnsPlatformsDiscordURL(t *testing.T) {
	rt := &fakeRoundTripper{
		resp: newRedirectResponse(
			"https://internal.gamingservices.accelbyte.io/auth/?client_id=player-pub-client&request_id=abc123&redirect_uri=http%3A%2F%2Flocalhost%3A5173%2Fcallback",
		),
	}
	svr := discordSvr(rt)

	resp, err := svr.GetDiscordLoginUrl(t.Context(), validDiscordLoginReq())
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}

	// 1. The outbound request must hit /iam/v3/oauth/authorize with the
	//    full PKCE + state forwarded through, and client_id=player-pub-client.
	got := rt.saw
	if got == nil {
		t.Fatal("no request issued")
	}
	if got.URL.Host != "ags.example.com" {
		t.Errorf("outbound host = %q, want ags.example.com", got.URL.Host)
	}
	if got.URL.Path != "/iam/v3/oauth/authorize" {
		t.Errorf("outbound path = %q, want /iam/v3/oauth/authorize", got.URL.Path)
	}
	q := got.URL.Query()
	if q.Get("client_id") != "player-pub-client" {
		t.Errorf("outbound client_id = %q", q.Get("client_id"))
	}
	if q.Get("response_type") != "code" {
		t.Errorf("outbound response_type = %q, want code", q.Get("response_type"))
	}
	if q.Get("redirect_uri") != "http://localhost:5173/callback" {
		t.Errorf("outbound redirect_uri = %q", q.Get("redirect_uri"))
	}
	if q.Get("state") != "state-xyz" {
		t.Errorf("outbound state = %q", q.Get("state"))
	}
	if q.Get("code_challenge") != "challenge-xyz" {
		t.Errorf("outbound code_challenge = %q", q.Get("code_challenge"))
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("outbound code_challenge_method = %q", q.Get("code_challenge_method"))
	}
	if q.Get("scope") != "account commerce" {
		t.Errorf("outbound scope = %q", q.Get("scope"))
	}

	// 2. The composed login_url must drive /oauth/platforms/discord/authorize
	//    on the namespace base URL with request_id, client_id, redirect_uri.
	parsed, err := url.Parse(resp.GetLoginUrl())
	if err != nil {
		t.Fatalf("login_url not a URL: %v", err)
	}
	if parsed.Host != "ags.example.com" {
		t.Errorf("login_url host = %q", parsed.Host)
	}
	if parsed.Path != "/iam/v3/oauth/platforms/discord/authorize" {
		t.Errorf("login_url path = %q", parsed.Path)
	}
	pq := parsed.Query()
	if pq.Get("request_id") != "abc123" {
		t.Errorf("login_url request_id = %q", pq.Get("request_id"))
	}
	if pq.Get("client_id") != "player-pub-client" {
		t.Errorf("login_url client_id = %q", pq.Get("client_id"))
	}
	if pq.Get("redirect_uri") != "http://localhost:5173/callback" {
		t.Errorf("login_url redirect_uri = %q", pq.Get("redirect_uri"))
	}
}

// ---------------- input validation ------------------------------------------

func TestGetDiscordLoginUrl_MissingRedirectUri_InvalidArgument(t *testing.T) {
	svr := discordSvr(&fakeRoundTripper{})
	req := validDiscordLoginReq()
	req.RedirectUri = ""

	_, err := svr.GetDiscordLoginUrl(t.Context(), req)
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "redirect_uri")
}

func TestGetDiscordLoginUrl_BadRedirectUri_InvalidArgument(t *testing.T) {
	svr := discordSvr(&fakeRoundTripper{})
	req := validDiscordLoginReq()
	req.RedirectUri = "not a url"

	_, err := svr.GetDiscordLoginUrl(t.Context(), req)
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "redirect_uri")
}

func TestGetDiscordLoginUrl_MissingState_InvalidArgument(t *testing.T) {
	svr := discordSvr(&fakeRoundTripper{})
	req := validDiscordLoginReq()
	req.State = ""

	_, err := svr.GetDiscordLoginUrl(t.Context(), req)
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "state")
}

func TestGetDiscordLoginUrl_MissingCodeChallenge_InvalidArgument(t *testing.T) {
	svr := discordSvr(&fakeRoundTripper{})
	req := validDiscordLoginReq()
	req.CodeChallenge = ""

	_, err := svr.GetDiscordLoginUrl(t.Context(), req)
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "code_challenge")
}

func TestGetDiscordLoginUrl_NonS256ChallengeMethod_InvalidArgument(t *testing.T) {
	svr := discordSvr(&fakeRoundTripper{})
	req := validDiscordLoginReq()
	req.CodeChallengeMethod = "plain"

	_, err := svr.GetDiscordLoginUrl(t.Context(), req)
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "code_challenge_method")
}

func TestGetDiscordLoginUrl_MissingScope_InvalidArgument(t *testing.T) {
	svr := discordSvr(&fakeRoundTripper{})
	req := validDiscordLoginReq()
	req.Scope = ""

	_, err := svr.GetDiscordLoginUrl(t.Context(), req)
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "scope")
}

// ---------------- AGS IAM responses -----------------------------------------

func TestGetDiscordLoginUrl_AgsRedirectURIInvalid_InvalidArgument(t *testing.T) {
	rt := &fakeRoundTripper{
		resp: newRedirectResponse(
			"https://internal.gamingservices.accelbyte.io/auth/?error=invalid_request&error_description=redirect+URI+invalid",
		),
	}
	svr := discordSvr(rt)

	_, err := svr.GetDiscordLoginUrl(t.Context(), validDiscordLoginReq())
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "redirect URI")
}

func TestGetDiscordLoginUrl_AgsServerError_Unavailable(t *testing.T) {
	rt := &fakeRoundTripper{
		resp: newRedirectResponse(
			"https://internal.gamingservices.accelbyte.io/auth/?error=server_error&error_description=upstream+failure",
		),
	}
	svr := discordSvr(rt)

	_, err := svr.GetDiscordLoginUrl(t.Context(), validDiscordLoginReq())
	requireStatus(t, err, codes.Unavailable)
}

func TestGetDiscordLoginUrl_NoLocation_Internal(t *testing.T) {
	rt := &fakeRoundTripper{resp: newPlainResponse(http.StatusOK, "{}")}
	svr := discordSvr(rt)

	_, err := svr.GetDiscordLoginUrl(t.Context(), validDiscordLoginReq())
	requireStatus(t, err, codes.Internal)
}

func TestGetDiscordLoginUrl_RedirectMissingRequestId_Internal(t *testing.T) {
	rt := &fakeRoundTripper{
		resp: newRedirectResponse("https://internal.gamingservices.accelbyte.io/auth/?client_id=x"),
	}
	svr := discordSvr(rt)

	_, err := svr.GetDiscordLoginUrl(t.Context(), validDiscordLoginReq())
	requireStatus(t, err, codes.Internal)
	requireMsgContains(t, err, "request_id")
}

func TestGetDiscordLoginUrl_NetworkError_Unavailable(t *testing.T) {
	rt := &fakeRoundTripper{err: io.ErrUnexpectedEOF}
	svr := discordSvr(rt)

	_, err := svr.GetDiscordLoginUrl(t.Context(), validDiscordLoginReq())
	requireStatus(t, err, codes.Unavailable)
}

// ---------------- safety: handler must not be wired to a default proxy ------

func TestGetDiscordLoginUrl_NotConfigured_Unavailable(t *testing.T) {
	svr, _, _ := newTestServer()
	_, err := svr.GetDiscordLoginUrl(t.Context(), validDiscordLoginReq())
	requireStatus(t, err, codes.Unavailable)
}
