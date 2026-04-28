package service

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
)

// fakeRoundTripper short-circuits the http.Client with a function. The
// handler issues exactly one POST to /iam/v3/oauth/platforms/discord/token
// per call; tests assert the request shape and supply the response.
type fakeRoundTripper struct {
	saw     *http.Request
	sawBody []byte
	resp    *http.Response
	err     error
}

func (f *fakeRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	f.saw = req
	if req.Body != nil {
		f.sawBody, _ = io.ReadAll(req.Body)
		_ = req.Body.Close()
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

func newJSONResponse(code int, body string) *http.Response {
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	return &http.Response{
		StatusCode: code,
		Header:     h,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func exchangeSvr(rt http.RoundTripper) *PlaytesthubServiceServer {
	svr, _, _ := newTestServer()
	return svr.WithDiscordExchangeProxy(DiscordExchangeProxy{
		AGSBaseURL:   "https://ags.example.com",
		ClientID:     "confidential-client",
		ClientSecret: "confidential-secret",
		HTTPClient:   &http.Client{Transport: rt},
	})
}

func validExchangeReq() *pb.ExchangeDiscordCodeRequest {
	return &pb.ExchangeDiscordCodeRequest{
		Code:        "discord-auth-code-xyz",
		RedirectUri: "http://localhost:5173/callback",
	}
}

// ---------------- happy path -------------------------------------------------

func TestExchangeDiscordCode_HappyPath_ForwardsTokens(t *testing.T) {
	rt := &fakeRoundTripper{
		resp: newJSONResponse(http.StatusOK, `{
			"access_token":"ags-access-tok",
			"refresh_token":"ags-refresh-tok",
			"expires_in":3600,
			"token_type":"Bearer"
		}`),
	}
	svr := exchangeSvr(rt)

	resp, err := svr.ExchangeDiscordCode(t.Context(), validExchangeReq())
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if resp.GetAccessToken() != "ags-access-tok" {
		t.Errorf("access_token = %q", resp.GetAccessToken())
	}
	if resp.GetRefreshToken() != "ags-refresh-tok" {
		t.Errorf("refresh_token = %q", resp.GetRefreshToken())
	}
	if resp.GetExpiresIn() != 3600 {
		t.Errorf("expires_in = %d", resp.GetExpiresIn())
	}
	if resp.GetTokenType() != bearerTokenType {
		t.Errorf("token_type = %q", resp.GetTokenType())
	}

	got := rt.saw
	if got == nil {
		t.Fatal("no request issued")
	}
	if got.URL.Host != "ags.example.com" {
		t.Errorf("outbound host = %q", got.URL.Host)
	}
	if got.URL.Path != "/iam/v3/oauth/platforms/discord/token" {
		t.Errorf("outbound path = %q", got.URL.Path)
	}
	if got.Method != http.MethodPost {
		t.Errorf("outbound method = %q", got.Method)
	}
	if ct := got.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q", ct)
	}
	user, pass, ok := got.BasicAuth()
	if !ok {
		t.Error("no Basic auth header set")
	}
	if user != "confidential-client" || pass != "confidential-secret" {
		t.Errorf("Basic auth = %q:%q", user, pass)
	}

	// Form body shape: AGS expects platform_token (the Discord code) +
	// redirect_uri (byte-exact what the player sent to Discord).
	if !bytes.Contains(rt.sawBody, []byte("platform_token=discord-auth-code-xyz")) {
		t.Errorf("body missing platform_token: %s", rt.sawBody)
	}
	if !bytes.Contains(rt.sawBody, []byte("redirect_uri=http%3A%2F%2Flocalhost%3A5173%2Fcallback")) {
		t.Errorf("body missing redirect_uri: %s", rt.sawBody)
	}
}

func TestExchangeDiscordCode_DefaultsTokenTypeWhenAbsent(t *testing.T) {
	// AGS may omit token_type — proto field should still default to Bearer
	// rather than the empty string (spec: always Bearer today).
	rt := &fakeRoundTripper{
		resp: newJSONResponse(http.StatusOK, `{"access_token":"x","expires_in":60}`),
	}
	svr := exchangeSvr(rt)

	resp, err := svr.ExchangeDiscordCode(t.Context(), validExchangeReq())
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if resp.GetTokenType() != bearerTokenType {
		t.Errorf("token_type = %q, want Bearer", resp.GetTokenType())
	}
}

// ---------------- input validation ------------------------------------------

func TestExchangeDiscordCode_MissingCode_InvalidArgument(t *testing.T) {
	svr := exchangeSvr(&fakeRoundTripper{})
	req := validExchangeReq()
	req.Code = ""

	_, err := svr.ExchangeDiscordCode(t.Context(), req)
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "code")
}

func TestExchangeDiscordCode_MissingRedirectUri_InvalidArgument(t *testing.T) {
	svr := exchangeSvr(&fakeRoundTripper{})
	req := validExchangeReq()
	req.RedirectUri = ""

	_, err := svr.ExchangeDiscordCode(t.Context(), req)
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "redirect_uri")
}

func TestExchangeDiscordCode_BadRedirectUri_InvalidArgument(t *testing.T) {
	svr := exchangeSvr(&fakeRoundTripper{})
	req := validExchangeReq()
	req.RedirectUri = "not a url"

	_, err := svr.ExchangeDiscordCode(t.Context(), req)
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "redirect_uri")
}

