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

// TestUnlinkADT_HappyPath_CallsADTThenLocalSoftDelete pins the 2026-05-21
// bug fix: UnlinkADT now propagates to ADT before soft-deleting locally
// so the orphan-flag-on-ADT-side state cannot recur.
func TestUnlinkADT_HappyPath_CallsADTThenLocalSoftDelete(t *testing.T) {
	h := newADTTestServer(t)
	audit := &fakeAuditLogStore{}
	h.svr.WithAuditLogStore(audit)
	linkage := h.linkage.live[testStudioNamespace+"|"+testADTNamespace]

	_, err := h.svr.UnlinkADT(authCtx(uuid.New()), &pb.UnlinkADTRequest{
		Namespace:    testNamespace,
		AdtLinkageId: linkage.ID.String(),
	})
	if err != nil {
		t.Fatalf("UnlinkADT: %v", err)
	}
	if h.mem.IsLinked(testStudioNamespace, testADTNamespace) {
		t.Fatal("ADT-side linkage flag still present; DeleteLinkage must have run")
	}
	if len(h.linkage.softDeletedKeys) != 1 {
		t.Fatalf("local soft-delete count = %d, want 1", len(h.linkage.softDeletedKeys))
	}
	if audit.countAction(repo.ActionADTLinkageDelete) != 1 {
		t.Fatalf("audit %s count = %d, want 1", repo.ActionADTLinkageDelete, audit.countAction(repo.ActionADTLinkageDelete))
	}
}

// TestUnlinkADT_ADT401_StillSoftDeletesLocally pins the orphan-flag
// recovery half: ADT already cleaned (or never had a flag) is benign;
// local soft-delete still proceeds + audit row still emits.
func TestUnlinkADT_ADT401_StillSoftDeletesLocally(t *testing.T) {
	h := newADTTestServer(t)
	audit := &fakeAuditLogStore{}
	h.svr.WithAuditLogStore(audit)
	linkage := h.linkage.live[testStudioNamespace+"|"+testADTNamespace]
	// Drop the ADT-side flag pre-call so MemClient returns ErrLinkageMissing.
	h.mem.ClearLinkage(testStudioNamespace, testADTNamespace)

	_, err := h.svr.UnlinkADT(authCtx(uuid.New()), &pb.UnlinkADTRequest{
		Namespace:    testNamespace,
		AdtLinkageId: linkage.ID.String(),
	})
	if err != nil {
		t.Fatalf("UnlinkADT: %v", err)
	}
	if len(h.linkage.softDeletedKeys) != 1 {
		t.Fatalf("local soft-delete count = %d, want 1 (ADT 401 must not block local soft-delete)", len(h.linkage.softDeletedKeys))
	}
	if audit.countAction(repo.ActionADTLinkageDelete) != 1 {
		t.Fatalf("audit row missing; got %d", audit.countAction(repo.ActionADTLinkageDelete))
	}
}

// TestUnlinkADT_ADTTransient_StillSoftDeletesLocally pins that ADT
// retry-exhausted (5xx loop) does NOT strand the operator — the whole
// point of the best-effort design.
func TestUnlinkADT_ADTTransient_StillSoftDeletesLocally(t *testing.T) {
	h := newADTTestServer(t)
	audit := &fakeAuditLogStore{}
	h.svr.WithAuditLogStore(audit)
	linkage := h.linkage.live[testStudioNamespace+"|"+testADTNamespace]
	// Stage a transient error class: ErrUnavailable mirrors the
	// 5xx-retry-exhausted shape callers see in production.
	h.mem.DeleteLinkageErr = []error{adt.ErrUnavailable}

	_, err := h.svr.UnlinkADT(authCtx(uuid.New()), &pb.UnlinkADTRequest{
		Namespace:    testNamespace,
		AdtLinkageId: linkage.ID.String(),
	})
	if err != nil {
		t.Fatalf("UnlinkADT: %v", err)
	}
	if len(h.linkage.softDeletedKeys) != 1 {
		t.Fatalf("local soft-delete count = %d, want 1 (ADT transient must not block local soft-delete)", len(h.linkage.softDeletedKeys))
	}
	if audit.countAction(repo.ActionADTLinkageDelete) != 1 {
		t.Fatalf("audit row missing; got %d", audit.countAction(repo.ActionADTLinkageDelete))
	}
}

