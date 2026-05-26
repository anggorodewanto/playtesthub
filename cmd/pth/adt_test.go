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

// TestRunADTGamesList_DryRun pins that the new `pth adt games list`
// subcommand emits a request JSON body and does NOT dial. Mirrors
// TestRunAuditList_DryRun.
func TestRunADTGamesList_DryRun(t *testing.T) {
	stub := &stubPlaytestClient{
		listADTGamesFunc: func(_ context.Context, _ *pb.ListADTGamesRequest, _ ...grpc.CallOption) (*pb.ListADTGamesResponse, error) {
			t.Fatal("dry-run must not dial")
			return nil, nil
		},
	}
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565", Namespace: testNamespaceDev}
	code := runADT(t.Context(), &stdout, &stderr, g, []string{
		"games", "list",
		"--linkage-id", "01234567-89ab-cdef-0123-456789abcdef",
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
	if got["adt_linkage_id"] != "01234567-89ab-cdef-0123-456789abcdef" {
		t.Errorf("adt_linkage_id = %v", got["adt_linkage_id"])
	}
}

// TestRunADTBuildCheck_DryRun pins that `pth adt build check` emits a
// CheckADTBuild request body and does not dial.
func TestRunADTBuildCheck_DryRun(t *testing.T) {
	stub := &stubPlaytestClient{}
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565", Namespace: testNamespaceDev}
	code := runADT(t.Context(), &stdout, &stderr, g, []string{
		"build", "check",
		"--playtest-id", "01234567-89ab-cdef-0123-456789abcdef",
		"--dry-run",
	}, factoryFor(stub))
	if code != exitOK {
		t.Fatalf("exit=%d, want %d (stderr=%q)", code, exitOK, stderr.String())
	}
	var got map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &got); err != nil {
		t.Fatalf("stdout not JSON: %v: %q", err, stdout.String())
	}
	if got["playtest_id"] != "01234567-89ab-cdef-0123-456789abcdef" {
		t.Errorf("playtest_id = %v", got["playtest_id"])
	}
}

func TestRunADTBuildCheck_RequiresPlaytestID(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565", Namespace: testNamespaceDev}
	code := runADT(t.Context(), &stdout, &stderr, g, []string{"build", "check", "--dry-run"}, factoryFor(&stubPlaytestClient{}))
	if code != exitLocalError {
		t.Fatalf("exit=%d, want %d", code, exitLocalError)
	}
	if !strings.Contains(stderr.String(), "--playtest-id") {
		t.Errorf("stderr missing --playtest-id hint: %q", stderr.String())
	}
}

func TestRunADTGamesList_RequiresLinkageID(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565", Namespace: testNamespaceDev}
	code := runADT(t.Context(), &stdout, &stderr, g, []string{"games", "list", "--dry-run"}, factoryFor(&stubPlaytestClient{}))
	if code != exitLocalError {
		t.Fatalf("exit=%d, want %d", code, exitLocalError)
	}
	if !strings.Contains(stderr.String(), "--linkage-id") {
		t.Errorf("stderr missing --linkage-id hint: %q", stderr.String())
	}
}

func TestRunADTGamesList_RequiresNamespace(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565"}
	code := runADT(t.Context(), &stdout, &stderr, g, []string{
		"games", "list",
		"--linkage-id", "01234567-89ab-cdef-0123-456789abcdef",
		"--dry-run",
	}, factoryFor(&stubPlaytestClient{}))
	if code != exitLocalError {
		t.Fatalf("exit=%d, want %d", code, exitLocalError)
	}
	if !strings.Contains(stderr.String(), "--namespace") {
		t.Errorf("stderr missing --namespace hint: %q", stderr.String())
	}
}

func TestRunADTGames_UnknownAction(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565"}
	code := runADT(t.Context(), &stdout, &stderr, g, []string{"games", "explode"}, factoryFor(&stubPlaytestClient{}))
	if code != exitLocalError {
		t.Fatalf("exit=%d, want %d", code, exitLocalError)
	}
}

// TestRunADTLinkageRecover_DryRun pins that the new orphan-recovery
// subcommand emits a request JSON body and does NOT dial. Mirrors
// TestRunADTGamesList_DryRun.
func TestRunADTLinkageRecover_DryRun(t *testing.T) {
	stub := &stubPlaytestClient{
		recoverADTFunc: func(_ context.Context, _ *pb.RecoverADTLinkageRequest, _ ...grpc.CallOption) (*pb.RecoverADTLinkageResponse, error) {
			t.Fatal("dry-run must not dial")
			return nil, nil
		},
	}
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565", Namespace: testNamespaceDev}
	code := runADT(t.Context(), &stdout, &stderr, g, []string{
		"linkage", "recover",
		"--adt-namespace", "adt-ns-orphan",
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
	if got["adt_namespace"] != "adt-ns-orphan" {
		t.Errorf("adt_namespace = %v", got["adt_namespace"])
	}
}

func TestRunADTLinkageRecover_RequiresADTNamespace(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565", Namespace: testNamespaceDev}
	code := runADT(t.Context(), &stdout, &stderr, g, []string{"linkage", "recover", "--dry-run"}, factoryFor(&stubPlaytestClient{}))
	if code != exitLocalError {
		t.Fatalf("exit=%d, want %d", code, exitLocalError)
	}
	if !strings.Contains(stderr.String(), "--adt-namespace") {
		t.Errorf("stderr missing --adt-namespace hint: %q", stderr.String())
	}
}

func TestRunADTLinkageRecover_RequiresNamespace(t *testing.T) {
	var stdout, stderr bytes.Buffer
	g := &Globals{Addr: "localhost:6565"}
	code := runADT(t.Context(), &stdout, &stderr, g, []string{
		"linkage", "recover",
		"--adt-namespace", "adt-ns-orphan",
		"--dry-run",
	}, factoryFor(&stubPlaytestClient{}))
	if code != exitLocalError {
		t.Fatalf("exit=%d, want %d", code, exitLocalError)
	}
	if !strings.Contains(stderr.String(), "--namespace") {
		t.Errorf("stderr missing --namespace hint: %q", stderr.String())
	}
}
