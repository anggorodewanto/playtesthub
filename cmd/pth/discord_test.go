package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// discordFixture wires a minimal authDeps + discordDeps pair pointed at
// fake servers, plus a captured-call hook for the browser opener and a
// scriptable paste reader for --manual. Same pattern as authFixture so a
// reader can switch between them without re-learning the conventions.
type discordFixture struct {
	t              *testing.T
	g              *Globals
	auth           *authDeps
	deps           *discordDeps
	exchangeServer *httptest.Server

	exchangeHandler http.HandlerFunc
	pasteReturn     string
	pasteErr        error
	openedURL       string
	stateValue      string
}

func newDiscordFixture(t *testing.T) *discordFixture {
	t.Helper()
	f := &discordFixture{
		t:           t,
		stateValue:  "test-state",
		pasteReturn: "",
	}
	f.exchangeHandler = func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req discordExchangeRequest
		_ = json.Unmarshal(body, &req)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  testDiscordAccessToken("user-discord-1"),
			"refresh_token": "RFR",
			"expires_in":    3600,
			"token_type":    "Bearer",
		})
		_ = req // captured by per-test wrappers via setExchangeHandler
	}
	f.exchangeServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.exchangeHandler(w, r)
	}))
	t.Cleanup(f.exchangeServer.Close)

	store := &credStore{path: filepath.Join(t.TempDir(), "playtesthub", "credentials.json")}
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	f.auth = &authDeps{
		store: store,
		now:   func() time.Time { return now },
	}
	port := pickFreePort(t)
	f.deps = &discordDeps{
		authDeps:           f.auth,
		DiscordClientID:    "discord-client-id",
		LoopbackPort:       port,
		BackendRESTBase:    f.exchangeServer.URL,
		BindLoopback:       func(addr string) (net.Listener, error) { return net.Listen("tcp", addr) },
		OpenBrowser:        func(u string) error { f.openedURL = u; return nil },
		ExchangeHTTPClient: f.exchangeServer.Client(),
		PasteReader:        func(string) (string, error) { return f.pasteReturn, f.pasteErr },
		StateGenerator:     func() (string, error) { return f.stateValue, nil },
	}
	f.g = &Globals{
		Addr:      "localhost:6565",
		Namespace: "test-ns",
		Profile:   "default",
		Timeout:   5 * time.Second,
	}
	return f
}

// pickFreePort grabs an unused TCP port for the loopback test. We bind a
// listener, capture its port, then immediately close it — there's a
// classic TOCTOU window before the test re-binds, but local test runs
// don't race for ports often enough for it to matter.
func pickFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pickFreePort: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

// testDiscordAccessToken builds an unsigned JWT whose `sub` claim is the
// requested user id. iam.DecodeSubject parses the middle segment without
// signature verification, so this is enough to drive the userId-from-JWT
// path without dragging in an HMAC secret.
func testDiscordAccessToken(sub string) string {
	header := base64URLNoPad(`{"alg":"none","typ":"JWT"}`)
	payload := base64URLNoPad(fmt.Sprintf(`{"sub":%q,"namespace":"test-ns"}`, sub))
	return header + "." + payload + "."
}

func base64URLNoPad(raw string) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	out := make([]byte, 0, (len(raw)*4)/3+4)
	var buf uint32
	bits := 0
	for i := 0; i < len(raw); i++ {
		buf = (buf << 8) | uint32(raw[i])
		bits += 8
		for bits >= 6 {
			bits -= 6
			out = append(out, alphabet[(buf>>bits)&0x3f])
		}
	}
	if bits > 0 {
		out = append(out, alphabet[(buf<<(6-bits))&0x3f])
	}
	return string(out)
}

