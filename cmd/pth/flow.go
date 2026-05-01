package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// flowProfileFactory builds a (playtestClientFactory, *Globals) pair for
// the given credential profile. The Globals copy keeps addr / namespace /
// timeout / insecure inherited from the top-level invocation while
// swapping the profile that drives token resolution. Tests inject a stub
// to bypass dialling entirely.
type flowProfileFactory func(base *Globals, profile string) (playtestClientFactory, *Globals)

// defaultFlowProfileFactory wires the production seam: each call clones
// `base`, swaps in the requested profile, and rebuilds the token resolver
// against the credentials store. The same getenv snapshot used by the
// outer dispatch feeds every refresh attempt so token IO stays consistent.
func defaultFlowProfileFactory(getenv envSnapshot) flowProfileFactory {
	return func(base *Globals, profile string) (playtestClientFactory, *Globals) {
		cp := *base
		cp.Profile = profile
		cp.tokenResolver = defaultTokenResolver(&cp, getenv)
		return defaultPlaytestClientFactory(&cp), &cp
	}
}

const flowUsage = `flow: action required (one of: golden-m1)`

func runFlow(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, mk flowProfileFactory) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, flowUsage)
		return exitLocalError
	}
	action, rest := args[0], args[1:]
	switch action {
	case "golden-m1":
		return runFlowGoldenM1(ctx, stdout, stderr, g, rest, mk)
	default:
		fmt.Fprintf(stderr, "flow: unknown action %q\n", action)
		return exitLocalError
	}
}

// runFlowGoldenM1 composes the PRD §4.1 M1 golden flow as four NDJSON
// steps: create-playtest → transition-open → signup → assert-pending.
// Each step writes one JSON line; the flow stops on the first failure
// and exits with the cli.md §8 code matching the failed step's gRPC
// status. --dry-run emits all four request shapes without dialling.
func runFlowGoldenM1(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, mk flowProfileFactory) int {
	fs := flag.NewFlagSet("flow golden-m1", flag.ContinueOnError)
	fs.SetOutput(stderr)
	slug := fs.String("slug", "", "playtest slug (required, PRD §5.1 regex)")
	title := fs.String("title", "", "playtest title (default: 'Playtest <slug>')")
	platformsCSV := fs.String("platforms", "STEAM", "platforms for both create and signup")
	adminProfile := fs.String("admin-profile", "", "credential profile for admin steps (create, transition)")
	playerProfile := fs.String("player-profile", "", "credential profile for player steps (signup, assert-pending)")
	dryRun := fs.Bool("dry-run", false, "print every step's request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}

	if *slug == "" {
		fmt.Fprintln(stderr, "flow golden-m1: --slug is required")
		return exitLocalError
	}
	if g.Namespace == "" {
		fmt.Fprintln(stderr, "flow golden-m1: --namespace (or PTH_NAMESPACE) is required")
		return exitLocalError
	}
	if !*dryRun {
		if *adminProfile == "" {
			fmt.Fprintln(stderr, "flow golden-m1: --admin-profile is required")
			return exitLocalError
		}
		if *playerProfile == "" {
			fmt.Fprintln(stderr, "flow golden-m1: --player-profile is required")
			return exitLocalError
		}
	}

	platforms, err := parsePlatforms(*platformsCSV)
	if err != nil {
		fmt.Fprintf(stderr, "flow golden-m1: %v\n", err)
		return exitLocalError
	}
	if len(platforms) == 0 {
		platforms = []pb.Platform{pb.Platform_PLATFORM_STEAM}
	}
	resolvedTitle := *title
	if resolvedTitle == "" {
		resolvedTitle = "Playtest " + *slug
	}

	createReq := &pb.CreatePlaytestRequest{
		Namespace:         g.Namespace,
		Slug:              *slug,
		Title:             resolvedTitle,
		Platforms:         platforms,
		DistributionModel: pb.DistributionModel_DISTRIBUTION_MODEL_STEAM_KEYS,
	}
	signupReq := &pb.SignupRequest{Slug: *slug, Platforms: platforms}
	statusReq := &pb.GetApplicantStatusRequest{Slug: *slug}

	if *dryRun {
		// Dry-run cannot resolve the playtestId without dialling; emit a
		// placeholder so the request shape is still useful to readers.
		dryTransitionReq := &pb.TransitionPlaytestStatusRequest{
			Namespace:    g.Namespace,
			PlaytestId:   "<resolved-after-create>",
			TargetStatus: pb.PlaytestStatus_PLAYTEST_STATUS_OPEN,
		}
		dryRunSteps := []struct {
			label string
			msg   proto.Message
		}{
			{"create-playtest", createReq},
			{"transition-open", dryTransitionReq},
			{"signup", signupReq},
			{"assert-pending", statusReq},
		}
		for _, s := range dryRunSteps {
			if !writeFlowDryRun(stdout, stderr, s.label, s.msg) {
				return exitLocalError
			}
		}
		return exitOK
	}

	adminFactory, _ := mk(g, *adminProfile)
	playerFactory, _ := mk(g, *playerProfile)

	// Step 1: create-playtest (admin profile).
	createResp, code := flowInvoke(ctx, stdout, stderr, g, adminFactory, "create-playtest",
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.CreatePlaytest(cctx, createReq)
		})
	if code != exitOK {
		return code
	}
	cp, ok := createResp.(*pb.CreatePlaytestResponse)
	if !ok || cp.Playtest == nil || cp.Playtest.Id == "" {
		writeFlowFailure(stdout, stderr, "create-playtest", "Internal", "CreatePlaytest response missing playtest.id")
		return exitClientError
	}
	playtestID := cp.Playtest.Id

	// Step 2: transition OPEN (admin profile).
	transReq := &pb.TransitionPlaytestStatusRequest{
		Namespace:    g.Namespace,
		PlaytestId:   playtestID,
		TargetStatus: pb.PlaytestStatus_PLAYTEST_STATUS_OPEN,
	}
	if _, code := flowInvoke(ctx, stdout, stderr, g, adminFactory, "transition-open",
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.TransitionPlaytestStatus(cctx, transReq)
		}); code != exitOK {
		return code
	}

	// Step 3: signup (player profile).
	if _, code := flowInvoke(ctx, stdout, stderr, g, playerFactory, "signup",
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.Signup(cctx, signupReq)
		}); code != exitOK {
		return code
	}

	// Step 4: assert APPLICANT_STATUS_PENDING. A successful RPC with the
	// wrong status surfaces as a synthetic FAILED line so the operator
	// sees the mismatch in the same NDJSON stream and the exit code
	// reflects "the flow did not reach its terminal state".
	statusResp, code := flowInvoke(ctx, stdout, stderr, g, playerFactory, "assert-pending",
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.GetApplicantStatus(cctx, statusReq)
		})
	if code != exitOK {
		return code
	}
	sr, ok := statusResp.(*pb.GetApplicantStatusResponse)
	if !ok || sr.Applicant == nil {
		writeFlowFailure(stdout, stderr, "assert-pending", "Internal", "GetApplicantStatus response missing applicant")
		return exitClientError
	}
	if sr.Applicant.Status != pb.ApplicantStatus_APPLICANT_STATUS_PENDING {
		writeFlowFailure(stdout, stderr, "assert-pending", "FailedPrecondition",
			fmt.Sprintf("expected APPLICANT_STATUS_PENDING, got %s", sr.Applicant.Status))
		return exitClientError
	}
	return exitOK
}

