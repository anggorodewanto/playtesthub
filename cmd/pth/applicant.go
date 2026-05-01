package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"google.golang.org/protobuf/proto"
)

const applicantUsage = `applicant: action required (one of: signup, status)`

// runApplicant dispatches `pth applicant <action> ...`. Both M1 actions
// take a slug rather than a playtest id — the proto request fields are
// `slug` (cli.md §6.1's `--playtest <id>` is loose phrasing for "the
// playtest's slug"; we expose --slug for symmetry with `playtest
// get-public --slug`).
func runApplicant(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, applicantUsage)
		return exitLocalError
	}
	action, rest := args[0], args[1:]
	switch action {
	case "signup":
		return runApplicantSignup(ctx, stdout, stderr, g, rest, factory)
	case "status":
		return runApplicantStatus(ctx, stdout, stderr, g, rest, factory)
	default:
		fmt.Fprintf(stderr, "applicant: unknown action %q\n", action)
		return exitLocalError
	}
}

func runApplicantSignup(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("applicant signup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	slug := fs.String("slug", "", "playtest slug (required)")
	platformsCSV := fs.String("platforms", "", "comma-separated platforms owned (required, e.g. STEAM,XBOX)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *slug == "" {
		fmt.Fprintln(stderr, "applicant signup: --slug is required")
		return exitLocalError
	}
	platforms, err := parsePlatforms(*platformsCSV)
	if err != nil {
		fmt.Fprintf(stderr, "applicant signup: %v\n", err)
		return exitLocalError
	}
	if len(platforms) == 0 {
		fmt.Fprintln(stderr, "applicant signup: --platforms is required (at least one of STEAM,XBOX,PLAYSTATION,EPIC,OTHER)")
		return exitLocalError
	}
	req := &pb.SignupRequest{Slug: *slug, Platforms: platforms}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "Signup", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.Signup(cctx, req)
		})
}

func runApplicantStatus(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("applicant status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	slug := fs.String("slug", "", "playtest slug (required)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *slug == "" {
		fmt.Fprintln(stderr, "applicant status: --slug is required")
		return exitLocalError
	}
	req := &pb.GetApplicantStatusRequest{Slug: *slug}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "GetApplicantStatus", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.GetApplicantStatus(cctx, req)
		})
}
