package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testAdminAccessToken is the access-token literal stored in the
// pre-seeded admin profile, and testAdminBearer is the matching
// Authorization header value. Both are lifted to constants so the test
// names + assertions stay grep-friendly without lint noise.
const (
	testAdminAccessToken = "ADMIN_TOKEN"
	testAdminBearer      = "Bearer " + testAdminAccessToken
)

// newUserFixture wires an authDeps + Globals + admin-credentialed profile
// stored on disk. The shared httptest server is a single multiplexed
// handler the per-test setup overrides via setHandler — keeps the fixture
// constructor cheap and the per-test wiring explicit.
type userFixture struct {
	t       *testing.T
	g       *Globals
	deps    *authDeps
	srv     *httptest.Server
	store   *credStore
	now     time.Time
	pwStdin string
	pwTTY   string
}

func newUserFixture(t *testing.T) *userFixture {
	t.Helper()
	f := &userFixture{
		t:       t,
		now:     time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC),
		pwStdin: "stdin-pw",
		pwTTY:   "tty-pw",
	}
	// 404 by default — every test installs its own handler before dialling.
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
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
		Profile:   "admin",
		Timeout:   5 * time.Second,
	}
	// Admin profile pre-stored: every user-group call needs a bearer.
	if err := f.store.putProfile("admin", &profileEntry{
		Addr:        "localhost:6565",
		Namespace:   "test-ns",
		UserID:      "admin-1",
		LoginMode:   loginModePassword,
		AccessToken: testAdminAccessToken,
		ExpiresAt:   f.now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("seeding admin profile: %v", err)
	}
	return f
}

func (f *userFixture) setHandler(h http.HandlerFunc) {
	f.srv.Close()
	f.srv = httptest.NewServer(h)
	f.deps.iam = &iamClient{BaseURL: f.srv.URL, ClientID: "cli", HTTPClient: f.srv.Client()}
	f.t.Cleanup(f.srv.Close)
}

func TestUserCreateRequiresAction(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runUser(context.Background(), stdout, stderr, &Globals{}, nil, nilEnv)
	if code != exitLocalError {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(stderr.String(), "action required") {
		t.Errorf("stderr=%q", stderr.String())
	}
}

func TestUserUnknownAction(t *testing.T) {
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runUser(context.Background(), stdout, stderr, &Globals{}, []string{"frobnicate"}, nilEnv)
	if code != exitLocalError || !strings.Contains(stderr.String(), "unknown action") {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
}

func TestUserCreateHappyPath(t *testing.T) {
	f := newUserFixture(t)
	gotMethod, gotPath, gotAuth := "", "", ""
	gotBody := map[string]any{}
	f.setHandler(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{
					"userId":       testIAMUserID,
					"username":     "ags-gen-name-1",
					"password":     "ags-gen-pw-1",
					"emailAddress": "u1@test.example",
					"namespace":    "test-ns",
				},
			},
		})
	})

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runUserCreate(context.Background(), stdout, stderr, f.g, []string{"--country", "ID"}, f.deps)
	if code != exitOK {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method=%s", gotMethod)
	}
	if gotPath != "/iam/v4/admin/namespaces/test-ns/test_users" {
		t.Errorf("path=%s", gotPath)
	}
	if gotAuth != testAdminBearer {
		t.Errorf("authorization=%q", gotAuth)
	}
	if gotBody["count"] != float64(1) {
		t.Errorf("count=%v", gotBody["count"])
	}
	ui, _ := gotBody["userInfo"].(map[string]any)
	if ui["country"] != "ID" {
		t.Errorf("country=%v", ui["country"])
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode stdout: %v: %s", err, stdout.String())
	}
	if out["userId"] != testIAMUserID || out["password"] != "ags-gen-pw-1" || out["username"] != "ags-gen-name-1" {
		t.Errorf("stdout=%v", out)
	}
}

