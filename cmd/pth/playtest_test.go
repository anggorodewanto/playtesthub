package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// testNamespaceDev is the namespace string reused by every admin-RPC
// test below. Pulled out as a constant so goconst stays quiet — the
// goal isn't a "real" namespace, just a non-empty placeholder so the
// CLI's required-field check passes.
const testNamespaceDev = "dev"

// stubPlaytestClient is the table-mock used by every playtest + applicant
// subcommand test. Each RPC field is optional; an unset field for a
// method the test calls panics, which is the failure mode we want.
type stubPlaytestClient struct {
	pb.PlaytesthubServiceClient

	getPublicFunc       func(ctx context.Context, in *pb.GetPublicPlaytestRequest, opts ...grpc.CallOption) (*pb.GetPublicPlaytestResponse, error)
	getPlayerFunc       func(ctx context.Context, in *pb.GetPlaytestForPlayerRequest, opts ...grpc.CallOption) (*pb.GetPlaytestForPlayerResponse, error)
	adminGetFunc        func(ctx context.Context, in *pb.AdminGetPlaytestRequest, opts ...grpc.CallOption) (*pb.AdminGetPlaytestResponse, error)
	listFunc            func(ctx context.Context, in *pb.ListPlaytestsRequest, opts ...grpc.CallOption) (*pb.ListPlaytestsResponse, error)
	createFunc          func(ctx context.Context, in *pb.CreatePlaytestRequest, opts ...grpc.CallOption) (*pb.CreatePlaytestResponse, error)
	editFunc            func(ctx context.Context, in *pb.EditPlaytestRequest, opts ...grpc.CallOption) (*pb.EditPlaytestResponse, error)
	deleteFunc          func(ctx context.Context, in *pb.SoftDeletePlaytestRequest, opts ...grpc.CallOption) (*pb.SoftDeletePlaytestResponse, error)
	transitionFunc      func(ctx context.Context, in *pb.TransitionPlaytestStatusRequest, opts ...grpc.CallOption) (*pb.TransitionPlaytestStatusResponse, error)
	signupFunc          func(ctx context.Context, in *pb.SignupRequest, opts ...grpc.CallOption) (*pb.SignupResponse, error)
	applicantStatusFunc func(ctx context.Context, in *pb.GetApplicantStatusRequest, opts ...grpc.CallOption) (*pb.GetApplicantStatusResponse, error)

	calls int
}

func (s *stubPlaytestClient) GetPublicPlaytest(ctx context.Context, in *pb.GetPublicPlaytestRequest, opts ...grpc.CallOption) (*pb.GetPublicPlaytestResponse, error) {
	s.calls++
	return s.getPublicFunc(ctx, in, opts...)
}

func (s *stubPlaytestClient) GetPlaytestForPlayer(ctx context.Context, in *pb.GetPlaytestForPlayerRequest, opts ...grpc.CallOption) (*pb.GetPlaytestForPlayerResponse, error) {
	s.calls++
	return s.getPlayerFunc(ctx, in, opts...)
}

func (s *stubPlaytestClient) AdminGetPlaytest(ctx context.Context, in *pb.AdminGetPlaytestRequest, opts ...grpc.CallOption) (*pb.AdminGetPlaytestResponse, error) {
	s.calls++
	return s.adminGetFunc(ctx, in, opts...)
}

func (s *stubPlaytestClient) ListPlaytests(ctx context.Context, in *pb.ListPlaytestsRequest, opts ...grpc.CallOption) (*pb.ListPlaytestsResponse, error) {
	s.calls++
	return s.listFunc(ctx, in, opts...)
}

func (s *stubPlaytestClient) CreatePlaytest(ctx context.Context, in *pb.CreatePlaytestRequest, opts ...grpc.CallOption) (*pb.CreatePlaytestResponse, error) {
	s.calls++
	return s.createFunc(ctx, in, opts...)
}

func (s *stubPlaytestClient) EditPlaytest(ctx context.Context, in *pb.EditPlaytestRequest, opts ...grpc.CallOption) (*pb.EditPlaytestResponse, error) {
	s.calls++
	return s.editFunc(ctx, in, opts...)
}

