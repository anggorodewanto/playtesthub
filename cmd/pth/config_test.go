package main

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"google.golang.org/grpc"
)

func TestRunPublicConfig_Success(t *testing.T) {
	stub := &stubPlaytestClient{
		publicConfigFunc: func(_ context.Context, _ *pb.GetPublicConfigRequest, _ ...grpc.CallOption) (*pb.GetPublicConfigResponse, error) {
			return &pb.GetPublicConfigResponse{PlayerBaseUrl: "https://play.example.com"}, nil
		},
	}
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565"}
	code := runPublicConfig(t.Context(), &stdout, &stderr, g, nil, factoryFor(stub))
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
	if got["player_base_url"] != "https://play.example.com" {
		t.Errorf("player_base_url round-trip wrong: %v", got)
	}
	if !g.Anon {
		t.Error("--anon must be implied for public-config")
	}
}

func TestRunPublicConfig_DryRun_NoCall(t *testing.T) {
	stub := &stubPlaytestClient{
		publicConfigFunc: func(_ context.Context, _ *pb.GetPublicConfigRequest, _ ...grpc.CallOption) (*pb.GetPublicConfigResponse, error) {
			t.Fatal("RPC should not be called in --dry-run mode")
			return nil, nil
		},
	}
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565"}
	code := runPublicConfig(t.Context(), &stdout, &stderr, g, []string{"--dry-run"}, factoryFor(stub))
	if code != exitOK {
		t.Fatalf("dry-run exit=%d, want %d (stderr=%q)", code, exitOK, stderr.String())
	}
	if stub.calls != 0 {
		t.Errorf("dry-run should not dial, got %d calls", stub.calls)
	}
}
