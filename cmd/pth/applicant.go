package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"google.golang.org/protobuf/proto"
)

const applicantUsage = `applicant: action required (one of: signup, status, accept-nda, list, approve, reject, retry-dm, get-code)`

// runApplicant dispatches `pth applicant <action> ...`. M1 actions
// (signup, status) take a slug rather than a playtest id — the proto
// request fields are `slug`. M2 actions are split between player-token
// flows keyed by playtest id (accept-nda, get-code) and admin-token flows
// keyed by applicant id (approve, reject, retry-dm) or playtest id (list).
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
	case "accept-nda":
		return runApplicantAcceptNDA(ctx, stdout, stderr, g, rest, factory)
	case actionList:
		return runApplicantList(ctx, stdout, stderr, g, rest, factory)
	case "approve":
		return runApplicantApprove(ctx, stdout, stderr, g, rest, factory)
	case "reject":
		return runApplicantReject(ctx, stdout, stderr, g, rest, factory)
	case "retry-dm":
		return runApplicantRetryDM(ctx, stdout, stderr, g, rest, factory)
	case "get-code":
		return runApplicantGetCode(ctx, stdout, stderr, g, rest, factory)
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

// runApplicantAcceptNDA wraps AcceptNDA. cli.md §6.2 surfaces this as
// `--playtest <id>` because the proto field is playtest_id; the player
// token in the credential profile identifies the applicant.
func runApplicantAcceptNDA(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("applicant accept-nda", flag.ContinueOnError)
	fs.SetOutput(stderr)
	playtestID := fs.String("playtest", "", "playtest id (required)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *playtestID == "" {
		fmt.Fprintln(stderr, "applicant accept-nda: --playtest is required")
		return exitLocalError
	}
	req := &pb.AcceptNDARequest{PlaytestId: *playtestID}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "AcceptNDA", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.AcceptNDA(cctx, req)
		})
}

// runApplicantList wraps ListApplicants. Admin token required; --status
// and --dm-failed are server-side filters per proto §680–691; --cursor is
// the opaque page_token round-tripped from a prior response.
func runApplicantList(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("applicant list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	playtestID := fs.String("playtest", "", "playtest id (required)")
	statusFilter := fs.String("status", "", "applicant status filter: PENDING | APPROVED | REJECTED")
	dmFailed := fs.Bool("dm-failed", false, "only rows where last_dm_status='failed'")
	cursor := fs.String("cursor", "", "opaque page_token from a prior response")
	pageSize := fs.Int("page-size", 0, "page size (0 → server default 50)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *playtestID == "" {
		fmt.Fprintln(stderr, "applicant list: --playtest is required")
		return exitLocalError
	}
	if g.Namespace == "" {
		fmt.Fprintln(stderr, "applicant list: --namespace (or PTH_NAMESPACE) is required")
		return exitLocalError
	}
	statusEnum, err := parseApplicantStatus(*statusFilter)
	if err != nil {
		fmt.Fprintf(stderr, "applicant list: %v\n", err)
		return exitLocalError
	}
	req := &pb.ListApplicantsRequest{
		Namespace:      g.Namespace,
		PlaytestId:     *playtestID,
		StatusFilter:   statusEnum,
		DmFailedFilter: *dmFailed,
		PageToken:      *cursor,
		PageSize:       int32(*pageSize),
	}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "ListApplicants", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.ListApplicants(cctx, req)
		})
}

func runApplicantApprove(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("applicant approve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("id", "", "applicant id (required)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *id == "" {
		fmt.Fprintln(stderr, "applicant approve: --id is required")
		return exitLocalError
	}
	if g.Namespace == "" {
		fmt.Fprintln(stderr, "applicant approve: --namespace (or PTH_NAMESPACE) is required")
		return exitLocalError
	}
	req := &pb.ApproveApplicantRequest{Namespace: g.Namespace, ApplicantId: *id}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "ApproveApplicant", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.ApproveApplicant(cctx, req)
		})
}

func runApplicantReject(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("applicant reject", flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("id", "", "applicant id (required)")
	reason := fs.String("reason", "", "admin-visible rejection reason (≤500 chars)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *id == "" {
		fmt.Fprintln(stderr, "applicant reject: --id is required")
		return exitLocalError
	}
	if g.Namespace == "" {
		fmt.Fprintln(stderr, "applicant reject: --namespace (or PTH_NAMESPACE) is required")
		return exitLocalError
	}
	req := &pb.RejectApplicantRequest{Namespace: g.Namespace, ApplicantId: *id}
	if *reason != "" {
		v := *reason
		req.RejectionReason = &v
	}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "RejectApplicant", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.RejectApplicant(cctx, req)
		})
}

func runApplicantRetryDM(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("applicant retry-dm", flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("id", "", "applicant id (required)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *id == "" {
		fmt.Fprintln(stderr, "applicant retry-dm: --id is required")
		return exitLocalError
	}
	if g.Namespace == "" {
		fmt.Fprintln(stderr, "applicant retry-dm: --namespace (or PTH_NAMESPACE) is required")
		return exitLocalError
	}
	req := &pb.RetryDMRequest{Namespace: g.Namespace, ApplicantId: *id}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "RetryDM", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.RetryDM(cctx, req)
		})
}

// runApplicantGetCode wraps GetGrantedCode. Player-token RPC; cli.md §6.2
// surfaces this as `--playtest <id>` for symmetry with accept-nda.
func runApplicantGetCode(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("applicant get-code", flag.ContinueOnError)
	fs.SetOutput(stderr)
	playtestID := fs.String("playtest", "", "playtest id (required)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *playtestID == "" {
		fmt.Fprintln(stderr, "applicant get-code: --playtest is required")
		return exitLocalError
	}
	req := &pb.GetGrantedCodeRequest{PlaytestId: *playtestID}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "GetGrantedCode", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.GetGrantedCode(cctx, req)
		})
}

// parseApplicantStatus maps short-form (PENDING/APPROVED/REJECTED) or
// full (APPLICANT_STATUS_*) status strings onto the proto enum. Empty
// input → UNSPECIFIED (the proto sentinel for "no filter").
func parseApplicantStatus(s string) (pb.ApplicantStatus, error) {
	token := strings.ToUpper(strings.TrimSpace(s))
	if token == "" {
		return pb.ApplicantStatus_APPLICANT_STATUS_UNSPECIFIED, nil
	}
	full := token
	if !strings.HasPrefix(full, "APPLICANT_STATUS_") {
		full = "APPLICANT_STATUS_" + full
	}
	v, ok := pb.ApplicantStatus_value[full]
	if !ok || pb.ApplicantStatus(v) == pb.ApplicantStatus_APPLICANT_STATUS_UNSPECIFIED {
		return 0, fmt.Errorf("--status: unknown value %q (valid: PENDING, APPROVED, REJECTED)", s)
	}
	return pb.ApplicantStatus(v), nil
}

// readFile reads the file at path. Used by `code upload` to slurp the CSV
// payload before constructing the request. Stdin via "-" matches the curl
// convention used elsewhere in the CLI (readMaybeFile in playtest.go).
func readFile(path string) ([]byte, error) {
	if path == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("reading stdin: %w", err)
		}
		return b, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return b, nil
}
