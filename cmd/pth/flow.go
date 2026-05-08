package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

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

const flowUsage = `flow: action required (one of: golden-m1, golden-m2, golden-m3)`

func runFlow(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, mk flowProfileFactory) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, flowUsage)
		return exitLocalError
	}
	action, rest := args[0], args[1:]
	switch action {
	case "golden-m1":
		return runFlowGoldenM1(ctx, stdout, stderr, g, rest, mk)
	case "golden-m2":
		return runFlowGoldenM2(ctx, stdout, stderr, g, rest, mk)
	case "golden-m3":
		return runFlowGoldenM3(ctx, stdout, stderr, g, rest, mk)
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
	if !g.requireNamespace(stderr, "flow golden-m1") {
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

// goldenM2Inputs bundles the parsed flag values for golden-m2. Pulled
// out as a struct so the parse / validation step has a single return
// shape and the orchestrator stays linear.
type goldenM2Inputs struct {
	slug          string
	title         string
	platforms     []pb.Platform
	ndaProse      string
	csvBody       string
	csvFilename   string
	adminProfile  string
	playerProfile string
	dryRun        bool
}

// parseGoldenM2Flags returns parsed inputs or, on validation failure,
// an exit code (caller returns it). All user-visible error messages are
// written to stderr inside this function.
func parseGoldenM2Flags(stderr io.Writer, g *Globals, args []string) (goldenM2Inputs, int) {
	fs := flag.NewFlagSet("flow golden-m2", flag.ContinueOnError)
	fs.SetOutput(stderr)
	slug := fs.String("slug", "", "playtest slug (required, PRD §5.1 regex)")
	title := fs.String("title", "", "playtest title (default: 'Playtest <slug>')")
	platformsCSV := fs.String("platforms", "STEAM", "platforms for both create and signup")
	ndaText := fs.String("nda-text", "Standard playtest NDA — golden-m2.", "NDA prose; @file to load from disk")
	codesFile := fs.String("codes-file", "", "CSV path to upload (overrides --codes-count)")
	codesCount := fs.Int("codes-count", 1, "number of synthetic codes to upload when --codes-file is empty (1..50)")
	adminProfile := fs.String("admin-profile", "", "credential profile for admin steps")
	playerProfile := fs.String("player-profile", "", "credential profile for player steps")
	dryRun := fs.Bool("dry-run", false, "print every step's request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return goldenM2Inputs{}, exitLocalError
	}
	if *slug == "" {
		fmt.Fprintln(stderr, "flow golden-m2: --slug is required")
		return goldenM2Inputs{}, exitLocalError
	}
	if g.Namespace == "" {
		fmt.Fprintln(stderr, "flow golden-m2: --namespace (or PTH_NAMESPACE) is required")
		return goldenM2Inputs{}, exitLocalError
	}
	if !*dryRun && *adminProfile == "" {
		fmt.Fprintln(stderr, "flow golden-m2: --admin-profile is required")
		return goldenM2Inputs{}, exitLocalError
	}
	if !*dryRun && *playerProfile == "" {
		fmt.Fprintln(stderr, "flow golden-m2: --player-profile is required")
		return goldenM2Inputs{}, exitLocalError
	}
	platforms, err := parsePlatforms(*platformsCSV)
	if err != nil {
		fmt.Fprintf(stderr, "flow golden-m2: %v\n", err)
		return goldenM2Inputs{}, exitLocalError
	}
	if len(platforms) == 0 {
		platforms = []pb.Platform{pb.Platform_PLATFORM_STEAM}
	}
	ndaProse, err := readMaybeFile(*ndaText)
	if err != nil {
		fmt.Fprintf(stderr, "flow golden-m2: --nda-text %v\n", err)
		return goldenM2Inputs{}, exitLocalError
	}
	if ndaProse == "" {
		fmt.Fprintln(stderr, "flow golden-m2: --nda-text must be non-empty (PRD §5.1: NDA-required playtests need prose)")
		return goldenM2Inputs{}, exitLocalError
	}
	csvBody, csvFilename, err := resolveGoldenM2CSV(*codesFile, *codesCount, *slug)
	if err != nil {
		fmt.Fprintf(stderr, "flow golden-m2: %v\n", err)
		return goldenM2Inputs{}, exitLocalError
	}
	resolvedTitle := *title
	if resolvedTitle == "" {
		resolvedTitle = "Playtest " + *slug
	}
	return goldenM2Inputs{
		slug:          *slug,
		title:         resolvedTitle,
		platforms:     platforms,
		ndaProse:      ndaProse,
		csvBody:       csvBody,
		csvFilename:   csvFilename,
		adminProfile:  *adminProfile,
		playerProfile: *playerProfile,
		dryRun:        *dryRun,
	}, exitOK
}

// runFlowGoldenM2 composes the PRD §4.1 M2 golden flow on top of M1's
// four steps. Seven NDJSON lines: create-playtest (NDA required) →
// transition-open → signup → accept-nda → upload-codes → approve →
// get-code. The flow stops on the first failure with the cli.md §8
// exit code matching the failed step.
func runFlowGoldenM2(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, mk flowProfileFactory) int {
	in, code := parseGoldenM2Flags(stderr, g, args)
	if code != exitOK {
		return code
	}
	createReq := &pb.CreatePlaytestRequest{
		Namespace:         g.Namespace,
		Slug:              in.slug,
		Title:             in.title,
		Platforms:         in.platforms,
		DistributionModel: pb.DistributionModel_DISTRIBUTION_MODEL_STEAM_KEYS,
		NdaRequired:       true,
		NdaText:           in.ndaProse,
	}
	if in.dryRun {
		return runFlowGoldenM2DryRun(stdout, stderr, g, &in, createReq)
	}
	return runFlowGoldenM2Live(ctx, stdout, stderr, g, &in, createReq, mk)
}

// runFlowGoldenM2DryRun emits one NDJSON line per request shape (with
// placeholder ids for fields that only resolve after a real dial).
func runFlowGoldenM2DryRun(stdout, stderr io.Writer, g *Globals, in *goldenM2Inputs, createReq *pb.CreatePlaytestRequest) int {
	const placeholder = "<resolved-after-create>"
	dryRunSteps := []struct {
		label string
		msg   proto.Message
	}{
		{"create-playtest", createReq},
		{"transition-open", &pb.TransitionPlaytestStatusRequest{
			Namespace:    g.Namespace,
			PlaytestId:   placeholder,
			TargetStatus: pb.PlaytestStatus_PLAYTEST_STATUS_OPEN,
		}},
		{"signup", &pb.SignupRequest{Slug: in.slug, Platforms: in.platforms}},
		{"accept-nda", &pb.AcceptNDARequest{PlaytestId: placeholder}},
		{"upload-codes", &pb.UploadCodesRequest{
			Namespace:  g.Namespace,
			PlaytestId: placeholder,
			CsvContent: in.csvBody,
			Filename:   in.csvFilename,
		}},
		{"approve", &pb.ApproveApplicantRequest{
			Namespace:   g.Namespace,
			ApplicantId: "<resolved-after-signup>",
		}},
		{"get-code", &pb.GetGrantedCodeRequest{PlaytestId: placeholder}},
	}
	for _, s := range dryRunSteps {
		if !writeFlowDryRun(stdout, stderr, s.label, s.msg) {
			return exitLocalError
		}
	}
	return exitOK
}

// runFlowGoldenM2Live drives the seven RPCs in sequence, halting on the
// first failure. The two id resolutions (playtest_id from create-playtest,
// applicant_id from signup) are the only state threaded between steps.
func runFlowGoldenM2Live(ctx context.Context, stdout, stderr io.Writer, g *Globals, in *goldenM2Inputs, createReq *pb.CreatePlaytestRequest, mk flowProfileFactory) int {
	adminFactory, _ := mk(g, in.adminProfile)
	playerFactory, _ := mk(g, in.playerProfile)

	playtestID, code := flowGoldenM2CreateAndOpen(ctx, stdout, stderr, g, adminFactory, createReq)
	if code != exitOK {
		return code
	}
	applicantID, code := flowGoldenM2SignupAndAccept(ctx, stdout, stderr, g, playerFactory, in, playtestID)
	if code != exitOK {
		return code
	}
	if code := flowGoldenM2UploadAndApprove(ctx, stdout, stderr, g, adminFactory, in, playtestID, applicantID); code != exitOK {
		return code
	}
	return flowGoldenM2GetCode(ctx, stdout, stderr, g, playerFactory, playtestID)
}

func flowGoldenM2CreateAndOpen(ctx context.Context, stdout, stderr io.Writer, g *Globals, admin playtestClientFactory, createReq *pb.CreatePlaytestRequest) (string, int) {
	createResp, code := flowInvoke(ctx, stdout, stderr, g, admin, "create-playtest",
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.CreatePlaytest(cctx, createReq)
		})
	if code != exitOK {
		return "", code
	}
	cp, ok := createResp.(*pb.CreatePlaytestResponse)
	if !ok || cp.Playtest == nil || cp.Playtest.Id == "" {
		writeFlowFailure(stdout, stderr, "create-playtest", "Internal", "CreatePlaytest response missing playtest.id")
		return "", exitClientError
	}
	playtestID := cp.Playtest.Id
	transReq := &pb.TransitionPlaytestStatusRequest{
		Namespace:    g.Namespace,
		PlaytestId:   playtestID,
		TargetStatus: pb.PlaytestStatus_PLAYTEST_STATUS_OPEN,
	}
	if _, code := flowInvoke(ctx, stdout, stderr, g, admin, "transition-open",
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.TransitionPlaytestStatus(cctx, transReq)
		}); code != exitOK {
		return "", code
	}
	return playtestID, exitOK
}

