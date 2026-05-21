package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
)

// TestGetADTClientDiagnostics_RequiresAuth pins that the diagnostic
// surface enforces the same admin-auth contract as UnlinkADT.
func TestGetADTClientDiagnostics_RequiresAuth(t *testing.T) {
	svr, _, _ := newTestServer()
	_, err := svr.GetADTClientDiagnostics(context.Background(), &pb.GetADTClientDiagnosticsRequest{Namespace: testNamespace})
	requireStatus(t, err, codes.Unauthenticated)
}

// TestGetADTClientDiagnostics_NamespaceMismatch_PermissionDenied pins
// the same namespace guard the rest of the admin RPCs share.
func TestGetADTClientDiagnostics_NamespaceMismatch_PermissionDenied(t *testing.T) {
	svr, _, _ := newTestServer()
	_, err := svr.GetADTClientDiagnostics(authCtx(uuid.New()), &pb.GetADTClientDiagnosticsRequest{Namespace: "not-the-right-one"})
	requireStatus(t, err, codes.PermissionDenied)
}

// TestGetADTClientDiagnostics_HTTPKind_AllPresent pins the "everything
// is configured" happy path: bootapp picks HTTP, every env var was set.
func TestGetADTClientDiagnostics_HTTPKind_AllPresent(t *testing.T) {
	svr, _, _ := newTestServer()
	svr.WithADTDiagnostics(ADTDiagnostics{
		ClientKind:            "http",
		AuthEnabled:           true,
		ADTBaseURLSet:         true,
		AGSBaseURLSet:         true,
		AGSIAMClientIDSet:     true,
		AGSIAMClientSecretSet: true,
	})
	resp, err := svr.GetADTClientDiagnostics(authCtx(uuid.New()), &pb.GetADTClientDiagnosticsRequest{Namespace: testNamespace})
	if err != nil {
		t.Fatalf("GetADTClientDiagnostics: %v", err)
	}
	if resp.GetAdtClientKind() != "http" {
		t.Fatalf("adt_client_kind = %q, want http", resp.GetAdtClientKind())
	}
	if !resp.GetAuthEnabled() || !resp.GetAdtBaseUrlSet() || !resp.GetAgsBaseUrlSet() ||
		!resp.GetAgsIamClientIdSet() || !resp.GetAgsIamClientSecretSet() {
		t.Fatalf("expected every presence flag true, got %+v", resp)
	}
}

// TestGetADTClientDiagnostics_MemKind_MissingSecret pins the
// 2026-05-21 silent-fallback case: ADT_BASE_URL + AGS_BASE_URL + client
// id were set but the secret was missing. RPC reports kind="mem" with
// the precise missing-flag so the operator can fix the deploy.
func TestGetADTClientDiagnostics_MemKind_MissingSecret(t *testing.T) {
	svr, _, _ := newTestServer()
	svr.WithADTDiagnostics(ADTDiagnostics{
		ClientKind:            "mem",
		AuthEnabled:           true,
		ADTBaseURLSet:         true,
		AGSBaseURLSet:         true,
		AGSIAMClientIDSet:     true,
		AGSIAMClientSecretSet: false,
	})
	resp, err := svr.GetADTClientDiagnostics(authCtx(uuid.New()), &pb.GetADTClientDiagnosticsRequest{Namespace: testNamespace})
	if err != nil {
		t.Fatalf("GetADTClientDiagnostics: %v", err)
	}
	if resp.GetAdtClientKind() != "mem" {
		t.Fatalf("adt_client_kind = %q, want mem", resp.GetAdtClientKind())
	}
	if resp.GetAgsIamClientSecretSet() {
		t.Fatal("ags_iam_client_secret_set must be false for the missing-secret scenario")
	}
	if !resp.GetAdtBaseUrlSet() || !resp.GetAgsBaseUrlSet() || !resp.GetAgsIamClientIdSet() {
		t.Fatalf("other presence flags must still report true, got %+v", resp)
	}
}

// TestGetADTClientDiagnostics_Unwired pins that a server with no
// WithADTDiagnostics call reports an empty client_kind so the absence
// is visible (vs a misleading default).
func TestGetADTClientDiagnostics_Unwired(t *testing.T) {
	svr, _, _ := newTestServer()
	resp, err := svr.GetADTClientDiagnostics(authCtx(uuid.New()), &pb.GetADTClientDiagnosticsRequest{Namespace: testNamespace})
	if err != nil {
		t.Fatalf("GetADTClientDiagnostics: %v", err)
	}
	if resp.GetAdtClientKind() != "" {
		t.Fatalf("unwired server must report empty adt_client_kind, got %q", resp.GetAdtClientKind())
	}
}