func (s *stubPlaytestClient) SoftDeletePlaytest(ctx context.Context, in *pb.SoftDeletePlaytestRequest, opts ...grpc.CallOption) (*pb.SoftDeletePlaytestResponse, error) {
	s.calls++
	return s.deleteFunc(ctx, in, opts...)
}

func (s *stubPlaytestClient) TransitionPlaytestStatus(ctx context.Context, in *pb.TransitionPlaytestStatusRequest, opts ...grpc.CallOption) (*pb.TransitionPlaytestStatusResponse, error) {
	s.calls++
	return s.transitionFunc(ctx, in, opts...)
}

func (s *stubPlaytestClient) Signup(ctx context.Context, in *pb.SignupRequest, opts ...grpc.CallOption) (*pb.SignupResponse, error) {
	s.calls++
	return s.signupFunc(ctx, in, opts...)
}

func (s *stubPlaytestClient) GetApplicantStatus(ctx context.Context, in *pb.GetApplicantStatusRequest, opts ...grpc.CallOption) (*pb.GetApplicantStatusResponse, error) {
	s.calls++
	return s.applicantStatusFunc(ctx, in, opts...)
}

func factoryFor(client pb.PlaytesthubServiceClient) playtestClientFactory {
	return func(ctx context.Context) (pb.PlaytesthubServiceClient, context.Context, func() error, error) {
		return client, ctx, func() error { return nil }, nil
	}
}

func TestRunPlaytestGetPublic_Success(t *testing.T) {
	stub := &stubPlaytestClient{
		getPublicFunc: func(_ context.Context, in *pb.GetPublicPlaytestRequest, _ ...grpc.CallOption) (*pb.GetPublicPlaytestResponse, error) {
			if in.Slug != testSlugDemo01 {
				t.Errorf("slug=%q, want demo-01", in.Slug)
			}
			return &pb.GetPublicPlaytestResponse{Playtest: &pb.PublicPlaytest{Slug: in.Slug, Title: "Demo"}}, nil
		},
	}
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565"}
	code := runPlaytest(t.Context(), &stdout, &stderr, g, []string{"get-public", "--slug", testSlugDemo01}, factoryFor(stub))
	if code != exitOK {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitOK, stderr.String())
	}
	if stub.calls != 1 {
		t.Fatalf("expected 1 RPC call, got %d", stub.calls)
	}
	var got map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &got); err != nil {
		t.Fatalf("stdout not JSON: %v: %q", err, stdout.String())
	}
	pt, ok := got["playtest"].(map[string]any)
	if !ok {
		t.Fatalf("expected playtest key, got %v", got)
	}
	if pt["slug"] != testSlugDemo01 {
		t.Errorf("slug round-trip wrong: %v", pt)
	}
	if !g.Anon {
		t.Error("--anon must be implied for get-public")
	}
}

func TestRunPlaytestGetPublic_NotFoundExit1(t *testing.T) {
	stub := &stubPlaytestClient{
		getPublicFunc: func(_ context.Context, _ *pb.GetPublicPlaytestRequest, _ ...grpc.CallOption) (*pb.GetPublicPlaytestResponse, error) {
			return nil, status.Error(codes.NotFound, "playtest not found")
		},
	}
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565"}
	code := runPlaytest(t.Context(), &stdout, &stderr, g, []string{"get-public", "--slug", "missing"}, factoryFor(stub))
	if code != exitClientError {
		t.Fatalf("NotFound exit=%d, want %d", code, exitClientError)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout should be empty on RPC failure, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "gRPC NotFound") {
		t.Errorf("stderr should carry the gRPC status line, got %q", stderr.String())
	}
}

