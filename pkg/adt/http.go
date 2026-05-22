package adt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// TokenGetter mints (or returns from cache) the AGS service IAM JWT
// attached to every ADT API call. Implemented by
// iam.AGSAdminPlatformLookup.AdminToken in production; tests can pass
// any function with the same shape.
type TokenGetter func(ctx context.Context) (string, error)

// HTTPClient is the live ADT Client per the 2026-05-20 spec
// (pkg/adt/client.go comment block):
//
//	GET <base>/profiling/namespaces/<ns>/agsplaytesthub/games/<gid>/builds
//	GET <base>/profiling/namespaces/<ns>/agsplaytesthub/games/<gid>/builds/<bid>/downloadUrls
//	DELETE <base>/profiling/namespaces/<ns>/agsplaytesthub/linkage
//
// Auth: every request carries the playtesthub AGS service IAM JWT
// (Authorization: Bearer …); ADT validates against AGS JWKS and reads
// studio identity from iss / union_namespace. No separate credential
// exchange. See STATUS_M5.md D2.
//
// Retries: per-call shouldRetry/classify is delegated to RetryPolicy.
type HTTPClient struct {
	BaseURL   string
	HTTP      *http.Client
	Token     TokenGetter
	Policy    RetryPolicy
	UserAgent string
}

// NewHTTPClient constructs the live adapter. baseURL is origin only
// (no path); http defaults to a 30s-timeout client when nil; policy
// defaults to DefaultRetryPolicy when zero-valued. Token must be
// non-nil — without it, ADT returns 401 / ErrLinkageMissing on every
// request and the operator gets no useful signal.
func NewHTTPClient(baseURL string, httpClient *http.Client, token TokenGetter) *HTTPClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &HTTPClient{
		BaseURL:   strings.TrimRight(baseURL, "/"),
		HTTP:      httpClient,
		Token:     token,
		Policy:    DefaultRetryPolicy(),
		UserAgent: "playtesthub-adt/1",
	}
}

// httpStatusError is the internal error type returned to RetryPolicy
// from request roundtrips. It satisfies HTTPStatusCarrier so classify
// can map status → sentinel, and ADTErrorCodeCarrier so classify can
// dispatch on the live ADT errorCode envelope when one is present
// (Bug 4 / 2026-05-21 probe).
type httpStatusError struct {
	status       int
	op           string
	body         string
	errorCode    int
	errorMessage string
}

func (e *httpStatusError) Error() string {
	if e.errorCode != 0 {
		return fmt.Sprintf("adt: %s: http %d: errorCode=%d %s", e.op, e.status, e.errorCode, e.errorMessage)
	}
	return fmt.Sprintf("adt: %s: http %d: %s", e.op, e.status, e.body)
}

func (e *httpStatusError) HTTPStatus() int { return e.status }

// ADTErrorCode returns the JSON `errorCode` field surfaced in the
// response body. Zero means "absent" (e.g. plaintext mux-level 404 /
// 405 from an unknown sub-path). Satisfies ADTErrorCodeCarrier so
// classify can dispatch on it before falling through to the HTTP
// status.
func (e *httpStatusError) ADTErrorCode() int { return e.errorCode }

// ListGames implements Client.ListGames against the live ADT API.
// Endpoint per the 2026-05-21 ADT-eng addendum (STATUS_M5.md):
//
//	GET <BaseURL>/profiling/namespaces/<adtNamespace>/agsplaytesthub/games
func (c *HTTPClient) ListGames(ctx context.Context, studioNamespace, adtNamespace string) ([]Game, error) {
	_ = studioNamespace // ADT scopes the linkage flag server-side from the bearer token.
	if c.BaseURL == "" {
		return nil, fmt.Errorf("adt: HTTPClient BaseURL is empty")
	}
	endpoint := c.BaseURL + "/profiling/namespaces/" + url.PathEscape(adtNamespace) + "/agsplaytesthub/games"

	var raw struct {
		Data []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			CreatedAt string `json:"created_at"`
		} `json:"data"`
	}
	if err := c.Policy.Run(ctx, "ListGames", func(attemptCtx context.Context) error {
		return c.doJSON(attemptCtx, http.MethodGet, endpoint, "ListGames", &raw)
	}); err != nil {
		return nil, err
	}
	out := make([]Game, 0, len(raw.Data))
	for _, g := range raw.Data {
		var createdAt time.Time
		if g.CreatedAt != "" {
			if t, err := time.Parse(time.RFC3339, g.CreatedAt); err == nil {
				createdAt = t
			}
		}
		out = append(out, Game{ID: g.ID, Name: g.Name, CreatedAt: createdAt})
	}
	return out, nil
}

// ListBuilds implements Client.ListBuilds against the live ADT API.
func (c *HTTPClient) ListBuilds(ctx context.Context, studioNamespace, adtNamespace, adtGameID string) ([]Build, error) {
	if c.BaseURL == "" {
		return nil, fmt.Errorf("adt: HTTPClient BaseURL is empty")
	}
	endpoint := c.BaseURL + "/profiling/namespaces/" + url.PathEscape(adtNamespace) +
		"/agsplaytesthub/games/" + url.PathEscape(adtGameID) + "/builds"

	var raw struct {
		Data []struct {
			ID              string `json:"id"`
			GameVersionName string `json:"game_version_name"`
			GameVersionID   string `json:"game_version_id"`
			CreatedAt       string `json:"created_at"`
			PlatformName    string `json:"platform_name"`
		} `json:"data"`
	}
	if err := c.Policy.Run(ctx, "ListBuilds", func(attemptCtx context.Context) error {
		return c.doJSON(attemptCtx, http.MethodGet, endpoint, "ListBuilds", &raw)
	}); err != nil {
		return nil, err
	}
	out := make([]Build, 0, len(raw.Data))
	for _, b := range raw.Data {
		var uploaded time.Time
		if b.CreatedAt != "" {
			if t, err := time.Parse(time.RFC3339, b.CreatedAt); err == nil {
				uploaded = t
			}
		}
		out = append(out, Build{
			ID:         b.ID,
			Name:       b.GameVersionName,
			Version:    b.GameVersionID,
			UploadedAt: uploaded,
			Platform:   b.PlatformName,
		})
	}
	return out, nil
}

