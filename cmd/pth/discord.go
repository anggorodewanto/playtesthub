package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/anggorodewanto/playtesthub/pkg/iam"
)

// discordLoopbackPortDefault is the port the loopback listener binds when
// PTH_DISCORD_LOOPBACK_PORT is unset. Must match the value the operator
// registered on Discord's developer portal + AGS Admin Portal — see
// docs/runbooks/setup-ags-discord.md § "CLI loopback origin". Fixed (not
// ephemeral) because Discord's redirect-URI allowlist is byte-exact.
const discordLoopbackPortDefault = 14565

// discordLoginScope mirrors the player's `DISCORD_LOGIN_SCOPE` so the CLI
// requests the same Discord scopes the backend expects when AGS calls
// /api/oauth2/token to redeem the code (PRD §5.2).
const discordLoginScope = "identify email"

// discordAuthorizeURL is the Discord OAuth2 authorize endpoint. Hard-coded
// rather than configurable: there is exactly one Discord, and overriding
// this would only enable a man-in-the-middle on a confidential CLI flow.
const discordAuthorizeURL = "https://discord.com/oauth2/authorize"

// discordExchangePath is the playtesthub backend's grpc-gateway REST
// mapping for ExchangeDiscordCode (proto/playtesthub/v1/playtesthub.proto).
const discordExchangePath = "/v1/player/discord/exchange"

// discordExchangeRequest matches the proto JSON shape with snake_case
// field names. grpc-gateway accepts these as the `body: "*"` payload.
type discordExchangeRequest struct {
	Code        string `json:"code"`
	RedirectURI string `json:"redirect_uri"`
}

// discordExchangeResponse mirrors ExchangeDiscordCodeResponse. The CLI
// only needs a subset (no token_type beyond "Bearer") but unmarshalling
// the full set future-proofs against AGS / backend additions.
type discordExchangeResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

// discordDeps gathers the Discord-flow seams. Keeping them on a separate
// struct (rather than expanding authDeps) leaves runAuthLoginPassword
// untouched and lets tests substitute one HTTP listener / browser opener
// without rebuilding the whole authDeps tree.
type discordDeps struct {
	*authDeps

	DiscordClientID    string
	LoopbackPort       int
	BackendRESTBase    string
	BindLoopback       func(addr string) (net.Listener, error)
	OpenBrowser        func(url string) error
	ExchangeHTTPClient *http.Client
	PasteReader        func(prompt string) (string, error)
	StateGenerator     func() (string, error)
}

// defaultDiscordDeps wires the production seams: a real TCP listener
// factory, the platform-appropriate browser opener, and a 30s-timeout
// HTTP client for the backend exchange POST. Env vars per cli.md §7.1.
func defaultDiscordDeps(auth *authDeps, getenv envSnapshot) (*discordDeps, error) {
	port := discordLoopbackPortDefault
	if v := getenvOr(getenv, "PTH_DISCORD_LOOPBACK_PORT", ""); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed <= 0 || parsed > 65535 {
			return nil, fmt.Errorf("PTH_DISCORD_LOOPBACK_PORT %q: must be an integer in 1..65535", v)
		}
		port = parsed
	}
	deps := &discordDeps{
		authDeps:        auth,
		DiscordClientID: getenvOr(getenv, "PTH_DISCORD_CLIENT_ID", ""),
		LoopbackPort:    port,
		BackendRESTBase: getenvOr(getenv, "PTH_BACKEND_REST_URL", ""),
		BindLoopback: func(addr string) (net.Listener, error) {
			return net.Listen("tcp", addr)
		},
		OpenBrowser:        openBrowser,
		ExchangeHTTPClient: &http.Client{Timeout: 30 * time.Second},
		PasteReader:        readPastedRedirectFromTTY,
		StateGenerator:     randomState,
	}
	return deps, nil
}

