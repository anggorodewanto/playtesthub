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
	factory := defaultPlaytestClientFactory(g)

	switch cmd {
	case "version":
		return runVersion(stdout, stderr, cmdArgs)
	case "doctor":
		return runDoctor(ctx, stdout, stderr, g, cmdArgs, factory)
	case "playtest":
		return runPlaytest(ctx, stdout, stderr, g, cmdArgs, factory)
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

Commands (M1 phase 10.1):
  version                          Print build SHA, proto schema, Go version.
  doctor                           Probe the backend. Reports gRPC code + latency.
  playtest get-public --slug <s>   Fetch the public view of a playtest (unauth).
  help                             Show this message.

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
