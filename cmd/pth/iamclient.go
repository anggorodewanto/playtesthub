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

// adminCreateTestUsersPath is the AGS IAM v4 "create test users" endpoint.
// AGS generates the username/password/email itself and marks the rows as
// test users — distinct from production rows, no verification email sent.
// We use this rather than `/admin/namespaces/{ns}/users` because the e2e
// surface only needs ephemeral throwaway accounts, and pinning a username
// on the production endpoint requires injecting fake country/dob defaults.
const adminCreateTestUsersPath = "/iam/v4/admin/namespaces/%s/test_users"

// adminGetUserPath is the v3 admin "get user by id" endpoint. Used by
// `pth user login-as` to resolve the AGS userId → username before running
// the ROPC grant (which requires a username).
const adminGetUserPath = "/iam/v3/admin/namespaces/%s/users/%s"

// adminDeleteUserPath is the v3 admin "delete user information" endpoint.
// The /iam/namespaces/.../users/{id} (no `v3`) variant is deprecated; AGS
// docs forward callers to /information.
const adminDeleteUserPath = "/iam/v3/admin/namespaces/%s/users/%s/information"

// adminCreateTestUsersRequest mirrors the JSON body for the v4 test_users
// endpoint. count is required (max 100 per AGS); userInfo.country is the
// only field AGS lets us pin — username/password/email are generated.
type adminCreateTestUsersRequest struct {
	Count    int                `json:"count"`
	UserInfo *adminTestUserInfo `json:"userInfo,omitempty"`
}

type adminTestUserInfo struct {
	Country string `json:"country,omitempty"`
}

// adminCreateTestUsersResponse mirrors the AGS shape `{data: [...]}`.
type adminCreateTestUsersResponse struct {
	Data []*adminCreateTestUserResponse `json:"data"`
}

// adminCreateTestUserResponse is one entry of the `data` array. AGS
// returns AuthType/Country/DateOfBirth/DisplayName too — the CLI only
// surfaces identity + the generated password.
type adminCreateTestUserResponse struct {
	UserID       string `json:"userId"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	EmailAddress string `json:"emailAddress"`
	Namespace    string `json:"namespace"`
}

// adminGetUserResponse covers the v3 admin GET-user fields login-as needs
// (just the username; UserID is round-tripped to surface mismatches).
type adminGetUserResponse struct {
	UserID    string `json:"userId"`
	Username  string `json:"userName"`
	Namespace string `json:"namespace"`
}

// adminCreateTestUsers POSTs a test-user creation request as the supplied
// bearer (an admin JWT). The bearer is required — admin endpoints reject
// anonymous callers with 401.
func (c *iamClient) adminCreateTestUsers(ctx context.Context, bearer, namespace string, body *adminCreateTestUsersRequest) (*adminCreateTestUsersResponse, error) {
	if err := c.requireAdminConfig(bearer, namespace); err != nil {
		return nil, err
	}
	if body == nil || body.Count <= 0 {
		return nil, fmt.Errorf("count must be >= 1")
	}
	target := strings.TrimRight(c.BaseURL, "/") + fmt.Sprintf(adminCreateTestUsersPath, url.PathEscape(namespace))
	respBody, err := c.doAdminJSON(ctx, http.MethodPost, target, bearer, body)
	if err != nil {
		return nil, err
	}
	var out adminCreateTestUsersResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("decoding admin create-test-users response: %w", err)
	}
	if len(out.Data) == 0 {
		return nil, fmt.Errorf("admin create-test-users response missing data array")
	}
	for i, u := range out.Data {
		if u.UserID == "" {
			return nil, fmt.Errorf("admin create-test-users response entry %d missing userId", i)
		}
	}
	return &out, nil
}

// adminGetUserByID resolves a userId → user record. Used by login-as to
// translate the caller-supplied id to the username AGS ROPC needs.
func (c *iamClient) adminGetUserByID(ctx context.Context, bearer, namespace, userID string) (*adminGetUserResponse, error) {
	if err := c.requireAdminConfig(bearer, namespace); err != nil {
		return nil, err
	}
	if userID == "" {
		return nil, fmt.Errorf("user id is empty")
	}
	target := strings.TrimRight(c.BaseURL, "/") + fmt.Sprintf(adminGetUserPath, url.PathEscape(namespace), url.PathEscape(userID))
	respBody, err := c.doAdminJSON(ctx, http.MethodGet, target, bearer, nil)
	if err != nil {
		return nil, err
	}
	var out adminGetUserResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, fmt.Errorf("decoding admin get-user response: %w", err)
	}
	if out.Username == "" {
		return nil, fmt.Errorf("admin get-user response missing userName")
	}
	return &out, nil
}

// adminDeleteUser sends DELETE to the user-information endpoint. A 204 is
// the success shape; 404 is mapped through the iamError path so callers
// can surface a clear "no such user" message.
func (c *iamClient) adminDeleteUser(ctx context.Context, bearer, namespace, userID string) error {
	if err := c.requireAdminConfig(bearer, namespace); err != nil {
		return err
	}
	if userID == "" {
		return fmt.Errorf("user id is empty")
	}
	target := strings.TrimRight(c.BaseURL, "/") + fmt.Sprintf(adminDeleteUserPath, url.PathEscape(namespace), url.PathEscape(userID))
	_, err := c.doAdminJSON(ctx, http.MethodDelete, target, bearer, nil)
	return err
}

// requireAdminConfig is the shared precondition check for admin REST
// calls — every admin path needs the base URL, an admin bearer, and a
// namespace.
func (c *iamClient) requireAdminConfig(bearer, namespace string) error {
	if c == nil {
		return fmt.Errorf("iam client not configured")
	}
	if c.BaseURL == "" {
		return fmt.Errorf("AGS base URL not configured (set --ags-base-url or PTH_AGS_BASE_URL)")
	}
	if bearer == "" {
		return fmt.Errorf("admin token required (run: pth auth login --password as an admin user)")
	}
	if namespace == "" {
		return fmt.Errorf("namespace required (set --namespace or PTH_NAMESPACE)")
	}
	return nil
}

// doAdminJSON is the shared transport for admin REST calls: JSON in, JSON
// out (or empty body on 204), bearer auth, parseIAMError on >=400. body
// may be nil for GET/DELETE.
func (c *iamClient) doAdminJSON(ctx context.Context, method, target, bearer string, body any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("encoding admin request: %w", err)
		}
		reader = strings.NewReader(string(buf))
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, fmt.Errorf("building admin request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("AGS IAM unreachable: %w", err)
	}
	defer drainAndClose(resp.Body)
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading AGS IAM response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, parseIAMError(resp.StatusCode, respBody)
	}
	return respBody, nil
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