// TestUnlinkADT_LocalSoftDeleteFails_SurfacesInternal pins the failure
// mode for the local-DB outage: ADT side already cleaned (best-effort
// fine) + local soft-delete failure → Internal. We do NOT roll back the
// ADT-side cleanup because eventual consistency on the playtesthub side
// is acceptable — the next call against the (still-live) local row
// would 401 against ADT and surface "re-link required" anyway.
func TestUnlinkADT_LocalSoftDeleteFails_SurfacesInternal(t *testing.T) {
	h := newADTTestServer(t)
	audit := &fakeAuditLogStore{}
	h.svr.WithAuditLogStore(audit)
	linkage := h.linkage.live[testStudioNamespace+"|"+testADTNamespace]
	h.linkage.softDeleteErr = errors.New("db gone")

	_, err := h.svr.UnlinkADT(authCtx(uuid.New()), &pb.UnlinkADTRequest{
		Namespace:    testNamespace,
		AdtLinkageId: linkage.ID.String(),
	})
	requireStatus(t, err, codes.Internal)
	// ADT side ran (best-effort) and the flag is gone.
	if h.mem.IsLinked(testStudioNamespace, testADTNamespace) {
		t.Fatal("expected ADT-side flag cleared even when local soft-delete fails")
	}
	if audit.countAction(repo.ActionADTLinkageDelete) != 0 {
		t.Fatalf("audit row should NOT have been written on Internal failure; got %d", audit.countAction(repo.ActionADTLinkageDelete))
	}
}

// TestUnlinkADT_AlreadySoftDeleted_IsNoOp pins that re-unlink against an
// already-deleted local row stays a no-op — and crucially, does NOT call
// ADT again (a no-op locally is a no-op everywhere).
func TestUnlinkADT_AlreadySoftDeleted_IsNoOp(t *testing.T) {
	h := newADTTestServer(t)
	linkage := h.linkage.live[testStudioNamespace+"|"+testADTNamespace]
	// First unlink: clears the ADT flag + local row.
	if _, err := h.svr.UnlinkADT(authCtx(uuid.New()), &pb.UnlinkADTRequest{
		Namespace:    testNamespace,
		AdtLinkageId: linkage.ID.String(),
	}); err != nil {
		t.Fatalf("first UnlinkADT: %v", err)
	}
	// Re-link on ADT side so we can detect whether the second
	// UnlinkADT incorrectly re-invokes DeleteLinkage.
	h.mem.RecordLinkage(testStudioNamespace, testADTNamespace)
	if _, err := h.svr.UnlinkADT(authCtx(uuid.New()), &pb.UnlinkADTRequest{
		Namespace:    testNamespace,
		AdtLinkageId: linkage.ID.String(),
	}); err != nil {
		t.Fatalf("second UnlinkADT: %v", err)
	}
	if !h.mem.IsLinked(testStudioNamespace, testADTNamespace) {
		t.Fatal("second UnlinkADT against soft-deleted row must not call DeleteLinkage")
	}
}

// TestUnlinkADT_NoADTClient_StillSoftDeletes pins that a service without
// a configured ADT client (the M5.B pre-live-adapter shape) keeps the
// previous behaviour: local soft-delete only.
func TestUnlinkADT_NoADTClient_StillSoftDeletes(t *testing.T) {
	svr, _, _ := newTestServer()
	link := newFakeADTLinkageStore()
	row := link.seedLive(testStudioNamespace, testADTNamespace)
	svr.
		WithADTLinkageStore(link).
		WithStudioNamespaceResolver(func(context.Context) (string, error) {
			return testStudioNamespace, nil
		})

	if _, err := svr.UnlinkADT(authCtx(uuid.New()), &pb.UnlinkADTRequest{
		Namespace:    testNamespace,
		AdtLinkageId: row.ID.String(),
	}); err != nil {
		t.Fatalf("UnlinkADT: %v", err)
	}
	if len(link.softDeletedKeys) != 1 {
		t.Fatalf("local soft-delete count = %d, want 1", len(link.softDeletedKeys))
	}
}
