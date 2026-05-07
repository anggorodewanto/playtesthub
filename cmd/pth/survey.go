package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const surveyUsage = `survey: action required (one of: create, edit, get, submit, responses)`

// runSurvey dispatches `pth survey <action> ...`. cli.md §6.3 (M3).
//
// `create` and `edit` accept --from <path|-> as a JSON file containing
// a list of SurveyQuestion entries. `submit` takes a JSON array of
// SurveyAnswer entries instead. (cli.md §6.3 final form is YAML; JSON
// ships first because it's protojson-trivial and unblocks the smoke
// harness. The YAML wrapper lands with phase 12.)
func runSurvey(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, surveyUsage)
		return exitLocalError
	}
	action, rest := args[0], args[1:]
	switch action {
	case actionCreate:
		return runSurveyCreate(ctx, stdout, stderr, g, rest, factory)
	case actionEdit:
		return runSurveyEdit(ctx, stdout, stderr, g, rest, factory)
	case actionGet:
		return runSurveyGet(ctx, stdout, stderr, g, rest, factory)
	case "submit":
		return runSurveySubmit(ctx, stdout, stderr, g, rest, factory)
	case "responses":
		return runSurveyResponses(ctx, stdout, stderr, g, rest, factory)
	default:
		fmt.Fprintf(stderr, "survey: unknown action %q\n", action)
		return exitLocalError
	}
}

func runSurveyCreate(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("survey create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	playtestID := fs.String("playtest", "", "playtest id (required)")
	from := fs.String("from", "", "path to JSON file containing the questions array ('-' reads stdin)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *playtestID == "" {
		fmt.Fprintln(stderr, "survey create: --playtest is required")
		return exitLocalError
	}
	if !g.requireNamespace(stderr, "survey create") {
		return exitLocalError
	}
	questions, code := loadSurveyQuestions(stderr, "survey create", *from)
	if code != exitOK {
		return code
	}
	req := &pb.CreateSurveyRequest{
		Namespace:  g.Namespace,
		PlaytestId: *playtestID,
		Questions:  questions,
	}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "CreateSurvey", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.CreateSurvey(cctx, req)
		})
}

func runSurveyEdit(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("survey edit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	playtestID := fs.String("playtest", "", "playtest id (required)")
	from := fs.String("from", "", "path to JSON file containing the questions array ('-' reads stdin)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *playtestID == "" {
		fmt.Fprintln(stderr, "survey edit: --playtest is required")
		return exitLocalError
	}
	if !g.requireNamespace(stderr, "survey edit") {
		return exitLocalError
	}
	questions, code := loadSurveyQuestions(stderr, "survey edit", *from)
	if code != exitOK {
		return code
	}
	req := &pb.EditSurveyRequest{
		Namespace:  g.Namespace,
		PlaytestId: *playtestID,
		Questions:  questions,
	}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "EditSurvey", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.EditSurvey(cctx, req)
		})
}

func runSurveySubmit(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("survey submit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	playtestID := fs.String("playtest", "", "playtest id (required)")
	surveyID := fs.String("survey", "", "survey id the answers target (required)")
	from := fs.String("from", "", "path to JSON file containing the answers array ('-' reads stdin)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *playtestID == "" {
		fmt.Fprintln(stderr, "survey submit: --playtest is required")
		return exitLocalError
	}
	if *surveyID == "" {
		fmt.Fprintln(stderr, "survey submit: --survey is required")
		return exitLocalError
	}
	answers, code := loadSurveyAnswers(stderr, "survey submit", *from)
	if code != exitOK {
		return code
	}
	req := &pb.SubmitSurveyResponseRequest{
		PlaytestId: *playtestID,
		SurveyId:   *surveyID,
		Answers:    answers,
	}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "SubmitSurveyResponse", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.SubmitSurveyResponse(cctx, req)
		})
}

