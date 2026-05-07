package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"google.golang.org/protobuf/proto"
)

const codeUsage = `code: action required (one of: upload, top-up, sync-from-ags, pool)`

// runCode dispatches `pth code <action> ...`. Every action is admin-token
// keyed by --playtest <id>. cli.md §6.2.
func runCode(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, codeUsage)
		return exitLocalError
	}
	action, rest := args[0], args[1:]
	switch action {
	case "upload":
		return runCodeUpload(ctx, stdout, stderr, g, rest, factory)
	case "top-up":
		return runCodeTopUp(ctx, stdout, stderr, g, rest, factory)
	case "sync-from-ags":
		return runCodeSyncFromAGS(ctx, stdout, stderr, g, rest, factory)
	case "pool":
		return runCodePool(ctx, stdout, stderr, g, rest, factory)
	default:
		fmt.Fprintf(stderr, "code: unknown action %q\n", action)
		return exitLocalError
	}
}

// runCodeUpload wraps UploadCodes. STEAM_KEYS-only per PRD §4.3 — the
// service rejects AGS_CAMPAIGN uploads. The CSV body is read into memory
// (the proto cap is 10 MB so this is fine for a CLI surface).
func runCodeUpload(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("code upload", flag.ContinueOnError)
	fs.SetOutput(stderr)
	playtestID := fs.String("playtest", "", "playtest id (required)")
	file := fs.String("file", "", "path to CSV file (required; '-' reads stdin)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON (without csv_content bytes) and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *playtestID == "" {
		fmt.Fprintln(stderr, "code upload: --playtest is required")
		return exitLocalError
	}
	if !g.requireNamespace(stderr, "code upload") {
		return exitLocalError
	}
	if *file == "" {
		fmt.Fprintln(stderr, "code upload: --file is required")
		return exitLocalError
	}
	body, err := readFile(*file)
	if err != nil {
		fmt.Fprintf(stderr, "code upload: %v\n", err)
		return exitLocalError
	}
	filename := ""
	if *file != "-" {
		filename = filepath.Base(*file)
	}
	req := &pb.UploadCodesRequest{
		Namespace:  g.Namespace,
		PlaytestId: *playtestID,
		CsvContent: string(body),
		Filename:   filename,
	}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "UploadCodes", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.UploadCodes(cctx, req)
		})
}

// runCodeTopUp wraps TopUpCodes. AGS_CAMPAIGN-only — server returns
// FailedPrecondition for STEAM_KEYS. --quantity is bounded by the proto
// shape; server enforces 1..50_000.
func runCodeTopUp(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("code top-up", flag.ContinueOnError)
	fs.SetOutput(stderr)
	playtestID := fs.String("playtest", "", "playtest id (required)")
	quantity := fs.Int("quantity", 0, "number of codes to generate via AGS Campaign API (required, 1..50000)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *playtestID == "" {
		fmt.Fprintln(stderr, "code top-up: --playtest is required")
		return exitLocalError
	}
	if !g.requireNamespace(stderr, "code top-up") {
		return exitLocalError
	}
	if *quantity <= 0 {
		fmt.Fprintln(stderr, "code top-up: --quantity is required (positive integer)")
		return exitLocalError
	}
	req := &pb.TopUpCodesRequest{
		Namespace:  g.Namespace,
		PlaytestId: *playtestID,
		Quantity:   int32(*quantity),
	}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "TopUpCodes", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.TopUpCodes(cctx, req)
		})
}

func runCodeSyncFromAGS(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("code sync-from-ags", flag.ContinueOnError)
	fs.SetOutput(stderr)
	playtestID := fs.String("playtest", "", "playtest id (required)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *playtestID == "" {
		fmt.Fprintln(stderr, "code sync-from-ags: --playtest is required")
		return exitLocalError
	}
	if !g.requireNamespace(stderr, "code sync-from-ags") {
		return exitLocalError
	}
	req := &pb.SyncFromAGSRequest{Namespace: g.Namespace, PlaytestId: *playtestID}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "SyncFromAGS", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.SyncFromAGS(cctx, req)
		})
}

func runCodePool(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("code pool", flag.ContinueOnError)
	fs.SetOutput(stderr)
	playtestID := fs.String("playtest", "", "playtest id (required)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *playtestID == "" {
		fmt.Fprintln(stderr, "code pool: --playtest is required")
		return exitLocalError
	}
	if !g.requireNamespace(stderr, "code pool") {
		return exitLocalError
	}
	req := &pb.GetCodePoolRequest{Namespace: g.Namespace, PlaytestId: *playtestID}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "GetCodePool", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.GetCodePool(cctx, req)
		})
}