// flowInvoke runs one step against the supplied factory, writes the step
// result line to stdout, and maps gRPC status to a cli.md §8 exit code.
// A non-OK exit means the caller stops the flow without further steps.
func flowInvoke(
	ctx context.Context, stdout, stderr io.Writer, g *Globals, factory playtestClientFactory,
	label string,
	invoke func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error),
) (proto.Message, int) {
	client, callCtx, closeFn, err := factory(ctx)
	if err != nil {
		writeFlowFailure(stdout, stderr, label, "Unavailable", fmt.Sprintf("dial: %v", err))
		return nil, exitTransportError
	}
	defer closeFn() //nolint:errcheck // best-effort close on a CLI exit path.
	g.logRPC(stderr, label)
	callCtx, cancel := context.WithTimeout(callCtx, g.Timeout)
	defer cancel()
	resp, err := invoke(client, callCtx)
	if err != nil {
		st, _ := status.FromError(err)
		writeFlowFailure(stdout, stderr, label, st.Code().String(), st.Message())
		return nil, exitCodeForGRPC(st.Code())
	}
	if err := writeFlowSuccess(stdout, label, resp); err != nil {
		fmt.Fprintf(stderr, "flow: %v\n", err)
		return nil, exitLocalError
	}
	return resp, exitOK
}

// flowSuccessLine + flowFailureLine + flowDryRunLine name the wire shape
// each step emits. They stay distinct types (rather than a sum type with
// optional fields) so absent keys are absent in JSON instead of `null`.
type flowSuccessLine struct {
	Step     string          `json:"step"`
	Status   string          `json:"status"`
	Response json.RawMessage `json:"response"`
}

type flowFailureLine struct {
	Step   string `json:"step"`
	Status string `json:"status"`
	Error  struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type flowDryRunLine struct {
	Step    string          `json:"step"`
	Status  string          `json:"status"`
	Request json.RawMessage `json:"request"`
}

func writeFlowSuccess(stdout io.Writer, label string, msg proto.Message) error {
	body, err := protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: false,
	}.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal %s response: %w", label, err)
	}
	return writeJSONLine(stdout, flowSuccessLine{Step: label, Status: statusOK, Response: body})
}

func writeFlowFailure(stdout, stderr io.Writer, label, code, msg string) {
	line := flowFailureLine{Step: label, Status: statusFailed}
	line.Error.Code = code
	line.Error.Message = msg
	if err := writeJSONLine(stdout, line); err != nil {
		fmt.Fprintf(stderr, "flow: %v\n", err)
	}
}

func writeFlowDryRun(stdout, stderr io.Writer, label string, msg proto.Message) bool {
	body, err := protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: false,
	}.Marshal(msg)
	if err != nil {
		fmt.Fprintf(stderr, "flow: marshal %s request: %v\n", label, err)
		return false
	}
	if err := writeJSONLine(stdout, flowDryRunLine{Step: label, Status: "DRY_RUN", Request: body}); err != nil {
		fmt.Fprintf(stderr, "flow: %v\n", err)
		return false
	}
	return true
}

func writeJSONLine(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal flow line: %w", err)
	}
	_, err = fmt.Fprintf(w, "%s\n", b)
	return err
}
