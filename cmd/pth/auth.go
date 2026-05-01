package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

// refreshLeeway is the cli.md §7.3 60-second window: a token within this
// margin of expiry is refreshed before the dial completes.
const refreshLeeway = 60 * time.Second

// Login-mode constants persisted as `loginMode` on profileEntry. Kept
// in one place so dispatch + storage agree on the wire string.
const (
	loginModePassword = "password"
	loginModeDiscord  = "discord"
)

// authDeps centralises the things `pth auth ...` (and the dial-time
// refresh path) need but cannot easily look up themselves: a credentials
// store, an IAM HTTP client, a clock, and a password reader. Tests
// substitute each one independently.
type authDeps struct {
	store         *credStore
	iam           *iamClient
	now           func() time.Time
	readPassword  func(prompt string) (string, error) // interactive TTY prompt
	stdinPassword func() (string, error)              // --password-stdin source
	stdin         io.Reader                           // raw stdin for stdinPassword default

	// discordDepsFactory wires the Discord-flow seams. Defaults to
	// defaultDiscordDeps; tests substitute a stub. Held as a factory (not
	// a built struct) so the env snapshot used at runAuth() entry feeds
	// every call without a global.
	discordDepsFactory func(getenv envSnapshot) (*discordDeps, error)
}

// defaultAuthDeps wires the production seams: filesystem store at
// ~/.config/playtesthub, real IAM HTTP client, real wall clock, real TTY
// reader.
func defaultAuthDeps(getenv envSnapshot) (*authDeps, error) {
	path, err := defaultCredStorePath()
	if err != nil {
		return nil, fmt.Errorf("resolving credentials path: %w", err)
	}
	base := getenvOr(getenv, "PTH_AGS_BASE_URL", "")
	cid := getenvOr(getenv, "PTH_IAM_CLIENT_ID", "")
	csec := getenvOr(getenv, "PTH_IAM_CLIENT_SECRET", "")
	deps := &authDeps{
		store: newCredStore(path),
		iam: &iamClient{
			BaseURL:      base,
			ClientID:     cid,
			ClientSecret: csec,
			HTTPClient:   &http.Client{Timeout: 30 * time.Second},
		},
		now:           time.Now,
		readPassword:  readPasswordFromTTY,
		stdinPassword: func() (string, error) { return readSingleLine(os.Stdin) },
		stdin:         os.Stdin,
	}
	deps.discordDepsFactory = func(getenv envSnapshot) (*discordDeps, error) {
		return defaultDiscordDeps(deps, getenv)
	}
	return deps, nil
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}

func getenvOr(getenv envSnapshot, key, fallback string) string {
	if getenv == nil {
		return fallback
	}
	if v, ok := getenv(key); ok && v != "" {
		return v
	}
	return fallback
}

func runAuth(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, getenv envSnapshot) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "auth: action required (one of: login, logout, whoami, token)")
		return exitLocalError
	}
	deps, err := defaultAuthDeps(getenv)
	if err != nil {
		fmt.Fprintf(stderr, "auth: %v\n", err)
		return exitLocalError
	}
	action, rest := args[0], args[1:]
	switch action {
	case "login":
		return runAuthLogin(ctx, stdout, stderr, g, rest, deps, getenv)
	case "logout":
		return runAuthLogout(stdout, stderr, g, rest, deps)
	case "whoami":
		return runAuthWhoami(ctx, stdout, stderr, g, rest, deps)
	case "token":
		return runAuthToken(ctx, stdout, stderr, g, rest, deps)
	default:
		fmt.Fprintf(stderr, "auth: unknown action %q\n", action)
		return exitLocalError
	}
}

// runAuthLogin classifies the login mode from args (--password vs --discord)
// and dispatches into the matching sub-flow. The mode flags are stripped
// from the forwarded args so each sub-flow's own flagset doesn't trip on
// them. Mutually exclusive: passing both is a flag error.
func runAuthLogin(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, deps *authDeps, getenv envSnapshot) int {
	mode, rest := classifyLoginMode(args)
	switch mode {
	case loginModeDiscord:
		if deps.discordDepsFactory == nil {
			fmt.Fprintln(stderr, "auth login --discord: discord deps factory not wired")
			return exitLocalError
		}
		ddeps, err := deps.discordDepsFactory(getenv)
		if err != nil {
			fmt.Fprintf(stderr, "auth login --discord: %v\n", err)
			return exitLocalError
		}
		return runAuthLoginDiscord(ctx, stdout, stderr, g, rest, ddeps)
	case loginModePassword:
		return runAuthLoginPassword(ctx, stdout, stderr, g, rest, deps)
	case "both":
		fmt.Fprintln(stderr, "auth login: --discord and --password are mutually exclusive")
		return exitLocalError
	default:
		fmt.Fprintln(stderr, "auth login: one of --password or --discord is required")
		return exitLocalError
	}
}