// runLoopbackEnd2End drives a real discord login by spawning a goroutine
// that waits for the listener to come up, then GETs the loopback
// /callback URL with the right code+state. Blocking on the listener
// avoids the race where the goroutine hits 127.0.0.1:<port> before
// http.Server.Serve has accepted.
func runLoopbackEnd2End(t *testing.T, f *discordFixture, code, state string) (stdout, stderr string, exitCode int) {
	t.Helper()
	var done sync.WaitGroup
	done.Go(func() {
		hitLoopbackUntilReady(t, f.deps.LoopbackPort, code, state)
	})
	stdoutBuf, stderrBuf := &bytes.Buffer{}, &bytes.Buffer{}
	exit := runAuthLoginDiscord(context.Background(), stdoutBuf, stderrBuf, f.g, []string{}, f.deps)
	done.Wait()
	return stdoutBuf.String(), stderrBuf.String(), exit
}

func hitLoopbackUntilReady(t *testing.T, port int, code, state string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	loopbackURL := fmt.Sprintf("http://127.0.0.1:%d/callback?code=%s&state=%s", port, url.QueryEscape(code), url.QueryEscape(state))
	for time.Now().Before(deadline) {
		resp, err := http.Get(loopbackURL) //nolint:gosec,noctx // test-only loopback hit
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("hitLoopbackUntilReady: never connected to 127.0.0.1:%d", port)
}

// ---------------- URL builder ----------------

func TestBuildDiscordAuthorizeURL(t *testing.T) {
	got := buildDiscordAuthorizeURL("client-X", "http://127.0.0.1:14565/callback", "STATE-Y")
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if u.Host != "discord.com" || u.Path != "/oauth2/authorize" {
		t.Errorf("base url wrong: %s", got)
	}
	q := u.Query()
	if q.Get("response_type") != "code" {
		t.Errorf("response_type=%q", q.Get("response_type"))
	}
	if q.Get("client_id") != "client-X" {
		t.Errorf("client_id=%q", q.Get("client_id"))
	}
	if q.Get("redirect_uri") != "http://127.0.0.1:14565/callback" {
		t.Errorf("redirect_uri=%q", q.Get("redirect_uri"))
	}
	if q.Get("state") != "STATE-Y" {
		t.Errorf("state=%q", q.Get("state"))
	}
	if q.Get("scope") != discordLoginScope {
		t.Errorf("scope=%q want %q", q.Get("scope"), discordLoginScope)
	}
}

// ---------------- Required env precheck ----------------

func TestRunAuthLoginDiscordMissingClientID(t *testing.T) {
	f := newDiscordFixture(t)
	f.deps.DiscordClientID = ""
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runAuthLoginDiscord(context.Background(), stdout, stderr, f.g, nil, f.deps)
	if code != exitLocalError {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(stderr.String(), "PTH_DISCORD_CLIENT_ID") {
		t.Errorf("stderr=%s", stderr.String())
	}
}

func TestRunAuthLoginDiscordMissingBackendURL(t *testing.T) {
	f := newDiscordFixture(t)
	f.deps.BackendRESTBase = ""
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runAuthLoginDiscord(context.Background(), stdout, stderr, f.g, nil, f.deps)
	if code != exitLocalError {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(stderr.String(), "PTH_BACKEND_REST_URL") {
		t.Errorf("stderr=%s", stderr.String())
	}
}

func TestRunAuthLoginDiscordMissingNamespace(t *testing.T) {
	f := newDiscordFixture(t)
	f.g.Namespace = ""
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runAuthLoginDiscord(context.Background(), stdout, stderr, f.g, nil, f.deps)
	if code != exitLocalError {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(stderr.String(), "namespace") {
		t.Errorf("stderr=%s", stderr.String())
	}
}

// ---------------- Dry run ----------------

func TestRunAuthLoginDiscordDryRunLoopback(t *testing.T) {
	f := newDiscordFixture(t)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runAuthLoginDiscord(context.Background(), stdout, stderr, f.g, []string{"--dry-run"}, f.deps)
	if code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode stdout: %v: %s", err, stdout.String())
	}
	if out["mode"] != "loopback" {
		t.Errorf("mode=%v", out["mode"])
	}
	wantRedirect := fmt.Sprintf("http://127.0.0.1:%d/callback", f.deps.LoopbackPort)
	if out["redirectUri"] != wantRedirect {
		t.Errorf("redirectUri=%v want %s", out["redirectUri"], wantRedirect)
	}
	if !strings.Contains(out["authorizeUrl"].(string), "discord.com/oauth2/authorize") {
		t.Errorf("authorizeUrl=%v", out["authorizeUrl"])
	}
	if !strings.HasSuffix(out["exchangeUrl"].(string), discordExchangePath) {
		t.Errorf("exchangeUrl=%v", out["exchangeUrl"])
	}
	if f.openedURL != "" {
		t.Errorf("dry-run opened browser: %s", f.openedURL)
	}
}

func TestRunAuthLoginDiscordDryRunManualHasNoListener(t *testing.T) {
	f := newDiscordFixture(t)
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	code := runAuthLoginDiscord(context.Background(), stdout, stderr, f.g, []string{"--manual", "--dry-run"}, f.deps)
	if code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("decode stdout: %v: %s", err, stdout.String())
	}
	if out["mode"] != "manual" {
		t.Errorf("mode=%v", out["mode"])
	}
	if out["listenerAddr"] != "" {
		t.Errorf("manual mode should report empty listenerAddr; got %v", out["listenerAddr"])
	}
}

// ---------------- Loopback happy path ----------------

func TestRunAuthLoginDiscordLoopbackHappyPath(t *testing.T) {
	f := newDiscordFixture(t)
	var captured discordExchangeRequest
	f.exchangeHandler = func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  testDiscordAccessToken("user-discord-happy"),
			"refresh_token": "RFR",
			"expires_in":    3600,
			"token_type":    "Bearer",
		})
	}

	stdoutStr, stderrStr, exit := runLoopbackEnd2End(t, f, "DISCORD_CODE", f.stateValue)
	if exit != exitOK {
		t.Fatalf("exit=%d stderr=%s", exit, stderrStr)
	}
	if captured.Code != "DISCORD_CODE" {
		t.Errorf("backend got code=%q", captured.Code)
	}
	wantRedirect := fmt.Sprintf("http://127.0.0.1:%d/callback", f.deps.LoopbackPort)
	if captured.RedirectURI != wantRedirect {
		t.Errorf("backend got redirect_uri=%q want %s", captured.RedirectURI, wantRedirect)
	}
	if f.openedURL == "" {
		t.Errorf("browser opener was not called")
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(stdoutStr), &out); err != nil {
		t.Fatalf("decode stdout: %v: %s", err, stdoutStr)
	}
	if out["userId"] != "user-discord-happy" {
		t.Errorf("userId=%v", out["userId"])
	}
	if out["loginMode"] != "discord" {
		t.Errorf("loginMode=%v", out["loginMode"])
	}
	// Stored entry round-trips.
	stored, err := f.deps.store.getProfile(f.g.Profile, f.g.Addr, f.g.Namespace)
	if err != nil {
		t.Fatalf("getProfile: %v", err)
	}
	if stored == nil || stored.AccessToken == "" {
		t.Fatalf("profile not persisted: %+v", stored)
	}
	if stored.LoginMode != "discord" {
		t.Errorf("stored.LoginMode=%q", stored.LoginMode)
	}
}