// ---------------- AGS IAM responses -----------------------------------------

func TestExchangeDiscordCode_AgsInvalidGrant_InvalidArgumentWithAgsDescription(t *testing.T) {
	rt := &fakeRoundTripper{
		resp: newJSONResponse(http.StatusBadRequest, `{
			"error":"invalid_grant",
			"error_description":"Discord code expired"
		}`),
	}
	svr := exchangeSvr(rt)

	_, err := svr.ExchangeDiscordCode(t.Context(), validExchangeReq())
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "Discord code expired")
}

func TestExchangeDiscordCode_AgsUnauthorizedClient_Internal(t *testing.T) {
	rt := &fakeRoundTripper{
		resp: newJSONResponse(http.StatusUnauthorized, `{
			"error":"unauthorized_client",
			"error_description":"client not allowed to use grant"
		}`),
	}
	svr := exchangeSvr(rt)

	_, err := svr.ExchangeDiscordCode(t.Context(), validExchangeReq())
	requireStatus(t, err, codes.Internal)
	// Must NOT leak the AGS description verbatim — it would surface
	// "client not allowed" to the player, which is unhelpful + leaks
	// upstream config detail.
	requireMsgContains(t, err, "misconfigured")
}

func TestExchangeDiscordCode_AgsServerError_Unavailable(t *testing.T) {
	rt := &fakeRoundTripper{
		resp: newJSONResponse(http.StatusBadGateway, `{"error":"server_error"}`),
	}
	svr := exchangeSvr(rt)

	_, err := svr.ExchangeDiscordCode(t.Context(), validExchangeReq())
	requireStatus(t, err, codes.Unavailable)
}

func TestExchangeDiscordCode_NetworkError_Unavailable(t *testing.T) {
	rt := &fakeRoundTripper{err: io.ErrUnexpectedEOF}
	svr := exchangeSvr(rt)

	_, err := svr.ExchangeDiscordCode(t.Context(), validExchangeReq())
	requireStatus(t, err, codes.Unavailable)
}

func TestExchangeDiscordCode_AgsResponseMissingAccessToken_Internal(t *testing.T) {
	rt := &fakeRoundTripper{
		resp: newJSONResponse(http.StatusOK, `{"refresh_token":"r","expires_in":60}`),
	}
	svr := exchangeSvr(rt)

	_, err := svr.ExchangeDiscordCode(t.Context(), validExchangeReq())
	requireStatus(t, err, codes.Internal)
	requireMsgContains(t, err, "access_token")
}

func TestExchangeDiscordCode_AgsResponseGarbledJSON_Internal(t *testing.T) {
	rt := &fakeRoundTripper{
		resp: newJSONResponse(http.StatusOK, `not-json`),
	}
	svr := exchangeSvr(rt)

	_, err := svr.ExchangeDiscordCode(t.Context(), validExchangeReq())
	requireStatus(t, err, codes.Internal)
}

// ---------------- safety: handler refuses to run with no proxy --------------

func TestExchangeDiscordCode_NotConfigured_Unavailable(t *testing.T) {
	svr, _, _ := newTestServer()
	_, err := svr.ExchangeDiscordCode(t.Context(), validExchangeReq())
	requireStatus(t, err, codes.Unavailable)
}

func TestExchangeDiscordCode_PartiallyConfigured_Unavailable(t *testing.T) {
	// Missing ClientSecret — handler must short-circuit before any HTTP
	// call (otherwise we'd post platform_token to AGS without auth and
	// burn the Discord code).
	svr, _, _ := newTestServer()
	svr = svr.WithDiscordExchangeProxy(DiscordExchangeProxy{
		AGSBaseURL: "https://ags.example.com",
		ClientID:   "confidential-client",
		HTTPClient: &http.Client{},
	})
	_, err := svr.ExchangeDiscordCode(t.Context(), validExchangeReq())
	requireStatus(t, err, codes.Unavailable)
}
