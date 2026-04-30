package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// Globals captures every flag in cli.md §4. The fields stay primitive so
// the dispatch path doesn't drift from the documented surface.
type Globals struct {
	Addr      string
	BasePath  string
	Namespace string
	Profile   string
	Token     string
	Anon      bool
	Timeout   time.Duration
	Insecure  bool

	// InsecureSet records whether the user explicitly passed --insecure on
	// the command line. When false, the dialer auto-flips insecure on for
	// loopback addresses (cli.md §4: "Default true when --addr is loopback").
	InsecureSet bool

	Verbose bool
}

// envSnapshot is the env-var lookup interface so tests can drive
// parseGlobals without polluting the real environment.
type envSnapshot func(key string) (string, bool)

// parseGlobals reads cli.md §4 global flags off args, returns the
// remaining tokens (subcommand + its flags), and resolves env-var
// fallbacks per the documented precedence (flag > env > default).
//
// Returns an error if a flag is malformed; the caller maps that to
// exitLocalError. Per cli.md §3, the parser is hand-rolled because we
// need to stop at the first non-flag positional (the subcommand) and
// hand the rest off untouched.
func parseGlobals(args []string, getenv envSnapshot) (*Globals, []string, error) {
	if getenv == nil {
		return nil, nil, fmt.Errorf("internal: nil env snapshot")
	}
	g := &Globals{
		Addr:    "localhost:6565",
		Profile: "default",
		Timeout: 10 * time.Second,
	}
	if err := applyEnvDefaults(g, getenv); err != nil {
		return nil, nil, err
	}
	rest, err := walkGlobalFlags(g, args)
	if err != nil {
		return nil, nil, err
	}
	return g, rest, nil
}

func applyEnvDefaults(g *Globals, getenv envSnapshot) error {
	stringOverrides := []struct {
		key    string
		target *string
	}{
		{"PTH_ADDR", &g.Addr},
		{"PTH_BASE_PATH", &g.BasePath},
		{"PTH_NAMESPACE", &g.Namespace},
		{"PTH_PROFILE", &g.Profile},
		{"PTH_TOKEN", &g.Token},
	}
	for _, s := range stringOverrides {
		if v, ok := getenv(s.key); ok && v != "" {
			*s.target = v
		}
	}
	if v, ok := getenv("PTH_TIMEOUT"); ok && v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("PTH_TIMEOUT %q: %w", v, err)
		}
		g.Timeout = d
	}
	if v, ok := getenv("PTH_INSECURE"); ok {
		b, err := parseBool(v)
		if err != nil {
			return fmt.Errorf("PTH_INSECURE %q: %w", v, err)
		}
		g.Insecure = b
		g.InsecureSet = true
	}
	return nil
}

func walkGlobalFlags(g *Globals, args []string) ([]string, error) {
	rest := []string{}
	for i := 0; i < len(args); {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			rest = append(rest, args[i:]...)
			return rest, nil
		}
		if a == "--" {
			rest = append(rest, args[i+1:]...)
			return rest, nil
		}
		key, val, hasVal := splitFlag(a)
		consumed, handled, err := assignGlobalFlag(g, key, val, hasVal, args, i)
		if err != nil {
			return nil, err
		}
		if !handled {
			// Unrecognised flag belongs to the subcommand.
			rest = append(rest, args[i:]...)
			return rest, nil
		}
		i += consumed
	}
	return rest, nil
}