func TestRunAuthLoginDiscordLoopbackNoBrowser(t *testing.T) {
	f := newDiscordFixture(t)
	var done sync.WaitGroup
	done.Go(func() {
		hitLoopbackUntilReady(t, f.deps.LoopbackPort, "C", f.stateValue)
	})
	stdoutBuf, stderrBuf := &bytes.Buffer{}, &bytes.Buffer{}
	exit := runAuthLoginDiscord(context.Background(), stdoutBuf, stderrBuf, f.g, []string{"--no-browser"}, f.deps)
	done.Wait()
	if exit != exitOK {
		t.Fatalf("exit=%d stderr=%s", exit, stderrBuf.String())
	}
	if f.openedURL != "" {
		t.Errorf("--no-browser should not invoke browser opener; got %s", f.openedURL)
	}
}

// ---------------- Loopback failure modes ----------------

func TestRunAuthLoginDiscordLoopbackStateMismatch(t *testing.T) {
	f := newDiscordFixture(t)
	stdoutStr, stderrStr, exit := runLoopbackEnd2End(t, f, "C", "WRONG-STATE")
	_ = stdoutStr
	if exit != exitLocalError {
		t.Fatalf("exit=%d stderr=%s", exit, stderrStr)
	}
	if !strings.Contains(stderrStr, "state mismatch") {
		t.Errorf("stderr=%s", stderrStr)
	}
}