// runAuthLoginDiscord implements `pth auth login --discord` per cli.md
// §7.1. Returns an exit code; the caller (runAuthLogin) wires that into
// process exit. Three flag combinations matter: default (loopback +
// browser), --no-browser (loopback, print URL only), --manual (skip
// listener, prompt for paste). --dry-run short-circuits before any IO.
func runAuthLoginDiscord(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, deps *discordDeps) int {
	fs := flag.NewFlagSet("auth login --discord", flag.ContinueOnError)
	fs.SetOutput(stderr)
	manual := fs.Bool("manual", false, "skip the loopback listener; prompt for the pasted redirect URL")
	noBrowser := fs.Bool("no-browser", false, "do not auto-open the authorize URL")
	dryRun := fs.Bool("dry-run", false, "print the authorize/exchange URLs and exit without binding the listener or POSTing")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}

	if g.Namespace == "" {
		fmt.Fprintln(stderr, "auth login --discord: --namespace (or PTH_NAMESPACE) is required")
		return exitLocalError
	}
	if deps.DiscordClientID == "" {
		fmt.Fprintln(stderr, "auth login --discord: PTH_DISCORD_CLIENT_ID is required (the Discord OAuth Client ID)")
		return exitLocalError
	}
	if deps.BackendRESTBase == "" {
		fmt.Fprintln(stderr, "auth login --discord: PTH_BACKEND_REST_URL is required (HTTPS gateway base, e.g. https://<host>/<base-path>)")
		return exitLocalError
	}
	if deps.LoopbackPort <= 0 {
		deps.LoopbackPort = discordLoopbackPortDefault
	}

	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", deps.LoopbackPort)
	exchangeURL := strings.TrimRight(deps.BackendRESTBase, "/") + discordExchangePath

	state, err := deps.StateGenerator()
	if err != nil {
		fmt.Fprintf(stderr, "auth login --discord: generating state: %v\n", err)
		return exitLocalError
	}
	authorizeURL := buildDiscordAuthorizeURL(deps.DiscordClientID, redirectURI, state)

	if *dryRun {
		mode := "loopback"
		if *manual {
			mode = "manual"
		}
		listenerAddr := ""
		if !*manual {
			listenerAddr = fmt.Sprintf("127.0.0.1:%d", deps.LoopbackPort)
		}
		if err := writeJSONValue(stdout, map[string]any{
			"mode":         mode,
			"authorizeUrl": authorizeURL,
			"redirectUri":  redirectURI,
			"exchangeUrl":  exchangeURL,
			"listenerAddr": listenerAddr,
			"clientId":     deps.DiscordClientID,
			"state":        state,
		}); err != nil {
			fmt.Fprintf(stderr, "auth login --discord: %v\n", err)
			return exitLocalError
		}
		return exitOK
	}

	var code string
	if *manual {
		code, err = obtainDiscordCodeManual(stderr, deps, authorizeURL, state)
	} else {
		code, err = obtainDiscordCodeLoopback(ctx, stderr, deps, authorizeURL, state, *noBrowser)
	}
	if err != nil {
		fmt.Fprintf(stderr, "auth login --discord: %v\n", err)
		return exitLocalError
	}

	tok, err := postExchange(ctx, deps.ExchangeHTTPClient, exchangeURL, code, redirectURI)
	if err != nil {
		return reportExchangeFailure(stderr, err)
	}

	expiresAt := deps.now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	userID, _ := iam.DecodeSubject(tok.AccessToken)
	if err := deps.store.putProfile(g.Profile, &profileEntry{
		Addr:         g.Addr,
		Namespace:    g.Namespace,
		UserID:       userID,
		LoginMode:    loginModeDiscord,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    expiresAt,
	}); err != nil {
		fmt.Fprintf(stderr, "auth login --discord: storing credential: %v\n", err)
		return exitLocalError
	}
	if err := writeJSONValue(stdout, map[string]any{
		"profile":   g.Profile,
		"userId":    userID,
		"namespace": g.Namespace,
		"loginMode": loginModeDiscord,
		"expiresAt": expiresAt.UTC().Format(time.RFC3339),
	}); err != nil {
		fmt.Fprintf(stderr, "auth login --discord: %v\n", err)
		return exitLocalError
	}
	return exitOK
}

// buildDiscordAuthorizeURL composes the Discord OAuth2 authorize URL.
// Mirrors player/src/lib/auth.ts buildDiscordAuthorizeUrl byte-for-byte
// so the verified player flow's URL shape stays the canonical reference.
func buildDiscordAuthorizeURL(clientID, redirectURI, state string) string {
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", clientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("state", state)
	params.Set("scope", discordLoginScope)
	return discordAuthorizeURL + "?" + params.Encode()
}

