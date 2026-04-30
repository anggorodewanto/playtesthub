package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	testUserID     = "user-1"
	testNewRefresh = "NEW_REFRESH"
)

// newAuthFixture wires an authDeps + Globals pair backed by a real
// httptest IAM server, a real on-disk credentials store under t.TempDir,
// a fixed clock, and stubs for password readers. Tests pick what they
// override (passwords, IAM responses, time) and leave the rest alone.
type authFixture struct {
	t       *testing.T
	g       *Globals
	deps    *authDeps
	srv     *httptest.Server
	store   *credStore
	now     time.Time
	pwTTY   string
	pwStdin string
}

func newAuthFixture(t *testing.T) *authFixture {
	t.Helper()
	f := &authFixture{
		t:       t,
		now:     time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
		pwTTY:   "tty-pw",
		pwStdin: "stdin-pw",
	}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "ACCESS",
			"refresh_token": "REFRESH",
			"expires_in":    3600,
			"token_type":    "Bearer",
			"user_id":       testUserID,
			"namespace":     "test-ns",
		})
	}))
	t.Cleanup(f.srv.Close)
	f.store = &credStore{path: filepath.Join(t.TempDir(), "playtesthub", "credentials.json")}
	f.deps = &authDeps{
		store:         f.store,
		iam:           &iamClient{BaseURL: f.srv.URL, ClientID: "cli", HTTPClient: f.srv.Client()},
		now:           func() time.Time { return f.now },
		readPassword:  func(string) (string, error) { return f.pwTTY, nil },
		stdinPassword: func() (string, error) { return f.pwStdin, nil },
	}
	f.g = &Globals{
		Addr:      "localhost:6565",
		Namespace: "test-ns",
		Profile:   "default",
		Timeout:   5 * time.Second,
	}
	return f
}

func (f *authFixture) setIAMHandler(h http.HandlerFunc) {
	f.srv.Close()
	f.srv = httptest.NewServer(h)
	f.deps.iam = &iamClient{BaseURL: f.srv.URL, ClientID: "cli", HTTPClient: f.srv.Client()}
	f.t.Cleanup(f.srv.Close)
}

func TestRunAuthLoginPasswordHappyPath(t *testing.T) {
	f := newAuthFixture(t)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runAuthLogin(context.Background(), stdout, stderr, f.g, []string{"--password", "--username", "alice"}, f.deps)
	if code != exitOK {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode stdout: %v: %s", err, stdout.String())
	}
	if out["userId"] != testUserID || out["loginMode"] != "password" {
		t.Errorf("out=%v", out)
	}
	p, err := f.store.getProfile("default", "localhost:6565", "test-ns")
	if err != nil || p == nil {
		t.Fatalf("store.getProfile: %v %v", p, err)
	}
	if p.AccessToken != "ACCESS" || p.RefreshToken != "REFRESH" || p.UserID != testUserID {
		t.Errorf("stored profile=%+v", p)
	}
	if !p.ExpiresAt.Equal(f.now.Add(time.Hour)) {
		t.Errorf("ExpiresAt=%s", p.ExpiresAt)
	}
}

func TestRunAuthLoginRequiresNamespace(t *testing.T) {
	f := newAuthFixture(t)
	f.g.Namespace = ""
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runAuthLogin(context.Background(), stdout, stderr, f.g, []string{"--password", "--username", "alice"}, f.deps)
	if code != exitLocalError {
		t.Fatalf("exit=%d, want %d (stderr=%s)", code, exitLocalError, stderr.String())
	}
	if !strings.Contains(stderr.String(), "namespace") {
		t.Errorf("stderr missing namespace remediation: %s", stderr.String())
	}
}

func TestRunAuthLoginRequiresPasswordFlag(t *testing.T) {
	f := newAuthFixture(t)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runAuthLogin(context.Background(), stdout, stderr, f.g, []string{"--username", "alice"}, f.deps)
	if code != exitLocalError {
		t.Fatalf("exit=%d, want %d", code, exitLocalError)
	}
}

func TestRunAuthLoginInvalidGrantExitsClientError(t *testing.T) {
	f := newAuthFixture(t)
	f.setIAMHandler(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":             "invalid_grant",
			"error_description": "Bad credentials",
		})
	})
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runAuthLogin(context.Background(), stdout, stderr, f.g, []string{"--password", "--username", "alice"}, f.deps)
	if code != exitClientError {
		t.Fatalf("exit=%d want %d", code, exitClientError)
	}
	if !strings.Contains(stderr.String(), "Bad credentials") {
		t.Errorf("stderr missing description: %s", stderr.String())
	}
}

func TestRunAuthLoginPasswordStdinReadsFromStdinHook(t *testing.T) {
	f := newAuthFixture(t)
	f.pwStdin = "stdin-secret"
	gotForm := ""
	f.setIAMHandler(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		gotForm = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "A", "refresh_token": "R", "expires_in": 60,
		})
	})
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runAuthLogin(context.Background(), stdout, stderr, f.g, []string{"--password", "--username", "alice", "--password-stdin"}, f.deps)
	if code != exitOK {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(gotForm, "password=stdin-secret") {
		t.Errorf("form did not carry stdin password: %s", gotForm)
	}
}

