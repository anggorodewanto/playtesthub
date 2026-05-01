// Command pth — playtesthub CLI client. See docs/cli.md.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	code := run(ctx, os.Stdout, os.Stderr, os.Args[1:], os.LookupEnv)
	os.Exit(code)
}

// run is the testable entry point. Exit codes follow cli.md §8.
func run(ctx context.Context, stdout, stderr io.Writer, args []string, getenv envSnapshot) int {
	g, rest, err := parseGlobals(args, getenv)
	if err != nil {
		fmt.Fprintf(stderr, "pth: %v\n", err)
		return exitLocalError
	}

	if len(rest) == 0 {
		writeUsage(stderr)
		return exitLocalError
	}

	cmd, cmdArgs := rest[0], rest[1:]
	g.tokenResolver = defaultTokenResolver(g, getenv)
	factory := defaultPlaytestClientFactory(g)

	switch cmd {
	case "version":
		return runVersion(stdout, stderr, cmdArgs)
	case "doctor":
		return runDoctor(ctx, stdout, stderr, g, cmdArgs, factory)
	case "playtest":
		return runPlaytest(ctx, stdout, stderr, g, cmdArgs, factory)
	case "auth":
		return runAuth(ctx, stdout, stderr, g, cmdArgs, getenv)
	case "user":
		return runUser(ctx, stdout, stderr, g, cmdArgs, getenv)
	case "applicant":
		return runApplicant(ctx, stdout, stderr, g, cmdArgs, factory)
	case "help", "-h", "--help":
		writeUsage(stdout)
		return exitOK
	default:
		fmt.Fprintf(stderr, "pth: unknown command %q\n", cmd)
		writeUsage(stderr)
		return exitLocalError
	}
}

// defaultPlaytestClientFactory wires (*Globals).dial into the
// playtestClientFactory shape the subcommands expect. Tests bypass this
// and pass their own stub.
func defaultPlaytestClientFactory(g *Globals) playtestClientFactory {
	return func(ctx context.Context) (pb.PlaytesthubServiceClient, context.Context, func() error, error) {
		conn, callCtx, err := g.dial(ctx)
		if err != nil {
			return nil, nil, nil, err
		}
		return pb.NewPlaytesthubServiceClient(conn), callCtx, func() error { return conn.Close() }, nil
	}
}

func writeUsage(w io.Writer) {
	fmt.Fprint(w, `pth — playtesthub CLI

Usage:
  pth [global flags] <command> [command flags]

Commands (M1 phase 10.1–10.5):
  version                          Print build SHA, proto schema, Go version.
  doctor                           Probe the backend. Reports gRPC code + latency.
  playtest get-public --slug <s>   Fetch the public view of a playtest (unauth).
  playtest get-player --slug <s>   Fetch the player view (player token required).
  playtest get --id <id>           Fetch the admin view (admin token required).
  playtest list                    List all playtests in --namespace (admin).
  playtest create --slug <s>       Create a playtest. Required: --slug, --title.
    --title <t> [--description <d>] [--banner-image-url <u>]
    [--platforms STEAM,XBOX,...]
    [--starts-at <RFC3339>] [--ends-at <RFC3339>]
    [--nda-required] [--nda-text @file.md]
    [--distribution-model STEAM_KEYS|AGS_CAMPAIGN]
    [--initial-code-quantity N]
  playtest edit --id <id>          Edit PRD-whitelisted mutable fields only.
    [--title|--description|--banner-image-url|--platforms|
     --starts-at|--ends-at|--nda-required|--nda-text]
  playtest delete --id <id>        Soft-delete (idempotent).
  playtest transition --id <id>    Drive the status machine (DRAFT → OPEN → CLOSED).
    --to DRAFT|OPEN|CLOSED
  applicant signup --slug <s>      Sign up the calling player.
    --platforms STEAM,XBOX,...
  applicant status --slug <s>      Show the calling player's applicant row.
  auth login --password            Log in via AGS IAM ROPC grant. Stores token under --profile.
    --username <u> [--password-stdin]
  auth login --discord             Log in via Discord OAuth (cli.md §7.1).
    [--manual] [--no-browser] [--dry-run]
  auth logout                      Remove the stored credential for --profile.
  auth whoami                      Print {profile, userId, namespace, addr, expiresAt}.
  auth token                       Print the active bearer token (for piping into curl/grpcurl).
  user create [--count N]          Create N AGS test users. Emits {userId, username, password, ...}.
    [--country US]                 Admin token required.
  user delete --id <userId> --yes  Delete an AGS user (destructive — --yes required).
  user login-as --id <userId>      Password-login as a previously-created test user.
    [--password-stdin]              Stores the credential under --profile.
  help                             Show this message.

Auth env (cli.md §7.2 / §7.1 setup; not in §4 since they only feed the auth subcommand):
  PTH_AGS_BASE_URL          AGS IAM base URL (e.g. https://internal-shared-cloud.accelbyte.io)
  PTH_IAM_CLIENT_ID         AGS IAM client id (must allow ROPC grant)
  PTH_IAM_CLIENT_SECRET     AGS IAM client secret (omit for public clients)
  PTH_CREDENTIALS_FILE      Override store path (default ~/.config/playtesthub/credentials.json)
  PTH_DISCORD_CLIENT_ID     Discord OAuth Client ID (required for --discord)
  PTH_DISCORD_LOOPBACK_PORT Loopback port for Discord callback (default 14565)
  PTH_BACKEND_REST_URL      Backend grpc-gateway base URL for the Discord exchange POST

Global flags (env var fallback in parens; see docs/cli.md §4):
  --addr <host:port>     gRPC endpoint              (PTH_ADDR; default localhost:6565)
  --base-path <path>     Backend BASE_PATH          (PTH_BASE_PATH; informational)
  --namespace <ns>       AGS namespace              (PTH_NAMESPACE)
  --profile <name>       Credential profile         (PTH_PROFILE; default "default")
  --token <bearer>       Override token verbatim    (PTH_TOKEN)
  --anon                 Send no auth metadata
  --timeout <duration>   Per-call deadline          (PTH_TIMEOUT; default 10s)
  --insecure             TLS off (auto-on for loopback)
  -v, --verbose          Log outgoing RPC to stderr (token redacted)

Subcommand flag:
  --dry-run              Print the request JSON and exit (no dial)

Exit codes (cli.md §8):
  0  gRPC OK
  1  gRPC InvalidArgument / NotFound / FailedPrecondition / etc.
  2  gRPC Unavailable / DeadlineExceeded / transport
  3  Local flag-parse / env / file-IO error
`)
}