// obtainDiscordCodeLoopback runs the default flow: bind the listener,
// optionally open a browser, wait for Discord's redirect with the code.
// Validates `state` to defeat CSRF / replay between concurrent CLI runs.
// `redirect_uri` doesn't appear in the signature because the handler
// reads `code` + `state` straight from the parsed query — Discord
// redirects to the listener's address regardless of what we pass.
func obtainDiscordCodeLoopback(ctx context.Context, stderr io.Writer, deps *discordDeps, authorizeURL, state string, noBrowser bool) (string, error) {
	listenAddr := fmt.Sprintf("127.0.0.1:%d", deps.LoopbackPort)
	ln, err := deps.BindLoopback(listenAddr)
	if err != nil {
		return "", fmt.Errorf("binding %s: %w (port already in use? set PTH_DISCORD_LOOPBACK_PORT)", listenAddr, err)
	}
	defer ln.Close() //nolint:errcheck // best-effort close; server.Shutdown owns the actual lifecycle.

	type result struct {
		code string
		err  error
	}
	resultCh := make(chan result, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		gotState := r.URL.Query().Get("state")
		gotCode := r.URL.Query().Get("code")
		gotErr := r.URL.Query().Get("error")
		if gotErr != "" {
			desc := r.URL.Query().Get("error_description")
			renderCallbackPage(w, false, fmt.Sprintf("Discord returned %s: %s", gotErr, desc))
			resultCh <- result{err: fmt.Errorf("Discord OAuth error: %s: %s", gotErr, desc)}
			return
		}
		if gotState != state {
			renderCallbackPage(w, false, "state mismatch — login aborted (possible CSRF / stale tab)")
			resultCh <- result{err: fmt.Errorf("state mismatch: got %q, want %q", gotState, state)}
			return
		}
		if gotCode == "" {
			renderCallbackPage(w, false, "no authorization code in callback URL")
			resultCh <- result{err: fmt.Errorf("callback missing ?code=")}
			return
		}
		renderCallbackPage(w, true, "")
		resultCh <- result{code: gotCode}
	})

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Serve(ln) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		<-serverDone
	}()

	fmt.Fprintf(stderr, "auth login --discord: listening on %s\n", listenAddr)
	fmt.Fprintf(stderr, "auth login --discord: open this URL to log in:\n  %s\n", authorizeURL)
	if !noBrowser {
		if err := deps.OpenBrowser(authorizeURL); err != nil {
			fmt.Fprintf(stderr, "auth login --discord: could not auto-open browser (%v); paste the URL manually\n", err)
		}
	}

	select {
	case res := <-resultCh:
		if res.err != nil {
			return "", res.err
		}
		return res.code, nil
	case <-ctx.Done():
		return "", fmt.Errorf("waiting for Discord callback: %w", ctx.Err())
	}
}

// obtainDiscordCodeManual prints the authorize URL and reads a pasted
// redirect URL (or bare ?code=... fragment) from the operator's TTY.
// Used when 127.0.0.1:<port> isn't reachable from the user's browser.
func obtainDiscordCodeManual(stderr io.Writer, deps *discordDeps, authorizeURL, state string) (string, error) {
	fmt.Fprintln(stderr, "auth login --discord (manual): open this URL in any browser:")
	fmt.Fprintf(stderr, "  %s\n", authorizeURL)
	pasted, err := deps.PasteReader("Paste the full redirect URL (or just ?code=...&state=...): ")
	if err != nil {
		return "", fmt.Errorf("reading pasted URL: %w", err)
	}
	code, gotState, err := parsePastedRedirect(pasted)
	if err != nil {
		return "", err
	}
	if gotState != "" && gotState != state {
		return "", fmt.Errorf("state mismatch: got %q, want %q (paste from the most recent login attempt)", gotState, state)
	}
	if code == "" {
		return "", fmt.Errorf("pasted URL missing ?code=")
	}
	return code, nil
}

// parsePastedRedirect accepts either a full URL (scheme + host + query)
// or just the query fragment (`?code=...&state=...` / `code=...&state=...`).
// Returns (code, state, error). State may be empty if the operator
// trimmed it; the caller's mismatch check tolerates that.
func parsePastedRedirect(input string) (code, state string, err error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", "", fmt.Errorf("empty input")
	}
	rawQuery := trimmed
	if u, perr := url.Parse(trimmed); perr == nil && u.RawQuery != "" {
		rawQuery = u.RawQuery
	} else {
		rawQuery = strings.TrimPrefix(rawQuery, "?")
	}
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "", "", fmt.Errorf("parsing pasted query: %w", err)
	}
	if e := values.Get("error"); e != "" {
		return "", "", fmt.Errorf("Discord returned %s: %s", e, values.Get("error_description"))
	}
	return values.Get("code"), values.Get("state"), nil
}