// assignGlobalFlag mutates g for a single flag token. handled=false means
// the flag is not a known global and the caller should defer to the
// subcommand parser. consumed is 1 (flag-only) or 2 (flag + separate value).
func assignGlobalFlag(g *Globals, key, val string, hasVal bool, args []string, i int) (consumed int, handled bool, err error) {
	consumed = 1
	take := func() (string, error) {
		if hasVal {
			return val, nil
		}
		if i+1 >= len(args) {
			return "", fmt.Errorf("flag %s requires a value", key)
		}
		consumed = 2
		return args[i+1], nil
	}

	switch key {
	case "--addr":
		v, err := take()
		if err != nil {
			return 0, true, err
		}
		g.Addr = v
	case "--base-path":
		v, err := take()
		if err != nil {
			return 0, true, err
		}
		g.BasePath = v
	case "--namespace":
		v, err := take()
		if err != nil {
			return 0, true, err
		}
		g.Namespace = v
	case "--profile":
		v, err := take()
		if err != nil {
			return 0, true, err
		}
		g.Profile = v
	case "--token":
		v, err := take()
		if err != nil {
			return 0, true, err
		}
		g.Token = v
	case "--timeout":
		v, err := take()
		if err != nil {
			return 0, true, err
		}
		d, err := time.ParseDuration(v)
		if err != nil {
			return 0, true, fmt.Errorf("--timeout %q: %w", v, err)
		}
		g.Timeout = d
	case "--anon":
		g.Anon = true
	case "--insecure":
		if err := applyInsecureFlag(g, val, hasVal); err != nil {
			return 0, true, err
		}
	case "-v", "--verbose":
		g.Verbose = true
	default:
		return 1, false, nil
	}
	return consumed, true, nil
}

func applyInsecureFlag(g *Globals, val string, hasVal bool) error {
	if hasVal {
		b, err := parseBool(val)
		if err != nil {
			return fmt.Errorf("--insecure %q: %w", val, err)
		}
		g.Insecure = b
	} else {
		g.Insecure = true
	}
	g.InsecureSet = true
	return nil
}

// splitFlag handles `--flag=value` and bare `--flag` forms uniformly.
func splitFlag(a string) (key, val string, hasVal bool) {
	if idx := strings.IndexByte(a, '='); idx > 0 {
		return a[:idx], a[idx+1:], true
	}
	return a, "", false
}

func parseBool(v string) (bool, error) {
	switch strings.ToLower(v) {
	case "1", "true", "t", "yes", "y", "on":
		return true, nil
	case "0", "false", "f", "no", "n", "off":
		return false, nil
	}
	return false, fmt.Errorf("not a boolean")
}

// effectiveInsecure resolves the cli.md §4 default rule: loopback ⇒ insecure
// unless the user explicitly opted in to TLS.
func (g *Globals) effectiveInsecure() bool {
	if g.InsecureSet {
		return g.Insecure
	}
	host, _, err := net.SplitHostPort(g.Addr)
	if err != nil {
		host = g.Addr
	}
	return isLoopback(host)
}

func isLoopback(host string) bool {
	if host == "localhost" || host == "" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// dial opens a gRPC client conn against g.Addr with the resolved security
// + auth metadata. Auth header attachment per cli.md §4 token-resolution
// order — `--anon` short-circuits everything; otherwise `--token` /
// PTH_TOKEN value is sent verbatim. Credential-store lookup arrives in
// phase 10.2 — until then a missing token on an authed RPC will surface
// as Unauthenticated from the server, which is the right error.
func (g *Globals) dial(ctx context.Context) (*grpc.ClientConn, context.Context, error) {
	var creds credentials.TransportCredentials
	if g.effectiveInsecure() {
		creds = insecure.NewCredentials()
	} else {
		creds = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
	}
	conn, err := grpc.NewClient(g.Addr, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, nil, fmt.Errorf("dial %s: %w", g.Addr, err)
	}
	if !g.Anon && g.Token != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+g.Token)
	}
	return conn, ctx, nil
}

// logRPC writes a redacted request line to stderr when -v is set. Token
// values never appear in this stream.
func (g *Globals) logRPC(w io.Writer, method string) {
	if !g.Verbose {
		return
	}
	auth := "anon"
	switch {
	case g.Anon:
		auth = "anon"
	case g.Token != "":
		auth = "bearer (redacted)"
	}
	fmt.Fprintf(w, "[pth] %s addr=%s auth=%s timeout=%s\n", method, g.Addr, auth, g.Timeout)
}