func flowGoldenM2SignupAndAccept(ctx context.Context, stdout, stderr io.Writer, g *Globals, player playtestClientFactory, in *goldenM2Inputs, playtestID string) (string, int) {
	signupResp, code := flowInvoke(ctx, stdout, stderr, g, player, "signup",
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.Signup(cctx, &pb.SignupRequest{Slug: in.slug, Platforms: in.platforms})
		})
	if code != exitOK {
		return "", code
	}
	sr, ok := signupResp.(*pb.SignupResponse)
	if !ok || sr.Applicant == nil || sr.Applicant.Id == "" {
		writeFlowFailure(stdout, stderr, "signup", "Internal", "Signup response missing applicant.id")
		return "", exitClientError
	}
	if _, code := flowInvoke(ctx, stdout, stderr, g, player, "accept-nda",
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.AcceptNDA(cctx, &pb.AcceptNDARequest{PlaytestId: playtestID})
		}); code != exitOK {
		return "", code
	}
	return sr.Applicant.Id, exitOK
}

func flowGoldenM2UploadAndApprove(ctx context.Context, stdout, stderr io.Writer, g *Globals, admin playtestClientFactory, in *goldenM2Inputs, playtestID, applicantID string) int {
	uploadReq := &pb.UploadCodesRequest{
		Namespace:  g.Namespace,
		PlaytestId: playtestID,
		CsvContent: in.csvBody,
		Filename:   in.csvFilename,
	}
	if _, code := flowInvoke(ctx, stdout, stderr, g, admin, "upload-codes",
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.UploadCodes(cctx, uploadReq)
		}); code != exitOK {
		return code
	}
	if _, code := flowInvoke(ctx, stdout, stderr, g, admin, "approve",
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.ApproveApplicant(cctx, &pb.ApproveApplicantRequest{Namespace: g.Namespace, ApplicantId: applicantID})
		}); code != exitOK {
		return code
	}
	return exitOK
}