func TestUserCreateCountReturnsArray(t *testing.T) {
	f := newUserFixture(t)
	f.setHandler(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"userId": testIAMUserID, "username": "n1", "password": "p1", "emailAddress": "e1", "namespace": "test-ns"},
				{"userId": "u-2", "username": "n2", "password": "p2", "emailAddress": "e2", "namespace": "test-ns"},
			},
		})
	})
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runUserCreate(context.Background(), stdout, stderr, f.g, []string{"--count", "2"}, f.deps)
	if code != exitOK {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var out []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode stdout: %v: %s", err, stdout.String())
	}
	if len(out) != 2 || out[0]["userId"] != testIAMUserID || out[1]["userId"] != "u-2" {
		t.Errorf("stdout=%v", out)
	}
}

func TestUserCreateRejectsBadCount(t *testing.T) {
	f := newUserFixture(t)
	for _, n := range []string{"0", "-1", "101"} {
		stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
		code := runUserCreate(context.Background(), stdout, stderr, f.g, []string{"--count", n}, f.deps)
		if code != exitLocalError {
			t.Errorf("count=%s exit=%d stderr=%q", n, code, stderr.String())
		}
	}
}

func TestUserCreateRequiresNamespace(t *testing.T) {
	f := newUserFixture(t)
	f.g.Namespace = ""
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runUserCreate(context.Background(), stdout, stderr, f.g, nil, f.deps)
	if code != exitLocalError || !strings.Contains(stderr.String(), "namespace") {
		t.Errorf("exit=%d stderr=%q", code, stderr.String())
	}
}

func TestUserCreateDryRunDoesNotDial(t *testing.T) {
	f := newUserFixture(t)
	f.setHandler(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("dry-run must not dial")
	})
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runUserCreate(context.Background(), stdout, stderr, f.g, []string{"--dry-run", "--count", "3"}, f.deps)
	if code != exitOK {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode stdout: %v: %s", err, stdout.String())
	}
	if out["method"] != "POST" || !strings.Contains(out["path"].(string), "test-ns/test_users") {
		t.Errorf("dry-run out=%v", out)
	}
}

func TestUserCreateNoAdminProfile(t *testing.T) {
	f := newUserFixture(t)
	f.g.Profile = "missing"
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runUserCreate(context.Background(), stdout, stderr, f.g, nil, f.deps)
	if code != exitLocalError {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(stderr.String(), "no credential") {
		t.Errorf("stderr=%q", stderr.String())
	}
}

func TestUserDeleteRequiresYes(t *testing.T) {
	f := newUserFixture(t)
	f.setHandler(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("must not dial without --yes")
	})
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runUserDelete(context.Background(), stdout, stderr, f.g, []string{"--id", testIAMUserID}, f.deps)
	if code != exitLocalError || !strings.Contains(stderr.String(), "--yes") {
		t.Errorf("exit=%d stderr=%q", code, stderr.String())
	}
}

func TestUserDeleteHappyPath(t *testing.T) {
	f := newUserFixture(t)
	gotMethod, gotPath, gotAuth := "", "", ""
	f.setHandler(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	})
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runUserDelete(context.Background(), stdout, stderr, f.g, []string{"--id", testIAMUserID, "--yes"}, f.deps)
	if code != exitOK {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method=%s", gotMethod)
	}
	if gotPath != "/iam/v3/admin/namespaces/test-ns/users/u-1/information" {
		t.Errorf("path=%s", gotPath)
	}
	if gotAuth != testAdminBearer {
		t.Errorf("authorization=%q", gotAuth)
	}
	var out map[string]any
	_ = json.Unmarshal(stdout.Bytes(), &out)
	if out["userId"] != testIAMUserID || out["deleted"] != true {
		t.Errorf("stdout=%v", out)
	}
}

func TestUserDeleteRequiresID(t *testing.T) {
	f := newUserFixture(t)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runUserDelete(context.Background(), stdout, stderr, f.g, []string{"--yes"}, f.deps)
	if code != exitLocalError || !strings.Contains(stderr.String(), "--id is required") {
		t.Errorf("exit=%d stderr=%q", code, stderr.String())
	}
}