func TestRunPlaytestGetPublic_UnavailableExit2(t *testing.T) {
	stub := &stubPlaytestClient{
		getPublicFunc: func(_ context.Context, _ *pb.GetPublicPlaytestRequest, _ ...grpc.CallOption) (*pb.GetPublicPlaytestResponse, error) {
			return nil, status.Error(codes.Unavailable, "no backend")
		},
	}
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565"}
	code := runPlaytest(t.Context(), &stdout, &stderr, g, []string{"get-public", "--slug", "x"}, factoryFor(stub))
	if code != exitTransportError {
		t.Fatalf("Unavailable exit=%d, want %d", code, exitTransportError)
	}
}

func TestRunPlaytestGetPublic_DryRunPrintsRequestNoCall(t *testing.T) {
	stub := &stubPlaytestClient{
		getPublicFunc: func(_ context.Context, _ *pb.GetPublicPlaytestRequest, _ ...grpc.CallOption) (*pb.GetPublicPlaytestResponse, error) {
			t.Fatal("RPC should not be called in --dry-run mode")
			return nil, nil
		},
	}
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565"}
	code := runPlaytest(t.Context(), &stdout, &stderr, g, []string{"get-public", "--slug", testSlugDemo01, "--dry-run"}, factoryFor(stub))
	if code != exitOK {
		t.Fatalf("dry-run exit=%d, want %d (stderr=%q)", code, exitOK, stderr.String())
	}
	var got map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &got); err != nil {
		t.Fatalf("stdout not JSON: %v: %q", err, stdout.String())
	}
	if got["slug"] != testSlugDemo01 {
		t.Errorf("dry-run output should carry the request body, got %v", got)
	}
	if stub.calls != 0 {
		t.Errorf("dry-run should not dial, got %d calls", stub.calls)
	}
}

func TestRunPlaytestGetPublic_MissingSlugExit3(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565"}
	code := runPlaytest(t.Context(), &stdout, &stderr, g, []string{"get-public"}, factoryFor(nil))
	if code != exitLocalError {
		t.Fatalf("missing --slug exit=%d, want %d", code, exitLocalError)
	}
	if !strings.Contains(stderr.String(), "--slug") {
		t.Errorf("stderr should mention --slug, got %q", stderr.String())
	}
}

func TestRunPlaytest_UnknownAction(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565"}
	code := runPlaytest(t.Context(), &stdout, &stderr, g, []string{"banana"}, factoryFor(nil))
	if code != exitLocalError {
		t.Fatalf("unknown action exit=%d, want %d", code, exitLocalError)
	}
	if !strings.Contains(stderr.String(), "banana") {
		t.Errorf("stderr should name the unknown action, got %q", stderr.String())
	}
}

func TestRunPlaytest_NoAction(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565"}
	code := runPlaytest(t.Context(), &stdout, &stderr, g, nil, factoryFor(nil))
	if code != exitLocalError {
		t.Fatalf("no action exit=%d, want %d", code, exitLocalError)
	}
}

func TestRunPlaytestGetPublic_FactoryFailsExit2(t *testing.T) {
	failingFactory := func(ctx context.Context) (pb.PlaytesthubServiceClient, context.Context, func() error, error) {
		return nil, nil, nil, errors.New("dial: refused")
	}
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565"}
	code := runPlaytest(t.Context(), &stdout, &stderr, g, []string{"get-public", "--slug", "x"}, failingFactory)
	if code != exitTransportError {
		t.Fatalf("dial failure exit=%d, want %d", code, exitTransportError)
	}
}

// --- 10.5: get-player ---

func TestRunPlaytestGetPlayer_Success(t *testing.T) {
	stub := &stubPlaytestClient{
		getPlayerFunc: func(_ context.Context, in *pb.GetPlaytestForPlayerRequest, _ ...grpc.CallOption) (*pb.GetPlaytestForPlayerResponse, error) {
			if in.Slug != testSlugDemo01 {
				t.Errorf("slug=%q, want demo-01", in.Slug)
			}
			return &pb.GetPlaytestForPlayerResponse{}, nil
		},
	}
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565"}
	code := runPlaytest(t.Context(), &stdout, &stderr, g, []string{"get-player", "--slug", testSlugDemo01}, factoryFor(stub))
	if code != exitOK {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitOK, stderr.String())
	}
	if stub.calls != 1 {
		t.Fatalf("expected 1 RPC call, got %d", stub.calls)
	}
}

