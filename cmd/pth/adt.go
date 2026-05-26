package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"google.golang.org/protobuf/proto"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
)

// runADT is the entry point for the `pth adt …` group introduced in
// M5.B phase 9 (linkage / build) and extended in B12 (games).
// Subcommands:
//
//	adt linkage list                          — list studio linkages
//	adt linkage start                         — mint a linkUrl + state
//	adt linkage complete --state --adt-namespace
//	adt linkage unlink   --id <adt_linkage_id>
//	adt linkage recover  --adt-namespace <ns>
//	adt build   list     --linkage-id <id> --game-id <id>
//	adt games   list     --linkage-id <id>
//	adt diagnostics                           — report which adt.Client kind was wired
//
// PRD §4.8 / STATUS_M5.md B9 + B12.
func runADT(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "adt: usage: pth adt {linkage|build|games|diagnostics} ...")
		return exitLocalError
	}
	group, rest := args[0], args[1:]
	switch group {
	case "linkage":
		return runADTLinkage(ctx, stdout, stderr, g, rest, factory)
	case "build":
		return runADTBuild(ctx, stdout, stderr, g, rest, factory)
	case "games":
		return runADTGames(ctx, stdout, stderr, g, rest, factory)
	case "diagnostics":
		return runADTDiagnostics(ctx, stdout, stderr, g, rest, factory)
	default:
		fmt.Fprintf(stderr, "adt: unknown group %q\n", group)
		return exitLocalError
	}
}

func runADTDiagnostics(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("adt diagnostics", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if !g.requireNamespace(stderr, "adt diagnostics") {
		return exitLocalError
	}
	req := &pb.GetADTClientDiagnosticsRequest{Namespace: g.Namespace}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "GetADTClientDiagnostics", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.GetADTClientDiagnostics(cctx, req)
		})
}

func runADTLinkage(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "adt linkage: usage: pth adt linkage {list|start|complete|unlink|recover} ...")
		return exitLocalError
	}
	action, rest := args[0], args[1:]
	switch action {
	case actionList:
		return runADTLinkageList(ctx, stdout, stderr, g, rest, factory)
	case "start":
		return runADTLinkageStart(ctx, stdout, stderr, g, rest, factory)
	case "complete":
		return runADTLinkageComplete(ctx, stdout, stderr, g, rest, factory)
	case "unlink":
		return runADTLinkageUnlink(ctx, stdout, stderr, g, rest, factory)
	case "recover":
		return runADTLinkageRecover(ctx, stdout, stderr, g, rest, factory)
	default:
		fmt.Fprintf(stderr, "adt linkage: unknown action %q\n", action)
		return exitLocalError
	}
}

func runADTLinkageList(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("adt linkage list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if !g.requireNamespace(stderr, "adt linkage list") {
		return exitLocalError
	}
	req := &pb.ListADTLinkagesRequest{Namespace: g.Namespace}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "ListADTLinkages", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.ListADTLinkages(cctx, req)
		})
}

func runADTLinkageStart(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("adt linkage start", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if !g.requireNamespace(stderr, "adt linkage start") {
		return exitLocalError
	}
	req := &pb.StartADTLinkRequest{Namespace: g.Namespace}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "StartADTLink", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.StartADTLink(cctx, req)
		})
}

func runADTLinkageComplete(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("adt linkage complete", flag.ContinueOnError)
	fs.SetOutput(stderr)
	state := fs.String("state", "", "linking state nonce returned from `adt linkage start` (required)")
	adtNS := fs.String("adt-namespace", "", "ADT namespace echoed by the redirect-back URL (required)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *state == "" {
		fmt.Fprintln(stderr, "adt linkage complete: --state is required")
		return exitLocalError
	}
	if *adtNS == "" {
		fmt.Fprintln(stderr, "adt linkage complete: --adt-namespace is required")
		return exitLocalError
	}
	if !g.requireNamespace(stderr, "adt linkage complete") {
		return exitLocalError
	}
	req := &pb.CompleteADTLinkRequest{
		Namespace:    g.Namespace,
		State:        *state,
		AdtNamespace: *adtNS,
	}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "CompleteADTLink", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.CompleteADTLink(cctx, req)
		})
}

func runADTLinkageUnlink(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("adt linkage unlink", flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("id", "", "adt_linkage_id to unlink (required)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *id == "" {
		fmt.Fprintln(stderr, "adt linkage unlink: --id is required")
		return exitLocalError
	}
	if !g.requireNamespace(stderr, "adt linkage unlink") {
		return exitLocalError
	}
	req := &pb.UnlinkADTRequest{Namespace: g.Namespace, AdtLinkageId: *id}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "UnlinkADT", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.UnlinkADT(cctx, req)
		})
}

