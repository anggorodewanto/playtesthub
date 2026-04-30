package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type stubPlaytestClient struct {
	pb.PlaytesthubServiceClient

	getPublicFunc func(ctx context.Context, in *pb.GetPublicPlaytestRequest, opts ...grpc.CallOption) (*pb.GetPublicPlaytestResponse, error)
	calls         int
}

func (s *stubPlaytestClient) GetPublicPlaytest(ctx context.Context, in *pb.GetPublicPlaytestRequest, opts ...grpc.CallOption) (*pb.GetPublicPlaytestResponse, error) {
	s.calls++
	return s.getPublicFunc(ctx, in, opts...)
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