func TestRunPlaytestGetPlayer_MissingSlug(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565"}
	code := runPlaytest(t.Context(), &stdout, &stderr, g, []string{"get-player"}, factoryFor(nil))
	if code != exitLocalError {
		t.Fatalf("missing --slug exit=%d, want %d", code, exitLocalError)
	}
}

// --- 10.5: get (admin) ---

func TestRunPlaytestGet_DryRun(t *testing.T) {
	stub := &stubPlaytestClient{
		adminGetFunc: func(_ context.Context, _ *pb.AdminGetPlaytestRequest, _ ...grpc.CallOption) (*pb.AdminGetPlaytestResponse, error) {
			t.Fatal("dry-run must not dial")
			return nil, nil
		},
	}
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565", Namespace: testNamespaceDev}
	code := runPlaytest(t.Context(), &stdout, &stderr, g, []string{"get", "--id", "p1", "--dry-run"}, factoryFor(stub))
	if code != exitOK {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitOK, stderr.String())
	}
	var got map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &got); err != nil {
		t.Fatalf("stdout not JSON: %v: %q", err, stdout.String())
	}
	if got["namespace"] != testNamespaceDev || got["playtest_id"] != "p1" {
		t.Errorf("dry-run body wrong: %v", got)
	}
}

func TestRunPlaytestGet_MissingNamespace(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565"}
	code := runPlaytest(t.Context(), &stdout, &stderr, g, []string{"get", "--id", "p1"}, factoryFor(nil))
	if code != exitLocalError {
		t.Fatalf("missing namespace exit=%d, want %d", code, exitLocalError)
	}
	if !strings.Contains(stderr.String(), "namespace") {
		t.Errorf("stderr should name namespace, got %q", stderr.String())
	}
}

// --- 10.5: list ---

func TestRunPlaytestList_Success(t *testing.T) {
	stub := &stubPlaytestClient{
		listFunc: func(_ context.Context, in *pb.ListPlaytestsRequest, _ ...grpc.CallOption) (*pb.ListPlaytestsResponse, error) {
			if in.Namespace != testNamespaceDev {
				t.Errorf("namespace=%q, want %s", in.Namespace, testNamespaceDev)
			}
			return &pb.ListPlaytestsResponse{Playtests: []*pb.Playtest{{Id: "p1"}}}, nil
		},
	}
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565", Namespace: testNamespaceDev}
	code := runPlaytest(t.Context(), &stdout, &stderr, g, []string{"list"}, factoryFor(stub))
	if code != exitOK {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitOK, stderr.String())
	}
}

// --- 10.5: create ---

func TestRunPlaytestCreate_DryRunBuildsRequest(t *testing.T) {
	stub := &stubPlaytestClient{}
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565", Namespace: testNamespaceDev}
	code := runPlaytest(t.Context(), &stdout, &stderr, g, []string{
		"create",
		"--slug", testSlugDemo01,
		"--title", "Demo Title",
		"--description", "desc",
		"--platforms", "STEAM,XBOX",
		"--starts-at", "2026-05-01T12:00:00Z",
		"--ends-at", "2026-06-01T12:00:00Z",
		"--nda-required",
		"--nda-text", "raw nda",
		"--distribution-model", "STEAM_KEYS",
		"--dry-run",
	}, factoryFor(stub))
	if code != exitOK {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitOK, stderr.String())
	}
	var got map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &got); err != nil {
		t.Fatalf("stdout not JSON: %v: %q", err, stdout.String())
	}
	if got["slug"] != testSlugDemo01 || got["title"] != "Demo Title" {
		t.Errorf("body missing core fields: %v", got)
	}
	if got["distribution_model"] != "DISTRIBUTION_MODEL_STEAM_KEYS" {
		t.Errorf("distribution_model wrong: %v", got["distribution_model"])
	}
	platforms, ok := got["platforms"].([]any)
	if !ok || len(platforms) != 2 {
		t.Fatalf("platforms wrong: %v", got["platforms"])
	}
}

