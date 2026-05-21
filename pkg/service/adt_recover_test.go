package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	"github.com/anggorodewanto/playtesthub/pkg/adt"
	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// TestRecoverADTLinkage_HappyPath_AdoptsOrphanFlag pins the 2026-05-21
// recovery surface: ADT carries a linkage flag but no local row exists
// → RecoverADTLinkage probes ADT, confirms the orphan, inserts the
// local row, writes the audit row.
func TestRecoverADTLinkage_HappyPath_AdoptsOrphanFlag(t *testing.T) {
	svr, _, _ := newTestServer()
	link := newFakeADTLinkageStore()
	mem := adt.NewMemClient()
	// Orphan-flag state: ADT side flagged, local store empty.
	mem.RecordLinkage(testStudioNamespace, testADTNamespace)
	mem.SeedGames(testStudioNamespace, testADTNamespace, []adt.Game{{ID: "g1", Name: "g1"}})
	audit := &fakeAuditLogStore{}
	svr.
		WithADTLinkageStore(link).
		WithADTClient(mem).
		WithAuditLogStore(audit).
		WithStudioNamespaceResolver(func(context.Context) (string, error) {
			return testStudioNamespace, nil
		})

	resp, err := svr.RecoverADTLinkage(authCtx(uuid.New()), &pb.RecoverADTLinkageRequest{
		Namespace:    testNamespace,
		AdtNamespace: testADTNamespace,
	})
	if err != nil {
		t.Fatalf("RecoverADTLinkage: %v", err)
	}
	if resp.GetLinkage() == nil {
		t.Fatal("response linkage is nil")
	}
	if resp.GetLinkage().GetAdtNamespace() != testADTNamespace {
		t.Errorf("adt_namespace = %q", resp.GetLinkage().GetAdtNamespace())
	}
	if len(link.insertedRows) != 1 {
		t.Fatalf("inserted rows = %d, want 1", len(link.insertedRows))
	}
	if got := audit.countAction(repo.ActionADTLinkageRecover); got != 1 {
		t.Fatalf("audit %s count = %d, want 1", repo.ActionADTLinkageRecover, got)
	}
}

// TestRecoverADTLinkage_LinkageAlreadyExists pins the AlreadyExists
// byte-exact contract — the operator already has a live local row;
// they should be using ListADTLinkages instead.
func TestRecoverADTLinkage_LinkageAlreadyExists(t *testing.T) {
	h := newADTTestServer(t)
	_, err := h.svr.RecoverADTLinkage(authCtx(uuid.New()), &pb.RecoverADTLinkageRequest{
		Namespace:    testNamespace,
		AdtNamespace: testADTNamespace,
	})
	requireStatus(t, err, codes.AlreadyExists)
	requireMsgContains(t, err, "adt linkage already exists for that namespace")
}

// TestRecoverADTLinkage_NoADTFlag_FailedPrecondition pins the
// byte-exact contract for "ADT says nothing exists for this pair" —
// the operator should be using StartADTLink instead.
func TestRecoverADTLinkage_NoADTFlag_FailedPrecondition(t *testing.T) {
	svr, _, _ := newTestServer()
	link := newFakeADTLinkageStore()
	mem := adt.NewMemClient() // no RecordLinkage call → ADT side returns ErrLinkageMissing
	svr.
		WithADTLinkageStore(link).
		WithADTClient(mem).
		WithStudioNamespaceResolver(func(context.Context) (string, error) {
			return testStudioNamespace, nil
		})

	_, err := svr.RecoverADTLinkage(authCtx(uuid.New()), &pb.RecoverADTLinkageRequest{
		Namespace:    testNamespace,
		AdtNamespace: testADTNamespace,
	})
	requireStatus(t, err, codes.FailedPrecondition)
	requireMsgContains(t, err, "no ADT-side linkage found for that namespace; use StartADTLink to create one")
}

// TestRecoverADTLinkage_ADTTransient_Unavailable pins that ADT-side
// transient errors (5xx-exhausted, 429) surface as gRPC Unavailable so
// the admin UI can render a retry affordance instead of telling the
// operator the namespace is bad.
func TestRecoverADTLinkage_ADTTransient_Unavailable(t *testing.T) {
	svr, _, _ := newTestServer()
	link := newFakeADTLinkageStore()
	mem := adt.NewMemClient()
	mem.RecordLinkage(testStudioNamespace, testADTNamespace)
	mem.ListGamesErr = []error{errors.New("boom")}
	svr.
		WithADTLinkageStore(link).
		WithADTClient(mem).
		WithStudioNamespaceResolver(func(context.Context) (string, error) {
			return testStudioNamespace, nil
		})

	_, err := svr.RecoverADTLinkage(authCtx(uuid.New()), &pb.RecoverADTLinkageRequest{
		Namespace:    testNamespace,
		AdtNamespace: testADTNamespace,
	})
	requireStatus(t, err, codes.Unavailable)
}

func TestRecoverADTLinkage_MissingADTNamespace_InvalidArgument(t *testing.T) {
	h := newADTTestServer(t)
	_, err := h.svr.RecoverADTLinkage(authCtx(uuid.New()), &pb.RecoverADTLinkageRequest{
		Namespace:    testNamespace,
		AdtNamespace: "",
	})
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "adt_namespace is required")
}

func TestRecoverADTLinkage_RequireActor(t *testing.T) {
	h := newADTTestServer(t)
	_, err := h.svr.RecoverADTLinkage(context.Background(), &pb.RecoverADTLinkageRequest{
		Namespace:    testNamespace,
		AdtNamespace: "x",
	})
	requireStatus(t, err, codes.Unauthenticated)
}

// TestRecoverADTLinkage_InsertUniqueViolation_AlreadyExists pins the
// race-loser contract: another caller raced us between GetLive +
// Insert. Surface AlreadyExists with the same byte-exact message so
// retry semantics are uniform.
func TestRecoverADTLinkage_InsertUniqueViolation_AlreadyExists(t *testing.T) {
	svr, _, _ := newTestServer()
	link := newFakeADTLinkageStore()
	link.insertErr = repo.ErrUniqueViolation
	mem := adt.NewMemClient()
	mem.RecordLinkage(testStudioNamespace, testADTNamespace)
	svr.
		WithADTLinkageStore(link).
		WithADTClient(mem).
		WithStudioNamespaceResolver(func(context.Context) (string, error) {
			return testStudioNamespace, nil
		})

	_, err := svr.RecoverADTLinkage(authCtx(uuid.New()), &pb.RecoverADTLinkageRequest{
		Namespace:    testNamespace,
		AdtNamespace: testADTNamespace,
	})
	requireStatus(t, err, codes.AlreadyExists)
	requireMsgContains(t, err, "adt linkage already exists for that namespace")
}
