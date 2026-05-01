package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// playtestClientFactory returns a configured PlaytesthubServiceClient and
// a cleanup func. Tests inject a stub via this seam; production wires it
// to (*Globals).dial.
type playtestClientFactory func(ctx context.Context) (pb.PlaytesthubServiceClient, context.Context, func() error, error)

const playtestUsage = `playtest: action required (one of: get-public, get-player, get, list, create, edit, delete, transition)`

func runPlaytest(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, playtestUsage)
		return exitLocalError
	}
	action, rest := args[0], args[1:]
	switch action {
	case "get-public":
		return runPlaytestGetPublic(ctx, stdout, stderr, g, rest, factory)
	case "get-player":
		return runPlaytestGetPlayer(ctx, stdout, stderr, g, rest, factory)
	case "get":
		return runPlaytestGet(ctx, stdout, stderr, g, rest, factory)
	case "list":
		return runPlaytestList(ctx, stdout, stderr, g, rest, factory)
	case "create":
		return runPlaytestCreate(ctx, stdout, stderr, g, rest, factory)
	case "edit":
		return runPlaytestEdit(ctx, stdout, stderr, g, rest, factory)
	case "delete":
		return runPlaytestDelete(ctx, stdout, stderr, g, rest, factory)
	case "transition":
		return runPlaytestTransition(ctx, stdout, stderr, g, rest, factory)
	default:
		fmt.Fprintf(stderr, "playtest: unknown action %q\n", action)
		return exitLocalError
	}
}

// invokePlaytest is the shared dial+RPC path used by every playtest +
// applicant subcommand. It honours --dry-run (prints the request JSON
// and exits 0 without dialling), enforces the per-call timeout, and
// maps gRPC status codes to cli.md §8 exit codes. Each subcommand only
// has to assemble the request and supply an `invoke` closure.
func invokePlaytest(
	ctx context.Context, stdout, stderr io.Writer, g *Globals, factory playtestClientFactory,
	label string, req proto.Message, dryRun bool,
	invoke func(client pb.PlaytesthubServiceClient, ctx context.Context) (proto.Message, error),
) int {
	if dryRun {
		if err := writeJSONProto(stdout, req); err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", label, err)
			return exitLocalError
		}
		return exitOK
	}
	client, callCtx, closeFn, err := factory(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", label, err)
		return exitTransportError
	}
	defer closeFn() //nolint:errcheck // best-effort close on a CLI exit path.
	g.logRPC(stderr, label)
	callCtx, cancel := context.WithTimeout(callCtx, g.Timeout)
	defer cancel()
	resp, err := invoke(client, callCtx)
	if err != nil {
		writeGRPCError(stderr, err)
		st, _ := status.FromError(err)
		return exitCodeForGRPC(st.Code())
	}
	if err := writeJSONProto(stdout, resp); err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", label, err)
		return exitLocalError
	}
	return exitOK
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

	return invokePlaytest(ctx, stdout, stderr, g, factory, "GetPublicPlaytest", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.GetPublicPlaytest(cctx, req)
		})
}

func runPlaytestGetPlayer(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("playtest get-player", flag.ContinueOnError)
	fs.SetOutput(stderr)
	slug := fs.String("slug", "", "playtest slug (required)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *slug == "" {
		fmt.Fprintln(stderr, "playtest get-player: --slug is required")
		return exitLocalError
	}
	req := &pb.GetPlaytestForPlayerRequest{Slug: *slug}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "GetPlaytestForPlayer", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.GetPlaytestForPlayer(cctx, req)
		})
}

func runPlaytestGet(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("playtest get", flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("id", "", "playtest id (required)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *id == "" {
		fmt.Fprintln(stderr, "playtest get: --id is required")
		return exitLocalError
	}
	if g.Namespace == "" {
		fmt.Fprintln(stderr, "playtest get: --namespace (or PTH_NAMESPACE) is required")
		return exitLocalError
	}
	req := &pb.AdminGetPlaytestRequest{Namespace: g.Namespace, PlaytestId: *id}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "AdminGetPlaytest", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.AdminGetPlaytest(cctx, req)
		})
}

