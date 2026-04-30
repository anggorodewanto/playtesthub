package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/anggorodewanto/playtesthub/pkg/iam"
)

// iamTokenPath is the AGS IAM v3 token endpoint, shared by the
// password-grant + refresh-token-grant flows. ROPC sets
// `grant_type=password`; refresh sets `grant_type=refresh_token`.
//
// Path is the same one the backend's IAM SDK targets; we POST directly
// from the CLI rather than dragging in the SDK because we need byte-level
// control over the form body and the response field set.
const iamTokenPath = "/iam/v3/oauth/token"

// iamClient is the CLI's narrow AGS IAM HTTP client. Confidential clients
// pass ClientSecret; public clients leave it empty (the request still
// sets HTTP Basic with an empty password, which AGS accepts when the
// client is registered as public).
type iamClient struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
	HTTPClient   *http.Client
}

// iamToken is the parsed subset of an AGS IAM token response. ExpiresAt
// is computed at the call site so the credentials store can compare
// against `time.Now()` without re-doing the math on every refresh check.
type iamToken struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	ExpiresAt    time.Time
	UserID       string
	Namespace    string
}

// iamTokenResponse mirrors the AGS IAM token-endpoint JSON. The set of
// fields here is intentionally narrow — AGS includes many more
// (`platform_id`, `roles`, etc.) but the CLI only needs identity +
// rotation material.
type iamTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	UserID       string `json:"user_id"`
	Namespace    string `json:"namespace"`
}

// iamErrorResponse covers AGS IAM's two error shapes: the OAuth2
// {error, error_description} form (typical on token-endpoint failures)
// and AGS's {errorCode, errorMessage} envelope (used by some non-OAuth
// endpoints).
type iamErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
	ErrorCode        int    `json:"errorCode"`
	ErrorMessage     string `json:"errorMessage"`
}

// iamError is the rich error type returned by iamClient calls. It carries
// enough structure that the auth subcommand can map to exit codes / user
// remediation without re-parsing the wire body.
type iamError struct {
	StatusCode  int
	Code        string // OAuth2 `error` field (e.g. "invalid_grant")
	Description string // OAuth2 `error_description` if present
	Raw         string // full body when nothing else parses
}

func (e *iamError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("AGS IAM %d %s: %s", e.StatusCode, e.Code, e.Description)
	}
	if e.Code != "" {
		return fmt.Sprintf("AGS IAM %d %s", e.StatusCode, e.Code)
	}
	if e.Raw != "" {
		return fmt.Sprintf("AGS IAM %d: %s", e.StatusCode, truncate(e.Raw, 200))
	}
	return fmt.Sprintf("AGS IAM %d", e.StatusCode)
}

// IsInvalidGrant is the user-actionable case (bad credentials, expired
// refresh token). Auth subcommands surface a clear remediation when
// this fires; everything else is treated as a transport / config bug.
func (e *iamError) IsInvalidGrant() bool {
	return e != nil && e.Code == "invalid_grant"
}

// passwordLogin runs the OAuth2 ROPC grant against AGS IAM. The username
// + password go into the form body; the client identity goes into HTTP
// Basic. The namespace is supplied for the `namespace` form field that
// AGS uses to route the token within the multi-namespace tenant.
func (c *iamClient) passwordLogin(ctx context.Context, namespace, username, password string, now func() time.Time) (*iamToken, error) {
	if c == nil {
		return nil, fmt.Errorf("iam client not configured")
	}
	if c.BaseURL == "" {
		return nil, fmt.Errorf("AGS base URL not configured (set --ags-base-url or PTH_AGS_BASE_URL)")
	}
	if c.ClientID == "" {
		return nil, fmt.Errorf("AGS IAM client id not configured (set --client-id or PTH_IAM_CLIENT_ID)")
	}
	form := url.Values{}
	form.Set("grant_type", "password")
	form.Set("username", username)
	form.Set("password", password)
	if namespace != "" {
		form.Set("namespace", namespace)
	}
	return c.postToken(ctx, form, now)
}

// refresh runs the OAuth2 refresh-token grant. AGS rotates the refresh
// token on every call, so the caller must persist the new value or risk
// a future refresh failing.
func (c *iamClient) refresh(ctx context.Context, refreshToken string, now func() time.Time) (*iamToken, error) {
	if c == nil {
		return nil, fmt.Errorf("iam client not configured")
	}
	if c.BaseURL == "" {
		return nil, fmt.Errorf("AGS base URL not configured (set --ags-base-url or PTH_AGS_BASE_URL)")
	}
	if refreshToken == "" {
		return nil, fmt.Errorf("refresh token is empty")
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	return c.postToken(ctx, form, now)
}

func (c *iamClient) postToken(ctx context.Context, form url.Values, now func() time.Time) (*iamToken, error) {
	tokenURL := strings.TrimRight(c.BaseURL, "/") + iamTokenPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("building token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(c.ClientID, c.ClientSecret)

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("AGS IAM unreachable: %w", err)
	}
	defer drainAndClose(resp.Body)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading AGS IAM response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, parseIAMError(resp.StatusCode, body)
	}
	var raw iamTokenResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decoding AGS IAM token response: %w", err)
	}
	if raw.AccessToken == "" {
		return nil, fmt.Errorf("AGS IAM token response missing access_token")
	}
	tokenType := raw.TokenType
	if tokenType == "" {
		tokenType = "Bearer"
	}
	tok := &iamToken{
		AccessToken:  raw.AccessToken,
		RefreshToken: raw.RefreshToken,
		TokenType:    tokenType,
		ExpiresAt:    now().Add(time.Duration(raw.ExpiresIn) * time.Second),
		UserID:       raw.UserID,
		Namespace:    raw.Namespace,
	}
	if tok.UserID == "" {
		// Fall back to the JWT `sub` claim — every AGS IAM access token
		// carries one and we already have a JWT decoder in pkg/iam, but
		// the CLI doesn't import it (avoids dragging the SDK into a thin
		// client binary). Decode locally to keep the import surface
		// small.
		if sub, err := iam.DecodeSubject(raw.AccessToken); err == nil {
			tok.UserID = sub
		}
	}
	return tok, nil
}

func parseIAMError(statusCode int, body []byte) error {
	e := &iamError{StatusCode: statusCode, Raw: string(body)}
	var parsed iamErrorResponse
	if err := json.Unmarshal(body, &parsed); err == nil {
		e.Code = parsed.Error
		e.Description = parsed.ErrorDescription
		if e.Code == "" && parsed.ErrorMessage != "" {
			e.Code = fmt.Sprintf("ags_%d", parsed.ErrorCode)
			e.Description = parsed.ErrorMessage
		}
	}
	return e
}

func drainAndClose(rc io.ReadCloser) {
	if rc == nil {
		return
	}
	_, _ = io.Copy(io.Discard, rc)
	_ = rc.Close()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