func flowGoldenM2GetCode(ctx context.Context, stdout, stderr io.Writer, g *Globals, player playtestClientFactory, playtestID string) int {
	getCodeResp, code := flowInvoke(ctx, stdout, stderr, g, player, "get-code",
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.GetGrantedCode(cctx, &pb.GetGrantedCodeRequest{PlaytestId: playtestID})
		})
	if code != exitOK {
		return code
	}
	gc, ok := getCodeResp.(*pb.GetGrantedCodeResponse)
	if !ok || gc.Value == "" {
		writeFlowFailure(stdout, stderr, "get-code", "FailedPrecondition",
			"GetGrantedCode response missing or empty code value")
		return exitClientError
	}
	return exitOK
}

// resolveGoldenM2CSV resolves the CSV body for the upload-codes step.
// --codes-file wins; otherwise we synthesise `count` short codes using
// slug+ordinal so concurrent runs against the same backend don't collide.
// The synthesis path keeps the harness self-contained — callers can run
// `pth flow golden-m2 --slug e2e-1 --admin-profile admin --player-profile p1`
// without staging a CSV on disk.
func resolveGoldenM2CSV(filePath string, count int, slug string) (string, string, error) {
	if filePath != "" {
		body, err := readFile(filePath)
		if err != nil {
			return "", "", err
		}
		filename := ""
		if filePath != "-" {
			filename = filePath
		}
		return string(body), filename, nil
	}
	if count <= 0 || count > 50 {
		return "", "", fmt.Errorf("--codes-count must be between 1 and 50 (got %d)", count)
	}
	var b strings.Builder
	for i := range count {
		fmt.Fprintf(&b, "GOLDEN-M2-%s-%04d\n", strings.ToUpper(slug), i)
	}
	return b.String(), fmt.Sprintf("golden-m2-%s.csv", slug), nil
}