func runPlaytestList(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("playtest list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if g.Namespace == "" {
		fmt.Fprintln(stderr, "playtest list: --namespace (or PTH_NAMESPACE) is required")
		return exitLocalError
	}
	req := &pb.ListPlaytestsRequest{Namespace: g.Namespace}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "ListPlaytests", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.ListPlaytests(cctx, req)
		})
}

func runPlaytestCreate(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("playtest create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	slug := fs.String("slug", "", "playtest slug (required, PRD §5.1 regex)")
	title := fs.String("title", "", "playtest title (required)")
	description := fs.String("description", "", "playtest description")
	bannerImageURL := fs.String("banner-image-url", "", "banner image URL")
	platformsCSV := fs.String("platforms", "", "comma-separated platforms (STEAM,XBOX,PLAYSTATION,EPIC,OTHER)")
	startsAt := fs.String("starts-at", "", "RFC3339 timestamp")
	endsAt := fs.String("ends-at", "", "RFC3339 timestamp")
	ndaRequired := fs.Bool("nda-required", false, "set if the playtest requires NDA acceptance")
	ndaText := fs.String("nda-text", "", "NDA prose; prefix with @ to read from a file (e.g. --nda-text @nda.md)")
	distModel := fs.String("distribution-model", "STEAM_KEYS", "STEAM_KEYS | AGS_CAMPAIGN")
	initialQuantity := fs.String("initial-code-quantity", "", "initial code quantity (AGS_CAMPAIGN only)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *slug == "" {
		fmt.Fprintln(stderr, "playtest create: --slug is required")
		return exitLocalError
	}
	if *title == "" {
		fmt.Fprintln(stderr, "playtest create: --title is required")
		return exitLocalError
	}
	if g.Namespace == "" {
		fmt.Fprintln(stderr, "playtest create: --namespace (or PTH_NAMESPACE) is required")
		return exitLocalError
	}
	platforms, err := parsePlatforms(*platformsCSV)
	if err != nil {
		fmt.Fprintf(stderr, "playtest create: %v\n", err)
		return exitLocalError
	}
	startsTS, err := parseTimestampFlag(*startsAt)
	if err != nil {
		fmt.Fprintf(stderr, "playtest create: --starts-at %v\n", err)
		return exitLocalError
	}
	endsTS, err := parseTimestampFlag(*endsAt)
	if err != nil {
		fmt.Fprintf(stderr, "playtest create: --ends-at %v\n", err)
		return exitLocalError
	}
	ndaProse, err := readMaybeFile(*ndaText)
	if err != nil {
		fmt.Fprintf(stderr, "playtest create: --nda-text %v\n", err)
		return exitLocalError
	}
	dm, err := parseDistributionModel(*distModel)
	if err != nil {
		fmt.Fprintf(stderr, "playtest create: %v\n", err)
		return exitLocalError
	}
	req := &pb.CreatePlaytestRequest{
		Namespace:         g.Namespace,
		Slug:              *slug,
		Title:             *title,
		Description:       *description,
		BannerImageUrl:    *bannerImageURL,
		Platforms:         platforms,
		StartsAt:          startsTS,
		EndsAt:            endsTS,
		NdaRequired:       *ndaRequired,
		NdaText:           ndaProse,
		DistributionModel: dm,
	}
	if *initialQuantity != "" {
		n, err := strconv.ParseInt(*initialQuantity, 10, 32)
		if err != nil {
			fmt.Fprintf(stderr, "playtest create: --initial-code-quantity %q: %v\n", *initialQuantity, err)
			return exitLocalError
		}
		v := int32(n)
		req.InitialCodeQuantity = &v
	}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "CreatePlaytest", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.CreatePlaytest(cctx, req)
		})
}

// editImmutableFlags is the set of `--<name>` tokens that are NOT mutable
// per PRD §5.1. The flagset alone would surface them as "unknown flag",
// but a tailored message points the user at the actual constraint.
var editImmutableFlags = map[string]string{
	"--slug":                  "slug is immutable (PRD §5.1) — create a new playtest instead",
	"--status":                "status is mutated via `playtest transition`, not edit",
	"--distribution-model":    "distribution_model is fixed at creation (PRD §5.1)",
	"--initial-code-quantity": "initial_code_quantity is fixed at creation (PRD §5.1)",
	"--ags-item-id":           "ags_item_id is owned by the AGS sync job (M2)",
	"--ags-campaign-id":       "ags_campaign_id is owned by the AGS sync job (M2)",
	"--namespace-field":       "namespace is fixed at creation",
}

