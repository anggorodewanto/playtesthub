package adt_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anggorodewanto/playtesthub/pkg/adt"
)

// bearerSvcJWT is the literal Authorization header value the tests
// assert against — pulled out so goconst stays quiet.
const bearerSvcJWT = "Bearer svc-jwt"

func tokenGetterReturning(tok string) adt.TokenGetter {
	return func(_ context.Context) (string, error) { return tok, nil }
}

func newTestClient(t *testing.T, srv *httptest.Server, token adt.TokenGetter) *adt.HTTPClient {
	t.Helper()
	c := adt.NewHTTPClient(srv.URL, srv.Client(), token)
	c.Policy = adt.RetryPolicy{
		MaxAttempts:       4,
		PerAttemptTimeout: 2 * time.Second,
		InitialBackoff:    0,
		MaxBackoff:        0,
		Sleep:             func(time.Duration) {},
	}
	return c
}

// TestHTTPClient_ListBuilds_RejectsLegacyEnvelope locks the live ADT
// envelope key. ADT returns `{"data": [...]}` (verified against the
// 2026-05-21 live swagger v1.35.0); a server replying with the legacy
// `{"builds": [...]}` shape must NOT silently surface rows — Bug 1 from
// the 2026-05-21 probe report.
func TestHTTPClient_ListBuilds_RejectsLegacyEnvelope(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"builds": []map[string]any{
				{"id": "build-legacy", "game_version_name": "v1", "game_version_id": "x", "created_at": "2026-05-20T12:00:00Z", "platform_name": "windows"},
			},
		})
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv, tokenGetterReturning("svc-jwt"))
	builds, err := c.ListBuilds(context.Background(), "studio-ns", "adt-ns", "game-1")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(builds) != 0 {
		t.Fatalf("legacy `builds` envelope should be ignored — len=%d, want 0", len(builds))
	}
}

// TestHTTPClient_ListGames_RejectsLegacyEnvelope locks the live ADT
// envelope key for the games endpoint. Mirrors the builds regression
// (Bug 2 from the 2026-05-21 probe).
func TestHTTPClient_ListGames_RejectsLegacyEnvelope(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"games": []map[string]any{
				{"id": "game-legacy", "name": "Old", "created_at": "2026-05-21T10:00:00Z"},
			},
		})
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv, tokenGetterReturning("svc-jwt"))
	games, err := c.ListGames(context.Background(), "studio-ns", "adt-ns")
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if len(games) != 0 {
		t.Fatalf("legacy `games` envelope should be ignored — len=%d, want 0", len(games))
	}
}

func TestHTTPClient_ListBuilds_HappyPath(t *testing.T) {
	t.Parallel()
	var capturedAuth string
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{
					"id":                "build-1",
					"game_version_name": "v1.2.3",
					"game_version_id":   "abc",
					"build_name":        "ignored-build-name",
					"created_at":        "2026-05-20T12:00:00Z",
					"platform_name":     "windows",
				},
			},
		})
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv, tokenGetterReturning("svc-jwt"))
	builds, err := c.ListBuilds(context.Background(), "studio-ns", "adt-ns", "game-1")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(builds) != 1 {
		t.Fatalf("len(builds) = %d, want 1", len(builds))
	}
	if builds[0].ID != "build-1" || builds[0].Name != "v1.2.3" || builds[0].Version != "abc" || builds[0].Platform != "windows" {
		t.Errorf("build = %+v", builds[0])
	}
	if builds[0].UploadedAt.IsZero() {
		t.Errorf("UploadedAt zero, want parsed")
	}
	if capturedAuth != bearerSvcJWT {
		t.Errorf("Authorization = %q", capturedAuth)
	}
	if !strings.Contains(capturedPath, "/profiling/namespaces/adt-ns/agsplaytesthub/games/game-1/builds") {
		t.Errorf("path = %q", capturedPath)
	}
}

