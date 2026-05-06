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

func TestRunAuditList_DryRun(t *testing.T) {
	stub := &stubPlaytestClient{
		listAuditLogFunc: func(_ context.Context, _ *pb.ListAuditLogRequest, _ ...grpc.CallOption) (*pb.ListAuditLogResponse, error) {
			t.Fatal("dry-run must not dial")
			return nil, nil
		},
	}
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565", Namespace: testNamespaceDev}
	code := runAudit(t.Context(), &stdout, &stderr, g, []string{
		"list",
		"--playtest", "p-1",
		"--actor", "system",
		"--action", "playtest.create",
		"--page-size", "7",
		"--dry-run",
	}, factoryFor(stub))
	if code != exitOK {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitOK, stderr.String())
	}
	var got map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &got); err != nil {
		t.Fatalf("stdout not JSON: %v: %q", err, stdout.String())
	}
	if got["namespace"] != testNamespaceDev {
		t.Errorf("namespace = %v, want %s", got["namespace"], testNamespaceDev)
	}
	if got["playtest_id"] != "p-1" {
		t.Errorf("playtest_id = %v, want p-1", got["playtest_id"])
	}
	if got["actor_filter"] != "system" {
		t.Errorf("actor_filter = %v, want system", got["actor_filter"])
	}
	if got["action_filter"] != "playtest.create" {
		t.Errorf("action_filter = %v, want playtest.create", got["action_filter"])
	}
	if got["page_size"] != float64(7) {
		t.Errorf("page_size = %v, want 7", got["page_size"])
	}
}

func TestRunAuditList_Success(t *testing.T) {
	stub := &stubPlaytestClient{
		listAuditLogFunc: func(_ context.Context, in *pb.ListAuditLogRequest, _ ...grpc.CallOption) (*pb.ListAuditLogResponse, error) {
			if in.GetPlaytestId() != "p-1" {
				t.Errorf("playtest_id = %q", in.GetPlaytestId())
			}
			return &pb.ListAuditLogResponse{
				Entries:       []*pb.AuditLogEntry{{Id: "a1", Action: "playtest.create"}},
				NextPageToken: "next-cursor",
			}, nil
		},
	}
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565", Namespace: testNamespaceDev}
	code := runAudit(t.Context(), &stdout, &stderr, g, []string{
		"list",
		"--playtest", "p-1",
	}, factoryFor(stub))
	if code != exitOK {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitOK, stderr.String())
	}
	if !strings.Contains(stdout.String(), "next-cursor") {
		t.Errorf("stdout missing next_page_token: %q", stdout.String())
	}
}

func TestRunAuditList_RequiresPlaytest(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565", Namespace: testNamespaceDev}
	code := runAudit(t.Context(), &stdout, &stderr, g, []string{
		"list",
		"--dry-run",
	}, factoryFor(&stubPlaytestClient{}))
	if code != exitLocalError {
		t.Fatalf("exit=%d, want %d", code, exitLocalError)
	}
	if !strings.Contains(stderr.String(), "--playtest") {
		t.Errorf("stderr missing --playtest hint: %q", stderr.String())
	}
}

func TestRunAuditList_RequiresNamespace(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565"}
	code := runAudit(t.Context(), &stdout, &stderr, g, []string{
		"list",
		"--playtest", "p-1",
		"--dry-run",
	}, factoryFor(&stubPlaytestClient{}))
	if code != exitLocalError {
		t.Fatalf("exit=%d, want %d", code, exitLocalError)
	}
	if !strings.Contains(stderr.String(), "--namespace") {
		t.Errorf("stderr missing --namespace hint: %q", stderr.String())
	}
}

func TestRunAudit_UnknownAction(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565"}
	code := runAudit(t.Context(), &stdout, &stderr, g, []string{"explode"}, factoryFor(&stubPlaytestClient{}))
	if code != exitLocalError {
		t.Fatalf("exit=%d, want %d", code, exitLocalError)
	}
}

func TestRunAudit_NoAction(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565"}
	code := runAudit(t.Context(), &stdout, &stderr, g, nil, factoryFor(&stubPlaytestClient{}))
	if code != exitLocalError {
		t.Fatalf("exit=%d, want %d", code, exitLocalError)
	}
}