// goldenM3SurveyQuestions are the two questions golden-m3 seeds in
// create-survey. One required TEXT + one required RATING. Inline so
// the harness has no external file to manage; bounds match the
// schema.md "Survey entity spec" gates (prompt ≤ 1,000 chars).
func goldenM3SurveyQuestions() []*pb.SurveyQuestion {
	return []*pb.SurveyQuestion{
		{Type: pb.SurveyQuestionType_SURVEY_QUESTION_TYPE_TEXT, Prompt: "How was the matchmaking?", Required: true},
		{Type: pb.SurveyQuestionType_SURVEY_QUESTION_TYPE_RATING, Prompt: "Rate the build (1-5)", Required: true},
	}
}

// runFlowGoldenM3 extends golden-m2 with the three survey steps.
// Ten NDJSON lines in total: M2's seven (create-playtest →
// transition-open → signup → accept-nda → upload-codes → approve →
// get-code) plus create-survey → submit-response → list-responses.
// Stops on the first failure with the cli.md §8 exit code.
func runFlowGoldenM3(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, mk flowProfileFactory) int {
	in, code := parseGoldenM2Flags(stderr, g, args)
	if code != exitOK {
		return code
	}
	createReq := &pb.CreatePlaytestRequest{
		Namespace:         g.Namespace,
		Slug:              in.slug,
		Title:             in.title,
		Platforms:         in.platforms,
		DistributionModel: pb.DistributionModel_DISTRIBUTION_MODEL_STEAM_KEYS,
		NdaRequired:       true,
		NdaText:           in.ndaProse,
	}
	if in.dryRun {
		return runFlowGoldenM3DryRun(stdout, stderr, g, &in, createReq)
	}
	return runFlowGoldenM3Live(ctx, stdout, stderr, g, &in, createReq, mk)
}

// runFlowGoldenM3DryRun emits the ten-step request shape catalogue.
// Survey ids are placeholders since they only resolve after a real
// CreateSurvey round-trip.
func runFlowGoldenM3DryRun(stdout, stderr io.Writer, g *Globals, in *goldenM2Inputs, createReq *pb.CreatePlaytestRequest) int {
	const placeholder = "<resolved-after-create>"
	const surveyPlaceholder = "<resolved-after-create-survey>"
	dryRunSteps := []struct {
		label string
		msg   proto.Message
	}{
		{"create-playtest", createReq},
		{"transition-open", &pb.TransitionPlaytestStatusRequest{
			Namespace:    g.Namespace,
			PlaytestId:   placeholder,
			TargetStatus: pb.PlaytestStatus_PLAYTEST_STATUS_OPEN,
		}},
		{"signup", &pb.SignupRequest{Slug: in.slug, Platforms: in.platforms}},
		{"accept-nda", &pb.AcceptNDARequest{PlaytestId: placeholder}},
		{"upload-codes", &pb.UploadCodesRequest{
			Namespace:  g.Namespace,
			PlaytestId: placeholder,
			CsvContent: in.csvBody,
			Filename:   in.csvFilename,
		}},
		{"approve", &pb.ApproveApplicantRequest{
			Namespace:   g.Namespace,
			ApplicantId: "<resolved-after-signup>",
		}},
		{"get-code", &pb.GetGrantedCodeRequest{PlaytestId: placeholder}},
		{"create-survey", &pb.CreateSurveyRequest{
			Namespace:  g.Namespace,
			PlaytestId: placeholder,
			Questions:  goldenM3SurveyQuestions(),
		}},
		{"submit-response", &pb.SubmitSurveyResponseRequest{
			PlaytestId: placeholder,
			SurveyId:   surveyPlaceholder,
			Answers: []*pb.SurveyAnswer{
				{QuestionId: "<resolved-from-create-survey>", Value: &pb.SurveyAnswer_Text{Text: "Smooth, no hiccups."}},
				{QuestionId: "<resolved-from-create-survey>", Value: &pb.SurveyAnswer_Rating{Rating: 5}},
			},
		}},
		{"list-responses", &pb.ListSurveyResponsesRequest{
			Namespace:  g.Namespace,
			PlaytestId: placeholder,
		}},
	}
	for _, s := range dryRunSteps {
		if !writeFlowDryRun(stdout, stderr, s.label, s.msg) {
			return exitLocalError
		}
	}
	return exitOK
}