func TestHTTPClient_ListBuilds_401MapsToLinkageMissing(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"linkage missing"}`))
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv, tokenGetterReturning("svc-jwt"))
	_, err := c.ListBuilds(context.Background(), "studio-ns", "adt-ns", "game-1")
	if !errors.Is(err, adt.ErrLinkageMissing) {
		t.Fatalf("err = %v, want ErrLinkageMissing", err)
	}
}

func TestHTTPClient_ListBuilds_429MapsToRateLimited(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv, tokenGetterReturning("svc-jwt"))
	_, err := c.ListBuilds(context.Background(), "studio-ns", "adt-ns", "game-1")
	if !errors.Is(err, adt.ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
}

func TestHTTPClient_ListBuilds_5xxRetriesThenExhausts(t *testing.T) {
	t.Parallel()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv, tokenGetterReturning("svc-jwt"))
	_, err := c.ListBuilds(context.Background(), "studio-ns", "adt-ns", "game-1")
	if !errors.Is(err, adt.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	if got := atomic.LoadInt32(&calls); got != 4 {
		t.Fatalf("calls = %d, want 4 (1 + 3 retries)", got)
	}
}

func TestHTTPClient_IssueDownloadURL_HappyPath(t *testing.T) {
	t.Parallel()
	expiry := "2026-05-21T00:00:00Z"
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path + "?" + r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{
			"download_urls": []map[string]any{
				{"url": "https://cdn.example/build.zip", "expires_at": expiry},
			},
		})
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv, tokenGetterReturning("svc-jwt"))
	got, err := c.IssueDownloadURL(context.Background(), adt.IssueDownloadURLParams{
		StudioNamespace: "studio-ns",
		ADTNamespace:    "adt-ns",
		ADTGameID:       "game-1",
		ADTBuildID:      "build-1",
		ApplicantIdent:  "app-1",
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got.URL != "https://cdn.example/build.zip" {
		t.Errorf("URL = %q", got.URL)
	}
	if got.ExpiresAt.IsZero() {
		t.Errorf("ExpiresAt zero, want parsed")
	}
	if !strings.Contains(capturedPath, "/profiling/namespaces/adt-ns/agsplaytesthub/games/game-1/builds/build-1/downloadUrls") {
		t.Errorf("path = %q", capturedPath)
	}
	if !strings.Contains(capturedPath, "limit=20") {
		t.Errorf("missing limit=20 in path = %q", capturedPath)
	}
}

func TestHTTPClient_IssueDownloadURL_EmptyListIsClientError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"download_urls":[]}`))
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv, tokenGetterReturning("svc-jwt"))
	_, err := c.IssueDownloadURL(context.Background(), adt.IssueDownloadURLParams{ADTNamespace: "n", ADTGameID: "g", ADTBuildID: "b"})
	if !adt.IsClientError(err) {
		t.Fatalf("err = %v, want ClientError", err)
	}
}

func TestHTTPClient_ListGames_HappyPath(t *testing.T) {
	t.Parallel()
	var capturedAuth, capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "game-1", "name": "Aces", "created_at": "2026-05-21T10:00:00Z"},
			},
		})
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv, tokenGetterReturning("svc-jwt"))
	games, err := c.ListGames(context.Background(), "studio-ns", "adt-ns")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(games) != 1 {
		t.Fatalf("len(games) = %d, want 1", len(games))
	}
	if games[0].ID != "game-1" || games[0].Name != "Aces" {
		t.Errorf("game = %+v", games[0])
	}
	if games[0].CreatedAt.IsZero() {
		t.Errorf("CreatedAt zero, want parsed")
	}
	if capturedAuth != bearerSvcJWT {
		t.Errorf("Authorization = %q", capturedAuth)
	}
	if !strings.Contains(capturedPath, "/profiling/namespaces/adt-ns/agsplaytesthub/games") {
		t.Errorf("path = %q", capturedPath)
	}
}