func TestRunPlaytestCreate_RejectsBadPlatform(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565", Namespace: testNamespaceDev}
	code := runPlaytest(t.Context(), &stdout, &stderr, g, []string{
		"create",
		"--slug", testSlugDemo01,
		"--title", "Demo",
		"--platforms", "GAMEBOY",
		"--dry-run",
	}, factoryFor(nil))
	if code != exitLocalError {
		t.Fatalf("bad platform exit=%d, want %d", code, exitLocalError)
	}
	if !strings.Contains(stderr.String(), "GAMEBOY") {
		t.Errorf("stderr should name the bad platform, got %q", stderr.String())
	}
}

func TestRunPlaytestCreate_RejectsBadTimestamp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565", Namespace: testNamespaceDev}
	code := runPlaytest(t.Context(), &stdout, &stderr, g, []string{
		"create",
		"--slug", testSlugDemo01,
		"--title", "Demo",
		"--starts-at", "yesterday",
		"--dry-run",
	}, factoryFor(nil))
	if code != exitLocalError {
		t.Fatalf("bad timestamp exit=%d, want %d", code, exitLocalError)
	}
}

func TestRunPlaytestCreate_RejectsBadDistributionModel(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565", Namespace: testNamespaceDev}
	code := runPlaytest(t.Context(), &stdout, &stderr, g, []string{
		"create",
		"--slug", testSlugDemo01,
		"--title", "Demo",
		"--distribution-model", "BITCOIN",
		"--dry-run",
	}, factoryFor(nil))
	if code != exitLocalError {
		t.Fatalf("bad dm exit=%d, want %d", code, exitLocalError)
	}
}

func TestRunPlaytestCreate_NDATextFromFile(t *testing.T) {
	tmp := t.TempDir() + "/nda.md"
	if err := writeFile(tmp, "FILE-LOADED-NDA"); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565", Namespace: testNamespaceDev}
	code := runPlaytest(t.Context(), &stdout, &stderr, g, []string{
		"create",
		"--slug", testSlugDemo01,
		"--title", "Demo",
		"--nda-required",
		"--nda-text", "@" + tmp,
		"--dry-run",
	}, factoryFor(nil))
	if code != exitOK {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitOK, stderr.String())
	}
	var got map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &got); err != nil {
		t.Fatalf("stdout not JSON: %v: %q", err, stdout.String())
	}
	if got["nda_text"] != "FILE-LOADED-NDA" {
		t.Errorf("nda_text not loaded from file: %v", got["nda_text"])
	}
}

func TestRunPlaytestCreate_InitialCodeQuantityOptional(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565", Namespace: testNamespaceDev}
	code := runPlaytest(t.Context(), &stdout, &stderr, g, []string{
		"create",
		"--slug", testSlugDemo01,
		"--title", "Demo",
		"--initial-code-quantity", "100",
		"--dry-run",
	}, factoryFor(nil))
	if code != exitOK {
		t.Fatalf("exit=%d, want %d", code, exitOK)
	}
	var got map[string]any
	_ = json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &got)
	// protojson encodes int32 as a number; some implementations as string. Accept both.
	v := got["initial_code_quantity"]
	switch x := v.(type) {
	case float64:
		if int(x) != 100 {
			t.Errorf("initial_code_quantity=%v, want 100", v)
		}
	case string:
		if x != "100" {
			t.Errorf("initial_code_quantity=%v, want 100", v)
		}
	default:
		t.Errorf("initial_code_quantity missing or wrong type: %v", v)
	}
}

// --- 10.5: edit ---

func TestRunPlaytestEdit_RejectsImmutableSlug(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565", Namespace: testNamespaceDev}
	code := runPlaytest(t.Context(), &stdout, &stderr, g, []string{
		"edit",
		"--id", "p1",
		"--slug", "new-slug",
		"--dry-run",
	}, factoryFor(nil))
	if code != exitLocalError {
		t.Fatalf("immutable --slug exit=%d, want %d", code, exitLocalError)
	}
	if !strings.Contains(stderr.String(), "slug is immutable") {
		t.Errorf("stderr should explain immutability, got %q", stderr.String())
	}
}

