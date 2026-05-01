package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"google.golang.org/grpc"
)

func TestRunApplicantSignup_DryRun(t *testing.T) {
	stub := &stubPlaytestClient{
		signupFunc: func(_ context.Context, _ *pb.SignupRequest, _ ...grpc.CallOption) (*pb.SignupResponse, error) {
			t.Fatal("dry-run must not dial")
			return nil, nil
		},
	}
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565"}
	code := runApplicant(t.Context(), &stdout, &stderr, g, []string{
		"signup",
		"--slug", testSlugDemo01,
		"--platforms", "STEAM,XBOX",
		"--dry-run",
	}, factoryFor(stub))
	if code != exitOK {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitOK, stderr.String())
	}
	var got map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &got); err != nil {
		t.Fatalf("stdout not JSON: %v: %q", err, stdout.String())
	}
	if got["slug"] != testSlugDemo01 {
		t.Errorf("slug wrong: %v", got["slug"])
	}
	platforms, ok := got["platforms"].([]any)
	if !ok || len(platforms) != 2 {
		t.Fatalf("platforms wrong: %v", got["platforms"])
	}
}

func TestRunApplicantSignup_Success(t *testing.T) {
	stub := &stubPlaytestClient{
		signupFunc: func(_ context.Context, in *pb.SignupRequest, _ ...grpc.CallOption) (*pb.SignupResponse, error) {
			if in.Slug != testSlugDemo01 {
				t.Errorf("slug=%q", in.Slug)
			}
			return &pb.SignupResponse{Applicant: &pb.Applicant{Id: "a1"}}, nil
		},
	}
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565"}
	code := runApplicant(t.Context(), &stdout, &stderr, g, []string{
		"signup",
		"--slug", testSlugDemo01,
		"--platforms", "STEAM",
	}, factoryFor(stub))
	if code != exitOK {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitOK, stderr.String())
	}
	if stub.calls != 1 {
		t.Errorf("expected 1 call, got %d", stub.calls)
	}
}

func TestRunApplicantSignup_RequiresPlatforms(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565"}
	code := runApplicant(t.Context(), &stdout, &stderr, g, []string{
		"signup",
		"--slug", testSlugDemo01,
	}, factoryFor(nil))
	if code != exitLocalError {
		t.Fatalf("missing --platforms exit=%d, want %d", code, exitLocalError)
	}
	if !strings.Contains(stderr.String(), "--platforms") {
		t.Errorf("stderr should name --platforms, got %q", stderr.String())
	}
}

func TestRunApplicantSignup_RequiresSlug(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565"}
	code := runApplicant(t.Context(), &stdout, &stderr, g, []string{
		"signup",
		"--platforms", "STEAM",
	}, factoryFor(nil))
	if code != exitLocalError {
		t.Fatalf("missing --slug exit=%d, want %d", code, exitLocalError)
	}
}

func TestRunApplicantStatus_Success(t *testing.T) {
	stub := &stubPlaytestClient{
		applicantStatusFunc: func(_ context.Context, in *pb.GetApplicantStatusRequest, _ ...grpc.CallOption) (*pb.GetApplicantStatusResponse, error) {
			if in.Slug != testSlugDemo01 {
				t.Errorf("slug=%q", in.Slug)
			}
			return &pb.GetApplicantStatusResponse{Applicant: &pb.Applicant{Id: "a1", Status: pb.ApplicantStatus_APPLICANT_STATUS_PENDING}}, nil
		},
	}
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565"}
	code := runApplicant(t.Context(), &stdout, &stderr, g, []string{"status", "--slug", testSlugDemo01}, factoryFor(stub))
	if code != exitOK {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitOK, stderr.String())
	}
}

func TestRunApplicant_NoAction(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565"}
	code := runApplicant(t.Context(), &stdout, &stderr, g, nil, factoryFor(nil))
	if code != exitLocalError {
		t.Fatalf("no action exit=%d, want %d", code, exitLocalError)
	}
}

func TestRunApplicant_UnknownAction(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565"}
	code := runApplicant(t.Context(), &stdout, &stderr, g, []string{"banana"}, factoryFor(nil))
	if code != exitLocalError {
		t.Fatalf("unknown action exit=%d, want %d", code, exitLocalError)
	}
	if !strings.Contains(stderr.String(), "banana") {
		t.Errorf("stderr should name action, got %q", stderr.String())
	}
}