func TestHTTPClient_ListGames_401MapsToLinkageMissing(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv, tokenGetterReturning("svc-jwt"))
	_, err := c.ListGames(context.Background(), "studio-ns", "adt-ns")
	if !errors.Is(err, adt.ErrLinkageMissing) {
		t.Fatalf("err = %v, want ErrLinkageMissing", err)
	}
}

func TestHTTPClient_ListGames_429MapsToRateLimited(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv, tokenGetterReturning("svc-jwt"))
	_, err := c.ListGames(context.Background(), "studio-ns", "adt-ns")
	if !errors.Is(err, adt.ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
}

func TestHTTPClient_ListGames_5xxRetriesThenExhausts(t *testing.T) {
	t.Parallel()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv, tokenGetterReturning("svc-jwt"))
	_, err := c.ListGames(context.Background(), "studio-ns", "adt-ns")
	if !errors.Is(err, adt.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	if got := atomic.LoadInt32(&calls); got != 4 {
		t.Fatalf("calls = %d, want 4 (1 + 3 retries)", got)
	}
}

func TestHTTPClient_ListGames_TokenGetterFailurePropagates(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("ADT should not be called when token getter fails")
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv, func(_ context.Context) (string, error) {
		return "", fmt.Errorf("ags down")
	})
	_, err := c.ListGames(context.Background(), "s", "n")
	if !errors.Is(err, adt.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable (token failure)", err)
	}
}

func TestHTTPClient_DeleteLinkage_HappyPath(t *testing.T) {
	t.Parallel()
	var capturedAuth, capturedPath, capturedMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedPath = r.URL.Path
		capturedMethod = r.Method
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv, tokenGetterReturning("svc-jwt"))
	if err := c.DeleteLinkage(context.Background(), "studio-ns", "adt-ns"); err != nil {
		t.Fatalf("err = %v", err)
	}
	if capturedMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", capturedMethod)
	}
	if capturedAuth != bearerSvcJWT {
		t.Errorf("Authorization = %q", capturedAuth)
	}
	if !strings.Contains(capturedPath, "/profiling/namespaces/adt-ns/agsplaytesthub/linkage") {
		t.Errorf("path = %q", capturedPath)
	}
}

func TestHTTPClient_DeleteLinkage_401MapsToLinkageMissing(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv, tokenGetterReturning("svc-jwt"))
	err := c.DeleteLinkage(context.Background(), "studio-ns", "adt-ns")
	if !errors.Is(err, adt.ErrLinkageMissing) {
		t.Fatalf("err = %v, want ErrLinkageMissing", err)
	}
}

func TestHTTPClient_DeleteLinkage_5xxRetriesThenExhausts(t *testing.T) {
	t.Parallel()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv, tokenGetterReturning("svc-jwt"))
	err := c.DeleteLinkage(context.Background(), "studio-ns", "adt-ns")
	if !errors.Is(err, adt.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
	if got := atomic.LoadInt32(&calls); got != 4 {
		t.Fatalf("calls = %d, want 4 (1 + 3 retries)", got)
	}
}

func TestHTTPClient_DeleteLinkage_TokenGetterFailurePropagates(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("ADT should not be called when token getter fails")
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv, func(_ context.Context) (string, error) {
		return "", fmt.Errorf("ags down")
	})
	err := c.DeleteLinkage(context.Background(), "s", "n")
	if !errors.Is(err, adt.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable (token failure)", err)
	}
}

func TestHTTPClient_TokenGetterFailurePropagates(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("ADT should not be called when token getter fails")
	}))
	t.Cleanup(srv.Close)

	c := newTestClient(t, srv, func(_ context.Context) (string, error) {
		return "", fmt.Errorf("ags down")
	})
	// classify maps unknown errors to ErrUnavailable on retry exhaustion.
	_, err := c.ListBuilds(context.Background(), "s", "n", "g")
	if !errors.Is(err, adt.ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable (token failure)", err)
	}
}