func TestRunAuthLoginDiscordLoopbackMissingCode(t *testing.T) {
	f := newDiscordFixture(t)
	stdoutStr, stderrStr, exit := runLoopbackEnd2End(t, f, "", f.stateValue)
	_ = stdoutStr
	if exit != exitLocalError {
		t.Fatalf("exit=%d stderr=%s", exit, stderrStr)
	}
	if !strings.Contains(stderrStr, "missing ?code=") {
		t.Errorf("stderr=%s", stderrStr)
	}
}

func TestRunAuthLoginDiscordLoopbackBindFails(t *testing.T) {
	f := newDiscordFixture(t)
	f.deps.BindLoopback = func(string) (net.Listener, error) {
		return nil, errors.New("address already in use")
	}
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	exit := runAuthLoginDiscord(context.Background(), stdout, stderr, f.g, nil, f.deps)
	if exit != exitLocalError {
		t.Fatalf("exit=%d", exit)
	}
	if !strings.Contains(stderr.String(), "PTH_DISCORD_LOOPBACK_PORT") {
		t.Errorf("stderr=%s", stderr.String())
	}
}

// ---------------- Manual paste flow ----------------

func TestParsePastedRedirectFullURL(t *testing.T) {
	code, state, err := parsePastedRedirect("http://127.0.0.1:14565/callback?code=AAA&state=BBB")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if code != "AAA" || state != "BBB" {
		t.Errorf("code=%q state=%q", code, state)
	}
}

func TestParsePastedRedirectBareQuery(t *testing.T) {
	code, state, err := parsePastedRedirect("?code=AAA&state=BBB")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if code != "AAA" || state != "BBB" {
		t.Errorf("code=%q state=%q", code, state)
	}
}

func TestParsePastedRedirectError(t *testing.T) {
	_, _, err := parsePastedRedirect("http://127.0.0.1/callback?error=access_denied&error_description=user+declined")
	if err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Errorf("err=%v", err)
	}
}

func TestParsePastedRedirectEmpty(t *testing.T) {
	_, _, err := parsePastedRedirect("   ")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRunAuthLoginDiscordManualHappyPath(t *testing.T) {
	f := newDiscordFixture(t)
	f.pasteReturn = fmt.Sprintf("http://127.0.0.1:%d/callback?code=MANUAL_CODE&state=%s", f.deps.LoopbackPort, f.stateValue)
	var captured discordExchangeRequest
	f.exchangeHandler = func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  testDiscordAccessToken("user-manual"),
			"refresh_token": "RFR",
			"expires_in":    3600,
			"token_type":    "Bearer",
		})
	}
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	exit := runAuthLoginDiscord(context.Background(), stdout, stderr, f.g, []string{"--manual"}, f.deps)
	if exit != exitOK {
		t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
	}
	if captured.Code != "MANUAL_CODE" {
		t.Errorf("backend got code=%q", captured.Code)
	}
	if f.openedURL != "" {
		t.Errorf("manual flow should not open browser; got %s", f.openedURL)
	}
}