func TestRunPlaytestEdit_RejectsStatusFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565", Namespace: testNamespaceDev}
	code := runPlaytest(t.Context(), &stdout, &stderr, g, []string{
		"edit",
		"--id", "p1",
		"--status=OPEN",
		"--dry-run",
	}, factoryFor(nil))
	if code != exitLocalError {
		t.Fatalf("immutable --status exit=%d, want %d", code, exitLocalError)
	}
	if !strings.Contains(stderr.String(), "transition") {
		t.Errorf("stderr should redirect to transition, got %q", stderr.String())
	}
}

func TestRunPlaytestEdit_DryRunBuildsRequest(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565", Namespace: testNamespaceDev}
	code := runPlaytest(t.Context(), &stdout, &stderr, g, []string{
		"edit",
		"--id", "p1",
		"--title", "New Title",
		"--platforms", "STEAM",
		"--dry-run",
	}, factoryFor(nil))
	if code != exitOK {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitOK, stderr.String())
	}
	var got map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &got); err != nil {
		t.Fatalf("stdout not JSON: %v: %q", err, stdout.String())
	}
	if got["playtest_id"] != "p1" || got["title"] != "New Title" {
		t.Errorf("body wrong: %v", got)
	}
	if _, ok := got["slug"]; ok {
		t.Errorf("edit request must not carry slug, got %v", got)
	}
}

// --- 10.5: delete ---

func TestRunPlaytestDelete_Success(t *testing.T) {
	stub := &stubPlaytestClient{
		deleteFunc: func(_ context.Context, in *pb.SoftDeletePlaytestRequest, _ ...grpc.CallOption) (*pb.SoftDeletePlaytestResponse, error) {
			if in.PlaytestId != "p1" || in.Namespace != "dev" {
				t.Errorf("delete body wrong: %+v", in)
			}
			return &pb.SoftDeletePlaytestResponse{}, nil
		},
	}
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565", Namespace: testNamespaceDev}
	code := runPlaytest(t.Context(), &stdout, &stderr, g, []string{"delete", "--id", "p1"}, factoryFor(stub))
	if code != exitOK {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitOK, stderr.String())
	}
	if stub.calls != 1 {
		t.Errorf("expected 1 call, got %d", stub.calls)
	}
}

// --- 10.5: transition ---

func TestRunPlaytestTransition_NormalizesShortStatus(t *testing.T) {
	stub := &stubPlaytestClient{
		transitionFunc: func(_ context.Context, in *pb.TransitionPlaytestStatusRequest, _ ...grpc.CallOption) (*pb.TransitionPlaytestStatusResponse, error) {
			if in.TargetStatus != pb.PlaytestStatus_PLAYTEST_STATUS_OPEN {
				t.Errorf("target=%v, want OPEN", in.TargetStatus)
			}
			return &pb.TransitionPlaytestStatusResponse{Playtest: &pb.Playtest{Id: in.PlaytestId, UpdatedAt: timestamppb.Now()}}, nil
		},
	}
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565", Namespace: testNamespaceDev}
	code := runPlaytest(t.Context(), &stdout, &stderr, g, []string{"transition", "--id", "p1", "--to", "OPEN"}, factoryFor(stub))
	if code != exitOK {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitOK, stderr.String())
	}
}

func TestRunPlaytestTransition_RejectsUnknownStatus(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565", Namespace: testNamespaceDev}
	code := runPlaytest(t.Context(), &stdout, &stderr, g, []string{"transition", "--id", "p1", "--to", "ARCHIVED"}, factoryFor(nil))
	if code != exitLocalError {
		t.Fatalf("bad status exit=%d, want %d", code, exitLocalError)
	}
	if !strings.Contains(stderr.String(), "ARCHIVED") {
		t.Errorf("stderr should name unknown status, got %q", stderr.String())
	}
}

// --- helpers ---

func writeFile(path, contents string) error {
	return os.WriteFile(path, []byte(contents), 0o600)
}
