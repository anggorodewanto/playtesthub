package iam

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// PlatformLookup fetches a player's Discord snowflake from AGS IAM. The
// AGS access token's `ipf=discord` claim tells us the caller federated
// via Discord, but the snowflake is not in the JWT — the admin endpoint
// `/iam/v3/admin/namespaces/{ns}/users/{userId}/distinctPlatforms` is
// the source of truth (see docs/ags-failure-modes.md).
type PlatformLookup interface {
	// GetDiscordID returns the Discord snowflake linked to agsUserID, or
	// "" if the user has no Discord platform link. agsUserID may be in
	// hyphenated UUID v4 form; AGS IAM requires hyphens stripped, so the
	// implementation does that internally.
	GetDiscordID(ctx context.Context, agsUserID string) (string, error)
}

// AGSAdminPlatformLookup talks to AGS IAM with the confidential
// server-side IAM client (the same creds used by DiscordExchangeProxy).
// A client-credentials access token is fetched lazily and cached until
// 60s before its `expires_in`; transient failures fall back to a fresh
// token on the next call.
type AGSAdminPlatformLookup struct {
	HTTPClient   *http.Client
	BaseURL      string
	Namespace    string
	ClientID     string
	ClientSecret string

	mu      sync.Mutex
	token   string
	expires time.Time
}

const platformLookupTokenSkew = 60 * time.Second

// AdminToken returns the cached client-credentials access token,
// minting a fresh one on first call or after expiry skew. Exposed for
// callers (e.g. pkg/adt HTTPClient) that need to attach the service
// JWT as Authorization: Bearer … on every outbound request — ADT
// validates it against AGS JWKS and derives studio identity from
// iss / union_namespace.
func (l *AGSAdminPlatformLookup) AdminToken(ctx context.Context) (string, error) {
	if l == nil {
		return "", fmt.Errorf("iam: platform lookup not configured")
	}
	return l.adminToken(ctx)
}

// GetStudioNamespace mints a client-credentials access token and
// extracts the studio identity from its claims as
// `union_namespace ?? namespace`. The token represents the playtesthub
// backend itself — NOT any calling admin — and is the canonical
// studio identity ADT sees on every downstream API call from
// playtesthub (PRD §4.8.2). Used by the ADT linkage flow to scope
// adt_linkage rows correctly. Returns an empty string + an error when
// neither claim is set on the service token (boot-time
// misconfiguration; the StartADTLink handler maps this to
// FailedPrecondition per errors.md).
func (l *AGSAdminPlatformLookup) GetStudioNamespace(ctx context.Context) (string, error) {
	if l == nil {
		return "", fmt.Errorf("iam: platform lookup not configured")
	}
	tok, err := l.adminToken(ctx)
	if err != nil {
		return "", err
	}
	claims, err := DecodeClaims(tok)
	if err != nil {
		return "", fmt.Errorf("iam: decode service token claims: %w", err)
	}
	if s := claims.StudioNamespace(); s != "" {
		return s, nil
	}
	return "", fmt.Errorf("iam: service token carries neither union_namespace nor namespace claim")
}

// GetDiscordID implements PlatformLookup against AGS IAM.
func (l *AGSAdminPlatformLookup) GetDiscordID(ctx context.Context, agsUserID string) (string, error) {
	if l == nil {
		return "", fmt.Errorf("iam: platform lookup not configured")
	}
	if agsUserID == "" {
		return "", fmt.Errorf("iam: empty user id")
	}

	tok, err := l.adminToken(ctx)
	if err != nil {
		return "", err
	}

	// AGS rejects hyphenated UUIDs on user-path params with errorCode
	// 20002 ("not valid uuid v4 without hyphen format"). Strip them.
	stripped := strings.ReplaceAll(agsUserID, "-", "")
	endpoint := strings.TrimRight(l.BaseURL, "/") +
		"/iam/v3/admin/namespaces/" + url.PathEscape(l.Namespace) +
		"/users/" + url.PathEscape(stripped) + "/distinctPlatforms"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("iam: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")

	resp, err := l.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("iam: distinctPlatforms: %w", err)
	}
	defer drainBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("iam: distinctPlatforms %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out struct {
		Platforms []struct {
			PlatformID     string `json:"platformId"`
			PlatformName   string `json:"platformName"`
			PlatformUserID string `json:"platformUserId"`
		} `json:"platforms"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("iam: decode distinctPlatforms: %w", err)
	}
	for _, p := range out.Platforms {
		if strings.EqualFold(p.PlatformName, "discord") || strings.EqualFold(p.PlatformID, "discord") {
			return p.PlatformUserID, nil
		}
	}
	return "", nil
}

// adminToken returns a cached client-credentials access token, fetching
// a fresh one when missing or within the expiry skew. Concurrent callers
// share the in-flight fetch via the mutex — one token per process is
// plenty since this is a low-QPS path.
func (l *AGSAdminPlatformLookup) adminToken(ctx context.Context) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.token != "" && time.Now().Before(l.expires.Add(-platformLookupTokenSkew)) {
		return l.token, nil
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")

	endpoint := strings.TrimRight(l.BaseURL, "/") + "/iam/v3/oauth/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", fmt.Errorf("iam: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.SetBasicAuth(l.ClientID, l.ClientSecret)

	resp, err := l.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("iam: client-credentials: %w", err)
	}
	defer drainBody(resp.Body)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("iam: client-credentials %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("iam: decode token: %w", err)
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("iam: client-credentials response missing access_token")
	}
	l.token = out.AccessToken
	l.expires = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second)
	return l.token, nil
}

func drainBody(rc io.ReadCloser) {
	if rc == nil {
		return
	}
	_, _ = io.Copy(io.Discard, rc)
	_ = rc.Close()
}