func TestRunAuthLoginDiscordManualStateMismatch(t *testing.T) {
	f := newDiscordFixture(t)
	f.pasteReturn = "?code=MANUAL_CODE&state=WRONG"
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	exit := runAuthLoginDiscord(context.Background(), stdout, stderr, f.g, []string{"--manual"}, f.deps)
	_ = stdout
	if exit != exitLocalError {
		t.Fatalf("exit=%d", exit)
	}
	if !strings.Contains(stderr.String(), "state mismatch") {
		t.Errorf("stderr=%s", stderr.String())
	}
}

// ---------------- Exchange POST failure modes ----------------

func TestRunAuthLoginDiscordExchange4xxIsClientError(t *testing.T) {
	f := newDiscordFixture(t)
	f.exchangeHandler = func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":"invalid_grant","error_description":"Invalid \"redirect_uri\""}`, http.StatusBadRequest)
	}
	_, stderr, exit := runLoopbackEnd2End(t, f, "C", f.stateValue)
	if exit != exitClientError {
		t.Fatalf("exit=%d stderr=%s", exit, stderr)
	}
	if !strings.Contains(stderr, "rejected exchange") {
		t.Errorf("stderr=%s", stderr)
	}
}

func TestRunAuthLoginDiscordExchange5xxIsTransport(t *testing.T) {
	f := newDiscordFixture(t)
	f.exchangeHandler = func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "AGS Discord call timed out", http.StatusServiceUnavailable)
	}
	_, stderr, exit := runLoopbackEnd2End(t, f, "C", f.stateValue)
	if exit != exitTransportError {
		t.Fatalf("exit=%d stderr=%s", exit, stderr)
	}
}

func TestRunAuthLoginDiscordExchangeNetworkErrorIsTransport(t *testing.T) {
	f := newDiscordFixture(t)
	f.exchangeServer.Close()
	_, stderr, exit := runLoopbackEnd2End(t, f, "C", f.stateValue)
	if exit != exitTransportError {
		t.Fatalf("exit=%d stderr=%s", exit, stderr)
	}
}

// ---------------- defaultDiscordDeps env parsing ----------------

func TestDefaultDiscordDepsEnvDefaults(t *testing.T) {
	auth := &authDeps{}
	getenv := func(k string) (string, bool) {
		switch k {
		case "PTH_DISCORD_CLIENT_ID":
			return "id-from-env", true
		case "PTH_BACKEND_REST_URL":
			return "https://example/base", true
		}
		return "", false
	}
	d, err := defaultDiscordDeps(auth, getenv)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if d.DiscordClientID != "id-from-env" {
		t.Errorf("client id=%q", d.DiscordClientID)
	}
	if d.BackendRESTBase != "https://example/base" {
		t.Errorf("backend rest=%q", d.BackendRESTBase)
	}
	if d.LoopbackPort != discordLoopbackPortDefault {
		t.Errorf("loopback port=%d want %d", d.LoopbackPort, discordLoopbackPortDefault)
	}
}

func TestDefaultDiscordDepsLoopbackPortOverride(t *testing.T) {
	auth := &authDeps{}
	getenv := func(k string) (string, bool) {
		if k == "PTH_DISCORD_LOOPBACK_PORT" {
			return "23456", true
		}
		return "", false
	}
	d, err := defaultDiscordDeps(auth, getenv)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if d.LoopbackPort != 23456 {
		t.Errorf("got %d", d.LoopbackPort)
	}
}

func TestDefaultDiscordDepsLoopbackPortInvalid(t *testing.T) {
	auth := &authDeps{}
	cases := []string{"abc", "0", "-1", "70000"}
	for _, v := range cases {
		t.Run(v, func(t *testing.T) {
			getenv := func(k string) (string, bool) {
				if k == "PTH_DISCORD_LOOPBACK_PORT" {
					return v, true
				}
				return "", false
			}
			_, err := defaultDiscordDeps(auth, getenv)
			if err == nil {
				t.Fatalf("expected error for %q", v)
			}
			if !strings.Contains(err.Error(), "PTH_DISCORD_LOOPBACK_PORT") {
				t.Errorf("err=%v", err)
			}
		})
	}
}
