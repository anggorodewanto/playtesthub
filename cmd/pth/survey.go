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

const surveyUsage = `survey: action required (one of: create, edit, get)`

// runSurvey dispatches `pth survey <action> ...`. cli.md §6.3 (M3).
//
// `create` and `edit` accept --from <path|-> as a JSON file containing
// a list of SurveyQuestion entries. (cli.md §6.3 final form is YAML;
// JSON ships first because it's protojson-trivial and unblocks the
// smoke harness. The YAML wrapper lands with phase 12.)
func runSurvey(ctx context.Context, stdout, stderr io.Writer, g *Globals, args []string, factory playtestClientFactory) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, surveyUsage)
		return exitLocalError
	}
	action, rest := args[0], args[1:]
	switch action {
	case "create":
		return runSurveyCreate(ctx, stdout, stderr, g, rest, factory)
	case "edit":
		return runSurveyEdit(ctx, stdout, stderr, g, rest, factory)
	case "get":
		return runSurveyGet(ctx, stdout, stderr, g, rest, factory)
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
	if g.Namespace == "" {
		fmt.Fprintln(stderr, "survey create: --namespace (or PTH_NAMESPACE) is required")
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
	if g.Namespace == "" {
		fmt.Fprintln(stderr, "survey edit: --namespace (or PTH_NAMESPACE) is required")
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

