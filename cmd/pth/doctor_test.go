package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRunDoctor_NotFoundIsOK(t *testing.T) {
	stub := &stubPlaytestClient{
		getPublicFunc: func(_ context.Context, in *pb.GetPublicPlaytestRequest, _ ...grpc.CallOption) (*pb.GetPublicPlaytestResponse, error) {
			if in.Slug != doctorSentinelSlug {
				t.Errorf("doctor sentinel slug=%q, want %q", in.Slug, doctorSentinelSlug)
			}
			return nil, status.Error(codes.NotFound, "no such playtest")
		},
	}
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565", Timeout: time.Second}
	code := runDoctor(t.Context(), &stdout, &stderr, g, nil, factoryFor(stub))
	if code != exitOK {
		t.Fatalf("doctor exit=%d, want %d (stderr=%q)", code, exitOK, stderr.String())
	}
	var report doctorReport
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &report); err != nil {
		t.Fatalf("stdout not JSON: %v: %q", err, stdout.String())
	}
	if report.Status != "OK" {
		t.Errorf("status=%q, want OK", report.Status)
	}
	if report.GrpcCode != "NotFound" {
		t.Errorf("grpcCode=%q, want NotFound", report.GrpcCode)
	}
	if report.Addr != "localhost:6565" {
		t.Errorf("addr=%q", report.Addr)
	}
	if !report.Insecure {
		t.Error("loopback default should resolve to insecure=true")
	}
}

func TestRunDoctor_InvalidArgumentAlsoOK(t *testing.T) {
	stub := &stubPlaytestClient{
		getPublicFunc: func(_ context.Context, _ *pb.GetPublicPlaytestRequest, _ ...grpc.CallOption) (*pb.GetPublicPlaytestResponse, error) {
			return nil, status.Error(codes.InvalidArgument, "bad slug")
		},
	}
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565"}
	code := runDoctor(t.Context(), &stdout, &stderr, g, nil, factoryFor(stub))
	if code != exitOK {
		t.Fatalf("doctor exit=%d, want %d", code, exitOK)
	}
	var report doctorReport
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &report); err != nil {
		t.Fatalf("stdout not JSON: %v", err)
	}
	if report.Status != "OK" {
		t.Errorf("status=%q, want OK", report.Status)
	}
}

func TestRunDoctor_UnavailableExit2(t *testing.T) {
	stub := &stubPlaytestClient{
		getPublicFunc: func(_ context.Context, _ *pb.GetPublicPlaytestRequest, _ ...grpc.CallOption) (*pb.GetPublicPlaytestResponse, error) {
			return nil, status.Error(codes.Unavailable, "no backend")
		},
	}
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "down.example.com:6565"}
	code := runDoctor(t.Context(), &stdout, &stderr, g, nil, factoryFor(stub))
	if code != exitTransportError {
		t.Fatalf("Unavailable exit=%d, want %d", code, exitTransportError)
	}
	var report doctorReport
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &report); err != nil {
		t.Fatalf("stdout not JSON: %v", err)
	}
	if report.Status != "FAILED" {
		t.Errorf("status=%q, want FAILED", report.Status)
	}
	if !strings.Contains(stderr.String(), "Unavailable") {
		t.Errorf("stderr should carry gRPC Unavailable, got %q", stderr.String())
	}
}

func TestRunDoctor_RejectsExtraArg(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565"}
	code := runDoctor(t.Context(), &stdout, &stderr, g, []string{"unexpected"}, factoryFor(nil))
	if code != exitLocalError {
		t.Fatalf("exit=%d, want %d", code, exitLocalError)
	}
}

func TestRunDoctor_BasePathEchoed(t *testing.T) {
	stub := &stubPlaytestClient{
		getPublicFunc: func(_ context.Context, _ *pb.GetPublicPlaytestRequest, _ ...grpc.CallOption) (*pb.GetPublicPlaytestResponse, error) {
			return nil, status.Error(codes.NotFound, "no")
		},
	}
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565", BasePath: "/playtesthub"}
	code := runDoctor(t.Context(), &stdout, &stderr, g, nil, factoryFor(stub))
	if code != exitOK {
		t.Fatalf("exit=%d", code)
	}
	var report doctorReport
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &report); err != nil {
		t.Fatalf("stdout not JSON: %v", err)
	}
	if report.BasePath != "/playtesthub" {
		t.Errorf("basePath=%q, want /playtesthub", report.BasePath)
	}
}
