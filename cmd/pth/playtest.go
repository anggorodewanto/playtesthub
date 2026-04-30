package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"google.golang.org/grpc/status"
)

// playtestClientFactory returns a configured PlaytesthubServiceClient and
// a cleanup func. Tests inject a stub via this seam; production wires it
// to (*Globals).dial.
type playtestClientFactory func(ctx context.Context) (pb.PlaytesthubServiceClient, context.Context, func() error, error)

// runPlaytest dispatches `pth playtest <action> ...`. Action gating stays
// in this single function so the catalogue is grep-friendly.
func runPlaytest(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "playtest: action required (one of: get-public)")
		return exitLocalError
	}
	action, rest := args[0], args[1:]
	switch action {
	case "get-public":
		return runPlaytestGetPublic(ctx, stdout, stderr, g, rest, factory)
	default:
		fmt.Fprintf(stderr, "playtest: unknown action %q\n", action)
		return exitLocalError
	}
}

func runPlaytestGetPublic(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("playtest get-public", flag.ContinueOnError)
	fs.SetOutput(stderr)
	slug := fs.String("slug", "", "playtest slug (required)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *slug == "" {
		fmt.Fprintln(stderr, "playtest get-public: --slug is required")
		return exitLocalError
	}

	// `--anon` is implied per cli.md §6.1; we coerce it on so a stale token
	// in the env doesn't get attached to an unauth RPC.
	g.Anon = true

	req := &pb.GetPublicPlaytestRequest{Slug: *slug}

	if *dryRun {
		// Dry-run prints the request body and exits 0 without dialling
		// (cli.md §10 — agent affordance: validate request shape before
		// committing an action).
		if err := writeJSONProto(stdout, req); err != nil {
			fmt.Fprintf(stderr, "playtest get-public: %v\n", err)
			return exitLocalError
		}
		return exitOK
	}

	client, callCtx, closeFn, err := factory(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "playtest get-public: %v\n", err)
		return exitTransportError
	}
	defer closeFn() //nolint:errcheck // best-effort close on a CLI exit path.

	g.logRPC(stderr, "GetPublicPlaytest")
	callCtx, cancel := context.WithTimeout(callCtx, g.Timeout)
	defer cancel()

	resp, err := client.GetPublicPlaytest(callCtx, req)
	if err != nil {
		writeGRPCError(stderr, err)
		st, _ := status.FromError(err)
		return exitCodeForGRPC(st.Code())
	}
	if err := writeJSONProto(stdout, resp); err != nil {
		fmt.Fprintf(stderr, "playtest get-public: %v\n", err)
		return exitLocalError
	}
	return exitOK
}