// loadSurveyAnswers reads a JSON array of SurveyAnswer entries from a
// file path (or stdin via "-"). Empty path is allowed for --dry-run
// callers and yields a nil slice. Mirrors loadSurveyQuestions; the
// server is the authority on bounds + per-question type checking.
func loadSurveyAnswers(stderr io.Writer, label, path string) ([]*pb.SurveyAnswer, int) {
	if path == "" {
		return nil, exitOK
	}
	body, err := readFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", label, err)
		return nil, exitLocalError
	}
	if len(body) == 0 {
		return nil, exitOK
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		fmt.Fprintf(stderr, "%s: --from must be a JSON array of SurveyAnswer objects: %v\n", label, err)
		return nil, exitLocalError
	}
	out := make([]*pb.SurveyAnswer, 0, len(raw))
	unmarshal := protojson.UnmarshalOptions{DiscardUnknown: true}
	for i, item := range raw {
		a := &pb.SurveyAnswer{}
		if err := unmarshal.Unmarshal(item, a); err != nil {
			fmt.Fprintf(stderr, "%s: answers[%d] is not a SurveyAnswer: %v\n", label, i, err)
			return nil, exitLocalError
		}
		out = append(out, a)
	}
	return out, exitOK
}

// runSurveyResponses lists submitted survey responses for a playtest.
// Admin token required; cursor pagination on (submittedAt, id) DESC,
// optional --survey filter narrows to a single Survey version.
// cli.md §6.3.
func runSurveyResponses(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("survey responses", flag.ContinueOnError)
	fs.SetOutput(stderr)
	playtestID := fs.String("playtest", "", "playtest id (required)")
	surveyID := fs.String("survey", "", "narrow to a specific Survey version (optional)")
	cursor := fs.String("cursor", "", "opaque page_token from a prior response")
	pageSize := fs.Int("page-size", 0, "page size (0 → server default 50)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *playtestID == "" {
		fmt.Fprintln(stderr, "survey responses: --playtest is required")
		return exitLocalError
	}
	if !g.requireNamespace(stderr, "survey responses") {
		return exitLocalError
	}
	req := &pb.ListSurveyResponsesRequest{
		Namespace:      g.Namespace,
		PlaytestId:     *playtestID,
		SurveyIdFilter: *surveyID,
		PageToken:      *cursor,
		PageSize:       int32(*pageSize),
	}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "ListSurveyResponses", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.ListSurveyResponses(cctx, req)
		})
}

func runSurveyGet(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	fs := flag.NewFlagSet("survey get", flag.ContinueOnError)
	fs.SetOutput(stderr)
	playtestID := fs.String("playtest", "", "playtest id (required)")
	dryRun := fs.Bool("dry-run", false, "print the request JSON and exit without dialling")
	if err := fs.Parse(args); err != nil {
		return exitLocalError
	}
	if *playtestID == "" {
		fmt.Fprintln(stderr, "survey get: --playtest is required")
		return exitLocalError
	}
	req := &pb.GetSurveyRequest{PlaytestId: *playtestID}
	return invokePlaytest(ctx, stdout, stderr, g, factory, "GetSurvey", req, *dryRun,
		func(c pb.PlaytesthubServiceClient, cctx context.Context) (proto.Message, error) {
			return c.GetSurvey(cctx, req)
		})
}

// loadSurveyQuestions reads a JSON array of SurveyQuestion entries
// from a file path (or stdin via "-"). Empty path is allowed — used
// by --dry-run callers that don't care about the body — and yields a
// nil slice. The JSON shape matches the protojson encoding of the
// repeated field; the server is the authority on bounds + id minting.
func loadSurveyQuestions(stderr io.Writer, label, path string) ([]*pb.SurveyQuestion, int) {
	if path == "" {
		return nil, exitOK
	}
	body, err := readFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", label, err)
		return nil, exitLocalError
	}
	if len(body) == 0 {
		return nil, exitOK
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		fmt.Fprintf(stderr, "%s: --from must be a JSON array of SurveyQuestion objects: %v\n", label, err)
		return nil, exitLocalError
	}
	out := make([]*pb.SurveyQuestion, 0, len(raw))
	unmarshal := protojson.UnmarshalOptions{DiscardUnknown: true}
	for i, item := range raw {
		q := &pb.SurveyQuestion{}
		if err := unmarshal.Unmarshal(item, q); err != nil {
			fmt.Fprintf(stderr, "%s: questions[%d] is not a SurveyQuestion: %v\n", label, i, err)
			return nil, exitLocalError
		}
		out = append(out, q)
	}
	return out, exitOK
}