// classifyLoginMode walks args, peels the mode flags (--password,
// --discord) and returns ("password" | "discord" | "both" | ""), plus
// the remaining args for the chosen sub-flow's flagset to parse.
func classifyLoginMode(args []string) (mode string, rest []string) {
	rest = make([]string, 0, len(args))
	var seenPassword, seenDiscord bool
	for _, a := range args {
		switch a {
		case "--" + loginModePassword:
			seenPassword = true
		case "--" + loginModeDiscord:
			seenDiscord = true
		default:
			rest = append(rest, a)
		}
	}
	switch {
	case seenPassword && seenDiscord:
		return "both", rest
	case seenPassword:
		return loginModePassword, rest
	case seenDiscord:
		return loginModeDiscord, rest
	default:
		return "", rest
	}
}

func runAuthLoginPassword(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, deps *authDeps) int {
	fs := flag.NewFlagSet("auth login --password", flag.ContinueOnError)
	fs.SetOutput(stderr)
	username := fs.String("username", "", "AGS username (required)")
	stdinPw := fs.Bool("password-stdin", false, "read password from one line on stdin instead of TTY prompt")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *username == "" {
		fmt.Fprintln(stderr, "auth login --password: --username is required")
		return exitLocalError
	}
	if g.Namespace == "" {
		fmt.Fprintln(stderr, "auth login --password: --namespace (or PTH_NAMESPACE) is required")
		return exitLocalError
	}
	pw, err := readLoginPassword(deps, *stdinPw, *username)
	if err != nil {
		fmt.Fprintf(stderr, "auth login: %v\n", err)
		return exitLocalError
	}
	if pw == "" {
		fmt.Fprintln(stderr, "auth login: empty password")
		return exitLocalError
	}
	tok, err := deps.iam.passwordLogin(ctx, g.Namespace, *username, pw, deps.now)
	if err != nil {
		return reportAuthFailure(stderr, "auth login", err)
	}
	if err := deps.store.putProfile(g.Profile, &profileEntry{
		Addr:         g.Addr,
		Namespace:    g.Namespace,
		UserID:       tok.UserID,
		LoginMode:    loginModePassword,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    tok.ExpiresAt,
	}); err != nil {
		fmt.Fprintf(stderr, "auth login: storing credential: %v\n", err)
		return exitLocalError
	}
	if err := writeJSONValue(stdout, map[string]any{
		"profile":   g.Profile,
		"userId":    tok.UserID,
		"namespace": g.Namespace,
		"loginMode": loginModePassword,
		"expiresAt": tok.ExpiresAt.UTC().Format(time.RFC3339),
	}); err != nil {
		fmt.Fprintf(stderr, "auth login: %v\n", err)
		return exitLocalError
	}
	return exitOK
}