// IssueDownloadURL implements Client.IssueDownloadURL. ADT returns a
// list of download URLs (one per build asset); the full list is
// surfaced in IssuedDownloadURL.URLs in ADT's original order so
// multi-asset builds round-trip without data loss. ApplicantIdent is
// forwarded as a query param for ADT-side audit attribution but ADT
// does not scope the URLs by it.
func (c *HTTPClient) IssueDownloadURL(ctx context.Context, params IssueDownloadURLParams) (IssuedDownloadURL, error) {
	if c.BaseURL == "" {
		return IssuedDownloadURL{}, fmt.Errorf("adt: HTTPClient BaseURL is empty")
	}
	endpoint := c.BaseURL + "/profiling/namespaces/" + url.PathEscape(params.ADTNamespace) +
		"/agsplaytesthub/games/" + url.PathEscape(params.ADTGameID) +
		"/builds/" + url.PathEscape(params.ADTBuildID) + "/downloadUrls?limit=20"

	var raw struct {
		URLs      []string `json:"urls"`
		ExpiresAt string   `json:"expires_at"`
	}
	if err := c.Policy.Run(ctx, "IssueDownloadURL", func(attemptCtx context.Context) error {
		return c.doJSON(attemptCtx, http.MethodGet, endpoint, "IssueDownloadURL", &raw)
	}); err != nil {
		return IssuedDownloadURL{}, err
	}
	if len(raw.URLs) == 0 {
		return IssuedDownloadURL{}, &ClientError{StatusCode: http.StatusNotFound, Op: "IssueDownloadURL", Message: "no download urls returned"}
	}
	var expires time.Time
	if raw.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, raw.ExpiresAt); err == nil {
			expires = t
		}
	}
	urls := make([]string, len(raw.URLs))
	copy(urls, raw.URLs)
	return IssuedDownloadURL{URLs: urls, ExpiresAt: expires}, nil
}

// DeleteLinkage best-effort drops the ADT-side linkage flag — the
// unlink half of B4 / PRD §4.8. studioNamespace is for interface
// symmetry + unit-test bookkeeping; ADT derives studio identity from
// the bearer token. Best-effort: callers (service.UnlinkADT) log +
// metric the error but still finish the local soft-delete so an ADT
// outage cannot strand an operator who wants to drop their own row.
func (c *HTTPClient) DeleteLinkage(ctx context.Context, studioNamespace, adtNamespace string) error {
	_ = studioNamespace
	if c.BaseURL == "" {
		return fmt.Errorf("adt: HTTPClient BaseURL is empty")
	}
	endpoint := c.BaseURL + "/profiling/namespaces/" + url.PathEscape(adtNamespace) + "/agsplaytesthub/linkage"
	return c.Policy.Run(ctx, "DeleteLinkage", func(attemptCtx context.Context) error {
		return c.doJSON(attemptCtx, http.MethodDelete, endpoint, "DeleteLinkage", nil)
	})
}

// doJSON runs one HTTP attempt: builds the request, attaches the bearer
// token, executes via c.HTTP, and decodes a 2xx JSON body into dst (or
// drains the body when dst is nil). Non-2xx returns *httpStatusError so
// RetryPolicy can classify the status.
func (c *HTTPClient) doJSON(ctx context.Context, method, endpoint, op string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return fmt.Errorf("adt: build %s request: %w", op, err)
	}
	if c.Token == nil {
		return fmt.Errorf("adt: %s: token getter not configured", op)
	}
	tok, err := c.Token(ctx)
	if err != nil {
		return fmt.Errorf("adt: %s: minting service token: %w", op, err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("adt: %s: http do: %w", op, err)
	}
	defer drainBody(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return newHTTPStatusError(resp.StatusCode, op, resp.Header.Get("Content-Type"), body)
	}
	if dst == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		return fmt.Errorf("adt: %s: decode response: %w", op, err)
	}
	return nil
}

func drainBody(rc io.ReadCloser) {
	if rc == nil {
		return
	}
	_, _ = io.Copy(io.Discard, rc)
	_ = rc.Close()
}

// newHTTPStatusError builds the typed error returned by doJSON on a
// non-2xx response. Content-Type-aware: a JSON body
// (`application/json…`) is parsed for the live ADT `{errorCode,
// errorMessage}` envelope (Bug 4 fix); plaintext bodies (mux-level 404
// / 405) are kept as-is so classify can fall back to the HTTP status.
// Either way the raw body is preserved on .body for log readability.
func newHTTPStatusError(status int, op, contentType string, body []byte) *httpStatusError {
	trimmed := strings.TrimSpace(string(body))
	e := &httpStatusError{status: status, op: op, body: trimmed}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "application/json") {
		return e
	}
	var env struct {
		ErrorCode    int    `json:"errorCode"`
		ErrorMessage string `json:"errorMessage"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return e
	}
	e.errorCode = env.ErrorCode
	e.errorMessage = env.ErrorMessage
	return e
}