// postExchange POSTs `{code, redirect_uri}` to the backend's
// /v1/player/discord/exchange (grpc-gateway REST mapping). Returns the
// parsed response or an error categorised so reportExchangeFailure can
// pick the right exit code + remediation message.
func postExchange(ctx context.Context, client *http.Client, exchangeURL, code, redirectURI string) (*discordExchangeResponse, error) {
	body, err := json.Marshal(discordExchangeRequest{Code: code, RedirectURI: redirectURI})
	if err != nil {
		return nil, fmt.Errorf("marshalling exchange request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, exchangeURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("building exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, &exchangeTransportError{wrapped: err}
	}
	defer drainAndClose(resp.Body)
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &exchangeTransportError{wrapped: fmt.Errorf("reading exchange response: %w", err)}
	}
	if resp.StatusCode >= 400 {
		return nil, &exchangeServerError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}
	var parsed discordExchangeResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("decoding exchange response: %w", err)
	}
	if parsed.AccessToken == "" {
		return nil, fmt.Errorf("exchange response missing access_token")
	}
	return &parsed, nil
}

// exchangeTransportError represents network-layer failures: DNS, TCP
// reset, TLS handshake, etc. Mapped to exitTransportError.
type exchangeTransportError struct{ wrapped error }

func (e *exchangeTransportError) Error() string { return e.wrapped.Error() }
func (e *exchangeTransportError) Unwrap() error { return e.wrapped }

// exchangeServerError represents a 4xx/5xx from the backend. 4xx is the
// user-actionable case (bad code, redirect_uri mismatch); 5xx is treated
// as transport since AGS-side latency / timeouts surface here per
// docs/runbooks/discord-login.md § Failure modes.
type exchangeServerError struct {
	StatusCode int
	Body       string
}

func (e *exchangeServerError) Error() string {
	return fmt.Sprintf("backend exchange %d: %s", e.StatusCode, truncate(e.Body, 300))
}

func reportExchangeFailure(stderr io.Writer, err error) int {
	var transport *exchangeTransportError
	if errors.As(err, &transport) {
		fmt.Fprintf(stderr, "auth login --discord: backend unreachable: %v\n", transport.wrapped)
		return exitTransportError
	}
	var server *exchangeServerError
	if errors.As(err, &server) {
		switch {
		case server.StatusCode >= 500:
			fmt.Fprintf(stderr, "auth login --discord: backend error %d (likely AGS-side latency, retry once): %s\n", server.StatusCode, truncate(server.Body, 300))
			return exitTransportError
		default:
			fmt.Fprintf(stderr, "auth login --discord: backend rejected exchange (%d): %s\n", server.StatusCode, truncate(server.Body, 300))
			return exitClientError
		}
	}
	fmt.Fprintf(stderr, "auth login --discord: %v\n", err)
	return exitLocalError
}

// renderCallbackPage writes the in-browser confirmation page. Success
// message tells the user to close the tab; failure message includes the
// reason so they don't have to dig through the CLI's stderr.
func renderCallbackPage(w http.ResponseWriter, ok bool, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if ok {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `<!doctype html><meta charset=utf-8><title>pth auth login</title><body style="font-family:system-ui;max-width:32rem;margin:4rem auto;line-height:1.5"><h1>Logged in.</h1><p>You can close this tab.</p></body>`)
		return
	}
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprintf(w, `<!doctype html><meta charset=utf-8><title>pth auth login</title><body style="font-family:system-ui;max-width:32rem;margin:4rem auto;line-height:1.5"><h1>Login failed.</h1><p>%s</p><p>Return to the terminal for details.</p></body>`, escapeHTML(errMsg))
}

func escapeHTML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")
	return r.Replace(s)
}

// randomState returns a 32-byte URL-safe base64 string. Length matches
// the player's `crypto.getRandomValues` output so an outsider can't
// distinguish the two flows by token shape.
func randomState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("reading random state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// openBrowser shells out to the platform's URL handler. Best-effort —
// failure is non-fatal; the operator can always paste the URL manually.
func openBrowser(targetURL string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name = "open"
		args = []string{targetURL}
	case "windows":
		name = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", targetURL}
	default:
		name = "xdg-open"
		args = []string{targetURL}
	}
	cmd := exec.Command(name, args...) //nolint:gosec // CLI invokes a fixed binary with a CLI-built URL; no user-supplied shell metachars.
	return cmd.Start()
}

// readPastedRedirectFromTTY is the production paste seam for --manual.
// Reads one line from /dev/tty (echoed — the redirect URL is not a
// secret) so a password manager / clipboard paste works without
// disturbing stdout. SSH-tunnelled sessions without a TTY hit the error
// path; those operators should set up port-forwarding for the loopback
// flow instead of pasting.
func readPastedRedirectFromTTY(prompt string) (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("opening /dev/tty: %w (forward the loopback port over SSH instead of using --manual)", err)
	}
	defer tty.Close()
	if _, err := fmt.Fprint(tty, prompt); err != nil {
		return "", fmt.Errorf("writing prompt: %w", err)
	}
	line, err := readSingleLine(tty)
	if err != nil {
		return "", err
	}
	return line, nil
}