func runAuthLogout(stdout, stderr io.Writer, g *Globals, args []string, deps *authDeps) int {
	fs := flag.NewFlagSet("auth logout", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	removed, err := deps.store.deleteProfile(g.Profile)
	if err != nil {
		fmt.Fprintf(stderr, "auth logout: %v\n", err)
		return exitLocalError
	}
	if err := writeJSONValue(stdout, map[string]any{
		"profile": g.Profile,
		"removed": removed,
	}); err != nil {
		fmt.Fprintf(stderr, "auth logout: %v\n", err)
		return exitLocalError
	}
	return exitOK
}

// runAuthWhoami reports the active token's identity. If the token is
// expired and refresh fails, exits non-zero so scripts can detect the
// state without parsing JSON. Refresh is opportunistic — a near-expiry
// token gets rotated, but a still-valid token is reported as-is.
func runAuthWhoami(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, deps *authDeps) int {
	fs := flag.NewFlagSet("auth whoami", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	p, err := resolveActiveProfile(ctx, g, deps)
	if err != nil {
		fmt.Fprintf(stderr, "auth whoami: %v\n", err)
		return exitLocalError
	}
	if p == nil {
		fmt.Fprintf(stderr, "auth whoami: no credential for profile %q (run: pth auth login --password)\n", g.Profile)
		return exitLocalError
	}
	if err := writeJSONValue(stdout, map[string]any{
		"profile":   g.Profile,
		"userId":    p.UserID,
		"namespace": p.Namespace,
		"addr":      p.Addr,
		"loginMode": p.LoginMode,
		"expiresAt": p.ExpiresAt.UTC().Format(time.RFC3339),
	}); err != nil {
		fmt.Fprintf(stderr, "auth whoami: %v\n", err)
		return exitLocalError
	}
	return exitOK
}

// runAuthToken prints just the bearer string (no newline-stripped JSON
// surrounding it) so it pipes cleanly into curl/grpcurl/etc.
func runAuthToken(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, deps *authDeps) int {
	fs := flag.NewFlagSet("auth token", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	p, err := resolveActiveProfile(ctx, g, deps)
	if err != nil {
		fmt.Fprintf(stderr, "auth token: %v\n", err)
		return exitLocalError
	}
	if p == nil || p.AccessToken == "" {
		fmt.Fprintf(stderr, "auth token: no credential for profile %q\n", g.Profile)
		return exitLocalError
	}
	fmt.Fprintln(stdout, p.AccessToken)
	return exitOK
}

// resolveActiveProfile returns the stored profile after rotating the
// token if it's within `refreshLeeway` of expiry. Returns (nil, nil)
// when no profile is stored — the caller decides whether that's an
// error (whoami: yes) or a fall-through (dial: send anon).
func resolveActiveProfile(ctx context.Context, g *Globals, deps *authDeps) (*profileEntry, error) {
	p, err := deps.store.getProfile(g.Profile, g.Addr, g.Namespace)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, nil
	}
	if !needsRefresh(p, deps.now()) {
		return p, nil
	}
	if p.RefreshToken == "" {
		return nil, fmt.Errorf("token for profile %q has expired and no refresh token is stored — run: pth auth login --password --username <u>", g.Profile)
	}
	tok, err := deps.iam.refresh(ctx, p.RefreshToken, deps.now)
	if err != nil {
		var ie *iamError
		if errors.As(err, &ie) && ie.IsInvalidGrant() {
			return nil, fmt.Errorf("refresh token for profile %q rejected by AGS IAM — run: pth auth login --password --username <u>", g.Profile)
		}
		return nil, fmt.Errorf("refreshing token for profile %q: %w", g.Profile, err)
	}
	updated := &profileEntry{
		Addr:         p.Addr,
		Namespace:    p.Namespace,
		UserID:       firstNonEmpty(tok.UserID, p.UserID),
		LoginMode:    p.LoginMode,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    tok.ExpiresAt,
	}
	if err := deps.store.putProfile(g.Profile, updated); err != nil {
		return nil, fmt.Errorf("persisting refreshed token for profile %q: %w", g.Profile, err)
	}
	return updated, nil
}

// needsRefresh returns true when `now` is within refreshLeeway of the
// token's expiry, OR already past it. A zero-value ExpiresAt is treated
// as "no expiry known" — never refresh.
func needsRefresh(p *profileEntry, now time.Time) bool {
	if p == nil || p.ExpiresAt.IsZero() {
		return false
	}
	return !now.Add(refreshLeeway).Before(p.ExpiresAt)
}

// readLoginPassword reads the password from the requested source and
// guarantees it never appears in argv. `--password-stdin` reads exactly
// one line from stdin (trailing newline trimmed); the default opens
// /dev/tty and disables echo.
func readLoginPassword(deps *authDeps, fromStdin bool, username string) (string, error) {
	if fromStdin {
		return deps.stdinPassword()
	}
	return deps.readPassword(fmt.Sprintf("Password for %s: ", username))
}

func reportAuthFailure(stderr io.Writer, prefix string, err error) int {
	var ie *iamError
	if errors.As(err, &ie) {
		switch {
		case ie.IsInvalidGrant():
			fmt.Fprintf(stderr, "%s: %s\n", prefix, ie.Description)
			return exitClientError
		case ie.StatusCode >= 500:
			fmt.Fprintf(stderr, "%s: %v\n", prefix, ie)
			return exitTransportError
		default:
			fmt.Fprintf(stderr, "%s: %v\n", prefix, ie)
			return exitClientError
		}
	}
	fmt.Fprintf(stderr, "%s: %v\n", prefix, err)
	return exitTransportError
}

// readSingleLine returns the first newline-terminated line from r,
// trimmed of CR/LF. Used by --password-stdin so a piped password file
// (`--password-stdin <file`) is always read as a single secret regardless
// of trailing whitespace.
func readSingleLine(r io.Reader) (string, error) {
	br := bufio.NewReader(r)
	line, err := br.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// readPasswordFromTTY opens /dev/tty (or the platform equivalent) and
// reads a password without echoing it. Falls back to a clear error when
// no TTY is available, telling the caller to use --password-stdin.
func readPasswordFromTTY(prompt string) (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("opening /dev/tty: %w (use --password-stdin instead)", err)
	}
	defer tty.Close()
	if !term.IsTerminal(int(tty.Fd())) {
		return "", fmt.Errorf("/dev/tty is not a terminal (use --password-stdin instead)")
	}
	if _, err := fmt.Fprint(tty, prompt); err != nil {
		return "", fmt.Errorf("writing prompt: %w", err)
	}
	pw, err := term.ReadPassword(int(tty.Fd()))
	// ReadPassword leaves the cursor on the same line; emit a newline so
	// the next stdout/stderr write doesn't collide with the prompt.
	_, _ = fmt.Fprintln(tty)
	if err != nil {
		return "", fmt.Errorf("reading password: %w", err)
	}
	return string(pw), nil
}