func runADTLinkageRecover(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("adt linkage recover", flag.ContinueOnError)
	fs.SetOutput(stderr)
	adtNS := fs.String("adt-namespace", "", "ADT namespace whose orphan flag to adopt (required)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *adtNS == "" {
		fmt.Fprintln(stderr, "adt linkage recover: --adt-namespace is required")
		return exitLocalError
	}
	if !g.requireNamespace(stderr, "adt linkage recover") {
		return exitLocalError
	}
	req := &pb.RecoverADTLinkageRequest{Namespace: g.Namespace, AdtNamespace: *adtNS}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "RecoverADTLinkage", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.RecoverADTLinkage(cctx, req)
		})
}

func runADTBuild(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "adt build: usage: pth adt build {list|change|check} ...")
		return exitLocalError
	}
	action, rest := args[0], args[1:]
	switch action {
	case actionList:
		return runADTBuildList(ctx, stdout, stderr, g, rest, factory)
	case "change":
		return runADTBuildChange(ctx, stdout, stderr, g, rest, factory)
	case "check":
		return runADTBuildCheck(ctx, stdout, stderr, g, rest, factory)
	default:
		fmt.Fprintf(stderr, "adt build: unknown action %q\n", action)
		return exitLocalError
	}
}

func runADTBuildList(ctx context.Context, stdout, stderr io.Writer, g *Globals, rest []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("adt build list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	linkageID := fs.String("linkage-id", "", "adt_linkage_id (required)")
	gameID := fs.String("game-id", "", "ADT game id (required)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(rest); err != nil {
		return exitLocalError
	}
	if *linkageID == "" {
		fmt.Fprintln(stderr, "adt build list: --linkage-id is required")
		return exitLocalError
	}
	if *gameID == "" {
		fmt.Fprintln(stderr, "adt build list: --game-id is required")
		return exitLocalError
	}
	if !g.requireNamespace(stderr, "adt build list") {
		return exitLocalError
	}
	req := &pb.ListADTBuildsRequest{
		Namespace:    g.Namespace,
		AdtLinkageId: *linkageID,
		AdtGameId:    *gameID,
	}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "ListADTBuilds", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.ListADTBuilds(cctx, req)
		})
}

func runADTBuildChange(ctx context.Context, stdout, stderr io.Writer, g *Globals, rest []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("adt build change", flag.ContinueOnError)
	fs.SetOutput(stderr)
	playtestID := fs.String("playtest-id", "", "playtest_id whose build to change (required)")
	gameID := fs.String("game-id", "", "new ADT game id (required)")
	buildID := fs.String("build-id", "", "new ADT build id (required)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(rest); err != nil {
		return exitLocalError
	}
	if *playtestID == "" {
		fmt.Fprintln(stderr, "adt build change: --playtest-id is required")
		return exitLocalError
	}
	if *gameID == "" {
		fmt.Fprintln(stderr, "adt build change: --game-id is required")
		return exitLocalError
	}
	if *buildID == "" {
		fmt.Fprintln(stderr, "adt build change: --build-id is required")
		return exitLocalError
	}
	if !g.requireNamespace(stderr, "adt build change") {
		return exitLocalError
	}
	req := &pb.ChangeADTBuildRequest{
		Namespace:  g.Namespace,
		PlaytestId: *playtestID,
		AdtGameId:  *gameID,
		AdtBuildId: *buildID,
	}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "ChangeADTBuild", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.ChangeADTBuild(cctx, req)
		})
}

func runADTBuildCheck(ctx context.Context, stdout, stderr io.Writer, g *Globals, rest []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("adt build check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	playtestID := fs.String("playtest-id", "", "playtest_id whose build health to probe (required)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(rest); err != nil {
		return exitLocalError
	}
	if *playtestID == "" {
		fmt.Fprintln(stderr, "adt build check: --playtest-id is required")
		return exitLocalError
	}
	if !g.requireNamespace(stderr, "adt build check") {
		return exitLocalError
	}
	req := &pb.CheckADTBuildRequest{
		Namespace:  g.Namespace,
		PlaytestId: *playtestID,
	}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "CheckADTBuild", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.CheckADTBuild(cctx, req)
		})
}

func runADTGames(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "adt games: usage: pth adt games list --linkage-id <id>")
		return exitLocalError
	}
	action, rest := args[0], args[1:]
	if action != actionList {
		fmt.Fprintf(stderr, "adt games: unknown action %q\n", action)
		return exitLocalError
	}
	fs := flag.NewFlagSet("adt games list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	linkageID := fs.String("linkage-id", "", "adt_linkage_id (required)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(rest); err != nil {
		return exitLocalError
	}
	if *linkageID == "" {
		fmt.Fprintln(stderr, "adt games list: --linkage-id is required")
		return exitLocalError
	}
	if !g.requireNamespace(stderr, "adt games list") {
		return exitLocalError
	}
	req := &pb.ListADTGamesRequest{
		Namespace:    g.Namespace,
		AdtLinkageId: *linkageID,
	}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "ListADTGames", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.ListADTGames(cctx, req)
		})
}