func runPlaytestEdit(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	if rejected := detectImmutableEditFlags(args); rejected != "" {
		fmt.Fprintf(stderr, "playtest edit: %s\n", editImmutableFlags[rejected])
		return exitLocalError
	}
	fs := flag.NewFlagSet("playtest edit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("id", "", "playtest id (required)")
	title := fs.String("title", "", "playtest title")
	description := fs.String("description", "", "playtest description")
	bannerImageURL := fs.String("banner-image-url", "", "banner image URL")
	platformsCSV := fs.String("platforms", "", "comma-separated platforms")
	startsAt := fs.String("starts-at", "", "RFC3339 timestamp")
	endsAt := fs.String("ends-at", "", "RFC3339 timestamp")
	ndaRequired := fs.Bool("nda-required", false, "NDA required")
	ndaText := fs.String("nda-text", "", "NDA prose; @file to load from disk")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *id == "" {
		fmt.Fprintln(stderr, "playtest edit: --id is required")
		return exitLocalError
	}
	if g.Namespace == "" {
		fmt.Fprintln(stderr, "playtest edit: --namespace (or PTH_NAMESPACE) is required")
		return exitLocalError
	}
	platforms, err := parsePlatforms(*platformsCSV)
	if err != nil {
		fmt.Fprintf(stderr, "playtest edit: %v\n", err)
		return exitLocalError
	}
	startsTS, err := parseTimestampFlag(*startsAt)
	if err != nil {
		fmt.Fprintf(stderr, "playtest edit: --starts-at %v\n", err)
		return exitLocalError
	}
	endsTS, err := parseTimestampFlag(*endsAt)
	if err != nil {
		fmt.Fprintf(stderr, "playtest edit: --ends-at %v\n", err)
		return exitLocalError
	}
	ndaProse, err := readMaybeFile(*ndaText)
	if err != nil {
		fmt.Fprintf(stderr, "playtest edit: --nda-text %v\n", err)
		return exitLocalError
	}
	req := &pb.EditPlaytestRequest{
		Namespace:      g.Namespace,
		PlaytestId:     *id,
		Title:          *title,
		Description:    *description,
		BannerImageUrl: *bannerImageURL,
		Platforms:      platforms,
		StartsAt:       startsTS,
		EndsAt:         endsTS,
		NdaRequired:    *ndaRequired,
		NdaText:        ndaProse,
	}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "EditPlaytest", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.EditPlaytest(cctx, req)
		})
}

// detectImmutableEditFlags scans args left-to-right for any token in
// editImmutableFlags. Matches both `--flag value` and `--flag=value`
// forms. Returns the canonical flag (with --) on first hit; "" if none.
func detectImmutableEditFlags(args []string) string {
	for _, a := range args {
		key, _, _ := splitFlag(a)
		if _, hit := editImmutableFlags[key]; hit {
			return key
		}
	}
	return ""
}

func runPlaytestDelete(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("playtest delete", flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("id", "", "playtest id (required)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *id == "" {
		fmt.Fprintln(stderr, "playtest delete: --id is required")
		return exitLocalError
	}
	if g.Namespace == "" {
		fmt.Fprintln(stderr, "playtest delete: --namespace (or PTH_NAMESPACE) is required")
		return exitLocalError
	}
	req := &pb.SoftDeletePlaytestRequest{Namespace: g.Namespace, PlaytestId: *id}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "SoftDeletePlaytest", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.SoftDeletePlaytest(cctx, req)
		})
}

func runPlaytestTransition(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("playtest transition", flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("id", "", "playtest id (required)")
	to := fs.String("to", "", "target status: DRAFT | OPEN | CLOSED (required)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *id == "" {
		fmt.Fprintln(stderr, "playtest transition: --id is required")
		return exitLocalError
	}
	if *to == "" {
		fmt.Fprintln(stderr, "playtest transition: --to is required (DRAFT | OPEN | CLOSED)")
		return exitLocalError
	}
	if g.Namespace == "" {
		fmt.Fprintln(stderr, "playtest transition: --namespace (or PTH_NAMESPACE) is required")
		return exitLocalError
	}
	target, err := parsePlaytestStatus(*to)
	if err != nil {
		fmt.Fprintf(stderr, "playtest transition: %v\n", err)
		return exitLocalError
	}
	req := &pb.TransitionPlaytestStatusRequest{
		Namespace:    g.Namespace,
		PlaytestId:   *id,
		TargetStatus: target,
	}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "TransitionPlaytestStatus", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.TransitionPlaytestStatus(cctx, req)
		})
}

