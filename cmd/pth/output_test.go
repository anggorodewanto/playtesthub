package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestExitCodeForGRPC(t *testing.T) {
	tests := []struct {
		name string
		code codes.Code
		want int
	}{
		{"ok", codes.OK, exitOK},
		{"not_found", codes.NotFound, exitClientError},
		{"invalid_argument", codes.InvalidArgument, exitClientError},
		{"failed_precondition", codes.FailedPrecondition, exitClientError},
		{"unauthenticated", codes.Unauthenticated, exitClientError},
		{"permission_denied", codes.PermissionDenied, exitClientError},
		{"unimplemented", codes.Unimplemented, exitClientError},
		{"internal", codes.Internal, exitClientError},
		{"resource_exhausted", codes.ResourceExhausted, exitClientError},
		{"unknown", codes.Unknown, exitClientError},
		{"unavailable", codes.Unavailable, exitTransportError},
		{"deadline_exceeded", codes.DeadlineExceeded, exitTransportError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := exitCodeForGRPC(tt.code)
			if got != tt.want {
				t.Fatalf("exitCodeForGRPC(%v) = %d, want %d", tt.code, got, tt.want)
			}
		})
	}
}

const testSlugDemo01 = "demo-01"

func TestWriteJSONProto_UsesProtoFieldNames(t *testing.T) {
	resp := &pb.GetPublicPlaytestResponse{
		Playtest: &pb.PublicPlaytest{
			Slug:  testSlugDemo01,
			Title: "Demo",
		},
	}
	var buf bytes.Buffer
	if err := writeJSONProto(&buf, resp); err != nil {
		t.Fatalf("writeJSONProto: %v", err)
	}
	out := buf.String()
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("output missing trailing newline: %q", out)
	}
	// One line of JSON.
	if strings.Count(strings.TrimRight(out, "\n"), "\n") != 0 {
		t.Fatalf("expected single-line JSON, got: %q", out)
	}
	var generic map[string]any
	if err := json.Unmarshal([]byte(out), &generic); err != nil {
		t.Fatalf("output not valid JSON: %v\n%s", err, out)
	}
	pt, ok := generic["playtest"].(map[string]any)
	if !ok {
		t.Fatalf("expected proto field name 'playtest' (snake_case), got keys=%v", keys(generic))
	}
	if pt["slug"] != testSlugDemo01 {
		t.Fatalf("slug round-trip wrong: %v", pt)
	}
}

func TestWriteJSONProto_OmitsUnpopulated(t *testing.T) {
	// EmitUnpopulated=false per cli.md §8 ("`null` / absent fields omitted
	// per `protojson`"). An empty response should serialise to `{}`, not
	// to a noisy zero-valued blob — keeps `jq` pipelines terse.
	var buf bytes.Buffer
	if err := writeJSONProto(&buf, &pb.GetPublicPlaytestResponse{}); err != nil {
		t.Fatalf("writeJSONProto: %v", err)
	}
	got := strings.TrimSpace(buf.String())
	if got != "{}" {
		t.Fatalf("expected '{}', got: %q", got)
	}
}

func TestWriteGRPCError_ShapeMatchesContract(t *testing.T) {
	err := status.Error(codes.NotFound, "playtest not found")
	var buf bytes.Buffer
	writeGRPCError(&buf, err)
	want := "gRPC NotFound: playtest not found\n"
	if buf.String() != want {
		t.Fatalf("writeGRPCError = %q, want %q", buf.String(), want)
	}
}

func TestWriteGRPCError_NonStatusErrorFallsBackToUnknown(t *testing.T) {
	var buf bytes.Buffer
	writeGRPCError(&buf, errors.New("connection refused"))
	want := "gRPC Unknown: connection refused\n"
	if buf.String() != want {
		t.Fatalf("writeGRPCError(non-status) = %q, want %q", buf.String(), want)
	}
}

func TestWriteGRPCError_NilIsNoOp(t *testing.T) {
	var buf bytes.Buffer
	writeGRPCError(&buf, nil)
	if buf.Len() != 0 {
		t.Fatalf("nil error should not write, got %q", buf.String())
	}
}

func TestWriteJSONValue_TerseDocument(t *testing.T) {
	var buf bytes.Buffer
	if err := writeJSONValue(&buf, map[string]any{"status": "OK", "latencyMs": 42}); err != nil {
		t.Fatalf("writeJSONValue: %v", err)
	}
	out := strings.TrimRight(buf.String(), "\n")
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output not valid JSON: %v: %q", err, out)
	}
	if got["status"] != "OK" {
		t.Fatalf("expected status=OK, got %v", got)
	}
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