func TestRunAuthLogoutRemovesProfile(t *testing.T) {
	f := newAuthFixture(t)
	if err := f.store.putProfile("default", &profileEntry{Addr: "localhost:6565", Namespace: "test-ns", AccessToken: "T"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runAuthLogout(stdout, stderr, f.g, nil, f.deps)
	if code != exitOK {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var out map[string]any
	_ = json.Unmarshal(stdout.Bytes(), &out)
	if out["removed"] != true {
		t.Errorf("removed=%v", out["removed"])
	}
}

func TestRunAuthWhoamiPrintsStoredProfile(t *testing.T) {
	f := newAuthFixture(t)
	if err := f.store.putProfile("default", &profileEntry{
		Addr: "localhost:6565", Namespace: "test-ns", UserID: "uX", LoginMode: "password",
		AccessToken: "T", ExpiresAt: f.now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runAuthWhoami(context.Background(), stdout, stderr, f.g, nil, f.deps)
	if code != exitOK {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["userId"] != "uX" || out["namespace"] != "test-ns" {
		t.Errorf("out=%v", out)
	}
}

func TestRunAuthWhoamiNoProfileExitsLocalError(t *testing.T) {
	f := newAuthFixture(t)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runAuthWhoami(context.Background(), stdout, stderr, f.g, nil, f.deps)
	if code != exitLocalError {
		t.Fatalf("exit=%d want %d", code, exitLocalError)
	}
	if !strings.Contains(stderr.String(), "no credential") {
		t.Errorf("stderr=%s", stderr.String())
	}
}

func TestRunAuthTokenPrintsBearerOnly(t *testing.T) {
	f := newAuthFixture(t)
	if err := f.store.putProfile("default", &profileEntry{
		Addr: "localhost:6565", Namespace: "test-ns",
		AccessToken: "BEARER_VALUE", ExpiresAt: f.now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runAuthToken(context.Background(), stdout, stderr, f.g, nil, f.deps)
	if code != exitOK {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if got := strings.TrimSpace(stdout.String()); got != "BEARER_VALUE" {
		t.Errorf("stdout=%q want BEARER_VALUE", got)
	}
}

func TestResolveActiveProfileRefreshesNearExpiry(t *testing.T) {
	f := newAuthFixture(t)
	// Stored token expires in 30s; with a 60s leeway, it must refresh.
	if err := f.store.putProfile("default", &profileEntry{
		Addr: "localhost:6565", Namespace: "test-ns", LoginMode: "password",
		AccessToken: "OLD", RefreshToken: "OLD_REFRESH", ExpiresAt: f.now.Add(30 * time.Second),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	gotForm := ""
	f.setIAMHandler(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotForm = string(buf)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "NEW", "refresh_token": testNewRefresh, "expires_in": 3600,
		})
	})
	p, err := resolveActiveProfile(context.Background(), f.g, f.deps)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.AccessToken != "NEW" || p.RefreshToken != testNewRefresh {
		t.Errorf("profile=%+v", p)
	}
	if !strings.Contains(gotForm, "grant_type=refresh_token") || !strings.Contains(gotForm, "refresh_token=OLD_REFRESH") {
		t.Errorf("refresh form=%s", gotForm)
	}
	persisted, err := f.store.getProfile("default", "localhost:6565", "test-ns")
	if err != nil {
		t.Fatalf("getProfile: %v", err)
	}
	if persisted.AccessToken != "NEW" || persisted.RefreshToken != testNewRefresh {
		t.Errorf("did not persist: %+v", persisted)
	}
}

func TestResolveActiveProfileRefreshFailureSurfacesRemediation(t *testing.T) {
	f := newAuthFixture(t)
	if err := f.store.putProfile("default", &profileEntry{
		Addr: "localhost:6565", Namespace: "test-ns",
		AccessToken: "OLD", RefreshToken: "EXPIRED", ExpiresAt: f.now.Add(-time.Minute),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	f.setIAMHandler(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant", "error_description": "stale"})
	})
	_, err := resolveActiveProfile(context.Background(), f.g, f.deps)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "rejected by AGS IAM") || !strings.Contains(err.Error(), "auth login --password") {
		t.Errorf("err missing remediation: %v", err)
	}
}

func TestResolveActiveProfileFreshTokenSkipsRefresh(t *testing.T) {
	f := newAuthFixture(t)
	if err := f.store.putProfile("default", &profileEntry{
		Addr: "localhost:6565", Namespace: "test-ns",
		AccessToken: "FRESH", RefreshToken: "R", ExpiresAt: f.now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	called := false
	f.setIAMHandler(func(http.ResponseWriter, *http.Request) { called = true })
	p, err := resolveActiveProfile(context.Background(), f.g, f.deps)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.AccessToken != "FRESH" {
		t.Errorf("token=%s", p.AccessToken)
	}
	if called {
		t.Error("IAM was called for a fresh token")
	}
}

func TestResolveActiveProfileMismatchedAddrReturnsNil(t *testing.T) {
	f := newAuthFixture(t)
	if err := f.store.putProfile("default", &profileEntry{Addr: "other:1", Namespace: "test-ns", AccessToken: "T"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	p, err := resolveActiveProfile(context.Background(), f.g, f.deps)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p != nil {
		t.Errorf("profile=%+v want nil", p)
	}
}

func TestNeedsRefreshBoundary(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		expiresIn time.Duration
		want      bool
	}{
		{"expired 1s ago", -time.Second, true},
		{"within leeway", refreshLeeway / 2, true},
		{"exactly at leeway", refreshLeeway, true},
		{"beyond leeway", refreshLeeway + time.Second, false},
		{"far future", time.Hour, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &profileEntry{ExpiresAt: now.Add(tc.expiresIn)}
			if got := needsRefresh(p, now); got != tc.want {
				t.Errorf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestNeedsRefreshZeroExpiryNeverRefreshes(t *testing.T) {
	if needsRefresh(&profileEntry{}, time.Now()) {
		t.Error("zero expiry should never refresh")
	}
}

func TestReadSingleLineTrimsNewline(t *testing.T) {
	got, err := readSingleLine(strings.NewReader("hunter2\nignored-line\n"))
	if err != nil {
		t.Fatalf("readSingleLine: %v", err)
	}
	if got != "hunter2" {
		t.Errorf("got %q want hunter2", got)
	}
}

func TestReadSingleLineNoTrailingNewline(t *testing.T) {
	got, err := readSingleLine(strings.NewReader("hunter2"))
	if err != nil {
		t.Fatalf("readSingleLine: %v", err)
	}
	if got != "hunter2" {
		t.Errorf("got %q want hunter2", got)
	}
}

func TestReportAuthFailureIsTransportOn5xx(t *testing.T) {
	stderr := &bytes.Buffer{}
	code := reportAuthFailure(stderr, "p", &iamError{StatusCode: 503, Code: "server_error"})
	if code != exitTransportError {
		t.Errorf("code=%d", code)
	}
}

func TestReportAuthFailureNonStatusErrorIsTransport(t *testing.T) {
	stderr := &bytes.Buffer{}
	code := reportAuthFailure(stderr, "p", errors.New("dial tcp: refused"))
	if code != exitTransportError {
		t.Errorf("code=%d", code)
	}
}

func TestRunAuthLoginNoArgsActionRequired(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runAuth(context.Background(), stdout, stderr, &Globals{Profile: "default"}, nil, func(string) (string, bool) { return "", false })
	if code != exitLocalError {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(stderr.String(), "action required") {
		t.Errorf("stderr=%s", stderr.String())
	}
}

func TestRunAuthDiscordIsDeferredToPhase103(t *testing.T) {
	f := newAuthFixture(t)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runAuthLogin(context.Background(), stdout, stderr, f.g, []string{"--discord"}, f.deps)
	if code != exitLocalError {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(stderr.String(), "phase 10.3") {
		t.Errorf("stderr=%s", stderr.String())
	}
}

func TestDefaultCredStorePathRespectsOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "alt", "creds.json")
	t.Setenv("PTH_CREDENTIALS_FILE", override)
	got, err := defaultCredStorePath()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != override {
		t.Errorf("got %s want %s", got, override)
	}
}

func TestDefaultCredStorePathRespectsXDG(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("PTH_CREDENTIALS_FILE", "")
	t.Setenv("XDG_CONFIG_HOME", xdg)
	got, err := defaultCredStorePath()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if want := filepath.Join(xdg, "playtesthub", "credentials.json"); got != want {
		t.Errorf("got %s want %s", got, want)
	}
}

func TestRunAuthLoginEmptyPasswordIsLocalError(t *testing.T) {
	f := newAuthFixture(t)
	f.pwStdin = ""
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runAuthLogin(context.Background(), stdout, stderr, f.g, []string{"--password", "--username", "alice", "--password-stdin"}, f.deps)
	if code != exitLocalError {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(stderr.String(), "empty password") {
		t.Errorf("stderr=%s", stderr.String())
	}
}

// Sanity: argv must NEVER carry the password literal — auth.go reads
// from TTY or stdin, not flags. This test catches a regression where
// someone adds a `--password <value>` flag.
func TestNoPasswordValueFlag(t *testing.T) {
	contents, err := os.ReadFile("auth.go")
	if err != nil {
		t.Fatalf("read auth.go: %v", err)
	}
	body := string(contents)
	// Look for a flag definition that takes a string password value.
	if strings.Contains(body, `fs.String("password",`) {
		t.Error("auth.go declares --password as a string flag — passwords must come from stdin or TTY")
	}
}