// parsePlatforms splits a comma-separated platform list into proto enums.
// Empty input returns nil (no platforms set). Each token is trimmed,
// uppercased, and matched against both the short form (STEAM) and the
// full enum name (PLATFORM_STEAM).
func parsePlatforms(csv string) ([]pb.Platform, error) {
	if strings.TrimSpace(csv) == "" {
		return nil, nil
	}
	out := make([]pb.Platform, 0)
	for _, raw := range strings.Split(csv, ",") {
		token := strings.ToUpper(strings.TrimSpace(raw))
		if token == "" {
			continue
		}
		full := token
		if !strings.HasPrefix(full, "PLATFORM_") {
			full = "PLATFORM_" + full
		}
		v, ok := pb.Platform_value[full]
		if !ok || pb.Platform(v) == pb.Platform_PLATFORM_UNSPECIFIED {
			return nil, fmt.Errorf("--platforms: unknown platform %q (valid: STEAM, XBOX, PLAYSTATION, EPIC, OTHER)", raw)
		}
		out = append(out, pb.Platform(v))
	}
	return out, nil
}

// parseTimestampFlag accepts an empty string (returns nil) or an RFC3339
// timestamp (returns *timestamppb.Timestamp). Any other input is an
// explicit caller error.
func parseTimestampFlag(s string) (*timestamppb.Timestamp, error) {
	if s == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, fmt.Errorf("expected RFC3339 (e.g. 2026-05-01T12:00:00Z): %w", err)
	}
	return timestamppb.New(t), nil
}

// readMaybeFile returns s verbatim unless it starts with `@`, in which
// case the rest is treated as a filesystem path and the file's contents
// are returned. `@-` reads from stdin (matches the curl convention).
func readMaybeFile(s string) (string, error) {
	if !strings.HasPrefix(s, "@") {
		return s, nil
	}
	path := strings.TrimPrefix(s, "@")
	if path == "-" {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("reading stdin: %w", err)
		}
		return string(b), nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return string(b), nil
}

// parseDistributionModel maps short-form CLI input (STEAM_KEYS,
// AGS_CAMPAIGN) onto the proto enum. The proto value-map already knows
// the full DISTRIBUTION_MODEL_* names, so we accept either form.
func parseDistributionModel(s string) (pb.DistributionModel, error) {
	token := strings.ToUpper(strings.TrimSpace(s))
	if token == "" {
		return pb.DistributionModel_DISTRIBUTION_MODEL_STEAM_KEYS, nil
	}
	full := token
	if !strings.HasPrefix(full, "DISTRIBUTION_MODEL_") {
		full = "DISTRIBUTION_MODEL_" + full
	}
	v, ok := pb.DistributionModel_value[full]
	if !ok || pb.DistributionModel(v) == pb.DistributionModel_DISTRIBUTION_MODEL_UNSPECIFIED {
		return 0, fmt.Errorf("--distribution-model: unknown value %q (valid: STEAM_KEYS, AGS_CAMPAIGN)", s)
	}
	return pb.DistributionModel(v), nil
}

// parsePlaytestStatus maps short-form (DRAFT/OPEN/CLOSED) or full
// (PLAYTEST_STATUS_*) status strings onto the proto enum.
func parsePlaytestStatus(s string) (pb.PlaytestStatus, error) {
	token := strings.ToUpper(strings.TrimSpace(s))
	if token == "" {
		return 0, fmt.Errorf("--to: empty target status")
	}
	full := token
	if !strings.HasPrefix(full, "PLAYTEST_STATUS_") {
		full = "PLAYTEST_STATUS_" + full
	}
	v, ok := pb.PlaytestStatus_value[full]
	if !ok || pb.PlaytestStatus(v) == pb.PlaytestStatus_PLAYTEST_STATUS_UNSPECIFIED {
		return 0, fmt.Errorf("--to: unknown status %q (valid: DRAFT, OPEN, CLOSED)", s)
	}
	return pb.PlaytestStatus(v), nil
}