// runFlowGoldenM3Live drives the ten RPCs in sequence, halting on the
// first failure. Reuses every M2 step then layers the three survey
// steps on top — the only state threaded between is the playtest_id
// (from create-playtest), the applicant_id (from signup), and the
// survey + question ids (from create-survey).
func runFlowGoldenM3Live(ctx context.Context, stdout, stderr io.Writer, g *Globals, in *goldenM2Inputs, createReq *pb.CreatePlaytestRequest, mk flowProfileFactory) int {
	adminFactory, _ := mk(g, in.adminProfile)
	playerFactory, _ := mk(g, in.playerProfile)

	playtestID, code := flowGoldenM2CreateAndOpen(ctx, stdout, stderr, g, adminFactory, createReq)
	if code != exitOK {
		return code
	}
	applicantID, code := flowGoldenM2SignupAndAccept(ctx, stdout, stderr, g, playerFactory, in, playtestID)
	if code != exitOK {
		return code
	}
	if code := flowGoldenM2UploadAndApprove(ctx, stdout, stderr, g, adminFactory, in, playtestID, applicantID); code != exitOK {
		return code
	}
	if code := flowGoldenM2GetCode(ctx, stdout, stderr, g, playerFactory, playtestID); code != exitOK {
		return code
	}

	survey, code := flowGoldenM3CreateSurvey(ctx, stdout, stderr, g, adminFactory, playtestID)
	if code != exitOK {
		return code
	}
	if code := flowGoldenM3SubmitResponse(ctx, stdout, stderr, g, playerFactory, playtestID, survey); code != exitOK {
		return code
	}
	return flowGoldenM3ListResponses(ctx, stdout, stderr, g, adminFactory, playtestID)
}

func flowGoldenM3CreateSurvey(ctx context.Context, stdout, stderr io.Writer, g *Globals, admin playtestClientFactory, playtestID string) (*pb.Survey, int) {
	resp, code := flowInvoke(ctx, stdout, stderr, g, admin, "create-survey",
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.CreateSurvey(cctx, &pb.CreateSurveyRequest{
				Namespace:  g.Namespace,
				PlaytestId: playtestID,
				Questions:  goldenM3SurveyQuestions(),
			})
		})
	if code != exitOK {
		return nil, code
	}
	cs, ok := resp.(*pb.CreateSurveyResponse)
	if !ok || cs.Survey == nil || cs.Survey.Id == "" || len(cs.Survey.Questions) != 2 {
		writeFlowFailure(stdout, stderr, "create-survey", "Internal", "CreateSurvey response missing survey.id or expected 2 questions")
		return nil, exitClientError
	}
	return cs.Survey, exitOK
}

func flowGoldenM3SubmitResponse(ctx context.Context, stdout, stderr io.Writer, g *Globals, player playtestClientFactory, playtestID string, survey *pb.Survey) int {
	answers := make([]*pb.SurveyAnswer, 0, len(survey.Questions))
	for _, q := range survey.Questions {
		switch q.GetType() {
		case pb.SurveyQuestionType_SURVEY_QUESTION_TYPE_TEXT:
			answers = append(answers, &pb.SurveyAnswer{
				QuestionId: q.GetId(),
				Value:      &pb.SurveyAnswer_Text{Text: "Smooth, no hiccups."},
			})
		case pb.SurveyQuestionType_SURVEY_QUESTION_TYPE_RATING:
			answers = append(answers, &pb.SurveyAnswer{
				QuestionId: q.GetId(),
				Value:      &pb.SurveyAnswer_Rating{Rating: 5},
			})
		default:
			writeFlowFailure(stdout, stderr, "submit-response", "Internal",
				fmt.Sprintf("unexpected question type %s for golden-m3 (expected TEXT or RATING)", q.GetType()))
			return exitClientError
		}
	}
	if _, code := flowInvoke(ctx, stdout, stderr, g, player, "submit-response",
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.SubmitSurveyResponse(cctx, &pb.SubmitSurveyResponseRequest{
				PlaytestId: playtestID,
				SurveyId:   survey.GetId(),
				Answers:    answers,
			})
		}); code != exitOK {
		return code
	}
	return exitOK
}

func flowGoldenM3ListResponses(ctx context.Context, stdout, stderr io.Writer, g *Globals, admin playtestClientFactory, playtestID string) int {
	resp, code := flowInvoke(ctx, stdout, stderr, g, admin, "list-responses",
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.ListSurveyResponses(cctx, &pb.ListSurveyResponsesRequest{
				Namespace:  g.Namespace,
				PlaytestId: playtestID,
			})
		})
	if code != exitOK {
		return code
	}
	lr, ok := resp.(*pb.ListSurveyResponsesResponse)
	if !ok || len(lr.Responses) == 0 {
		writeFlowFailure(stdout, stderr, "list-responses", "FailedPrecondition",
			"ListSurveyResponses returned an empty responses array (expected the just-submitted row)")
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
	if _, err := fmt.Fprintf(w, "%s\n", b); err != nil {
		return fmt.Errorf("writing flow line: %w", err)
	}
	return nil
}