func TestUserDeleteDryRunDoesNotDial(t *testing.T) {
	f := newUserFixture(t)
	f.setHandler(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("dry-run must not dial")
	})
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runUserDelete(context.Background(), stdout, stderr, f.g, []string{"--dry-run", "--id", testIAMUserID}, f.deps)
	if code != exitOK {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var out map[string]any
	_ = json.Unmarshal(stdout.Bytes(), &out)
	if out["method"] != "DELETE" || !strings.Contains(out["path"].(string), "users/u-1/information") {
		t.Errorf("dry-run out=%v", out)
	}
}

func TestUserLoginAsHappyPath(t *testing.T) {
	f := newUserFixture(t)
	type call struct {
		method, path, auth string
		body               []byte
	}
	var calls []call
	f.setHandler(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, call{r.Method, r.URL.Path, r.Header.Get("Authorization"), body})
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/iam/v3/admin/namespaces/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"userId":    "u-target",
				"userName":  "ags-gen-name",
				"namespace": "test-ns",
			})
		case r.URL.Path == iamTokenPath:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "TARGET_ACCESS",
				"refresh_token": "TARGET_REFRESH",
				"expires_in":    3600,
				"token_type":    "Bearer",
				"user_id":       "u-target",
				"namespace":     "test-ns",
			})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
	f.g.Profile = "target"
	// admin profile keyed under "admin" still resolvable via explicit lookup
	// — but since runUserLoginAs uses g.Profile for both bearer + storage,
	// we need an admin-credential under "target". Re-seed.
	if err := f.store.putProfile("target", &profileEntry{
		Addr: "localhost:6565", Namespace: "test-ns", UserID: "admin-1",
		LoginMode: loginModePassword, AccessToken: testAdminAccessToken, ExpiresAt: f.now.Add(time.Hour),
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runUserLoginAs(context.Background(), stdout, stderr, f.g, []string{"--id", "u-target", "--password-stdin"}, f.deps)
	if code != exitOK {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if len(calls) != 2 {
		t.Fatalf("calls=%d", len(calls))
	}
	if calls[0].method != http.MethodGet || calls[0].path != "/iam/v3/admin/namespaces/test-ns/users/u-target" {
		t.Errorf("lookup call=%+v", calls[0])
	}
	if calls[0].auth != testAdminBearer {
		t.Errorf("lookup auth=%q", calls[0].auth)
	}
	if calls[1].method != http.MethodPost || calls[1].path != iamTokenPath {
		t.Errorf("token call=%+v", calls[1])
	}
	if !strings.Contains(string(calls[1].body), "username=ags-gen-name") {
		t.Errorf("token body=%s", calls[1].body)
	}
	if !strings.Contains(string(calls[1].body), "password=stdin-pw") {
		t.Errorf("token body=%s", calls[1].body)
	}
	p, err := f.store.getProfile("target", "localhost:6565", "test-ns")
	if err != nil || p == nil {
		t.Fatalf("store.getProfile: %v %v", p, err)
	}
	if p.AccessToken != "TARGET_ACCESS" || p.RefreshToken != "TARGET_REFRESH" || p.UserID != "u-target" {
		t.Errorf("stored profile=%+v", p)
	}
}

func TestUserLoginAsRequiresID(t *testing.T) {
	f := newUserFixture(t)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runUserLoginAs(context.Background(), stdout, stderr, f.g, []string{"--password-stdin"}, f.deps)
	if code != exitLocalError || !strings.Contains(stderr.String(), "--id is required") {
		t.Errorf("exit=%d stderr=%q", code, stderr.String())
	}
}

func TestUserLoginAsDryRunDoesNotDial(t *testing.T) {
	f := newUserFixture(t)
	f.setHandler(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("dry-run must not dial")
	})
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runUserLoginAs(context.Background(), stdout, stderr, f.g, []string{"--dry-run", "--id", "u-target"}, f.deps)
	if code != exitOK {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var out map[string]any
	_ = json.Unmarshal(stdout.Bytes(), &out)
	if !strings.Contains(out["lookupPath"].(string), "users/u-target") {
		t.Errorf("dry-run out=%v", out)
	}
}
