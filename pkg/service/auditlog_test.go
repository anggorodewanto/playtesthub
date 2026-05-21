package service

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	"github.com/anggorodewanto/playtesthub/pkg/agsid"
	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// auditTestRig bundles the test server with the audit fake store so
// ListAuditLog handler tests can seed rows directly. Mirrors the
// pattern used by surveyTestRig / ndaTestRig.
type auditTestRig struct {
	svr       *PlaytesthubServiceServer
	playtests *fakePlaytestStore
	audit     *fakeAuditLogStore
}

func withAuditStores(t *testing.T) auditTestRig {
	t.Helper()
	svr, pt, _ := newTestServer()
	audit := &fakeAuditLogStore{}
	svr = svr.WithAuditLogStore(audit)
	return auditTestRig{svr: svr, playtests: pt, audit: audit}
}

// seedAuditRows drops `count` audit rows on the playtest with
// monotonically increasing CreatedAt + the given action. Useful for
// the (createdAt, id) DESC ordering assertions.
func seedAuditRows(rig auditTestRig, playtestID uuid.UUID, count int, action string, actor *uuid.UUID) []*repo.AuditLog {
	out := make([]*repo.AuditLog, 0, count)
	base := time.Now().Add(-time.Duration(count) * time.Minute)
	for i := 0; i < count; i++ {
		pt := playtestID
		row := &repo.AuditLog{
			ID:          uuid.New(),
			Namespace:   testNamespace,
			PlaytestID:  &pt,
			ActorUserID: actor,
			Action:      action,
			Before:      json.RawMessage(`{}`),
			After:       json.RawMessage(`{"i":` + strconv.Itoa(i) + `}`),
			CreatedAt:   base.Add(time.Duration(i) * time.Minute),
		}
		rig.audit.rows = append(rig.audit.rows, row)
		out = append(out, row)
	}
	return out
}

func TestListAuditLog_HappyPath_NewestFirst(t *testing.T) {
	rig := withAuditStores(t)
	pt := openPlaytest("audit-list")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	seedAuditRows(rig, pt.ID, 3, "playtest.create", nil)

	resp, err := rig.svr.ListAuditLog(authCtx(uuid.New()), &pb.ListAuditLogRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
	})
	if err != nil {
		t.Fatalf("ListAuditLog: %v", err)
	}
	if got := len(resp.GetEntries()); got != 3 {
		t.Fatalf("entries = %d, want 3", got)
	}
	prev := time.Now().Add(time.Hour)
	for i, e := range resp.GetEntries() {
		got := e.GetCreatedAt().AsTime()
		if got.After(prev) {
			t.Errorf("entries[%d].createdAt %s out of order vs prev %s", i, got, prev)
		}
		prev = got
		if e.GetAction() != "playtest.create" {
			t.Errorf("entries[%d].action = %q, want %q", i, e.GetAction(), "playtest.create")
		}
		if e.GetBeforeJson() == "" || e.GetAfterJson() == "" {
			t.Errorf("entries[%d] before/after should not stringify to empty", i)
		}
	}
}

func TestListAuditLog_PaginatesAcrossPages(t *testing.T) {
	rig := withAuditStores(t)
	pt := openPlaytest("audit-page")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	seedAuditRows(rig, pt.ID, 5, "applicant.approve", nil)

	first, err := rig.svr.ListAuditLog(authCtx(uuid.New()), &pb.ListAuditLogRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		PageSize:   2,
	})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first.GetEntries()) != 2 {
		t.Fatalf("first page len = %d, want 2", len(first.GetEntries()))
	}
	if first.GetNextPageToken() == "" {
		t.Fatal("expected non-empty next_page_token after first page")
	}

	second, err := rig.svr.ListAuditLog(authCtx(uuid.New()), &pb.ListAuditLogRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		PageSize:   2,
		PageToken:  first.GetNextPageToken(),
	})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.GetEntries()) != 2 {
		t.Errorf("second page len = %d, want 2", len(second.GetEntries()))
	}

	seen := make(map[string]struct{})
	for _, e := range first.GetEntries() {
		seen[e.GetId()] = struct{}{}
	}
	for _, e := range second.GetEntries() {
		if _, dup := seen[e.GetId()]; dup {
			t.Errorf("entry %s duplicated across pages", e.GetId())
		}
	}
}

func TestListAuditLog_ActorFilterSystem(t *testing.T) {
	rig := withAuditStores(t)
	pt := openPlaytest("audit-system")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	human := uuid.New()
	seedAuditRows(rig, pt.ID, 2, "applicant.approve", &human)
	seedAuditRows(rig, pt.ID, 3, "code.grant_orphaned", nil)

	resp, err := rig.svr.ListAuditLog(authCtx(uuid.New()), &pb.ListAuditLogRequest{
		Namespace:   testNamespace,
		PlaytestId:  pt.ID.String(),
		ActorFilter: "system",
	})
	if err != nil {
		t.Fatalf("ListAuditLog: %v", err)
	}
	if got := len(resp.GetEntries()); got != 3 {
		t.Fatalf("entries = %d, want 3", got)
	}
	for i, e := range resp.GetEntries() {
		if e.ActorUserId != nil {
			t.Errorf("entries[%d].actor_user_id = %v, want nil for actor=system", i, e.ActorUserId)
		}
	}
}

func TestListAuditLog_ActorFilterUUID(t *testing.T) {
	rig := withAuditStores(t)
	pt := openPlaytest("audit-actor")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	target := uuid.New()
	other := uuid.New()
	seedAuditRows(rig, pt.ID, 2, "playtest.edit", &target)
	seedAuditRows(rig, pt.ID, 4, "playtest.edit", &other)

	resp, err := rig.svr.ListAuditLog(authCtx(uuid.New()), &pb.ListAuditLogRequest{
		Namespace:   testNamespace,
		PlaytestId:  pt.ID.String(),
		ActorFilter: target.String(),
	})
	if err != nil {
		t.Fatalf("ListAuditLog: %v", err)
	}
	if got := len(resp.GetEntries()); got != 2 {
		t.Fatalf("entries = %d, want 2", got)
	}
	for i, e := range resp.GetEntries() {
		if want := agsid.Format(target); e.GetActorUserId() != want {
			t.Errorf("entries[%d].actor_user_id = %s, want %s", i, e.GetActorUserId(), want)
		}
	}
}

func TestListAuditLog_ActionFilter(t *testing.T) {
	rig := withAuditStores(t)
	pt := openPlaytest("audit-action")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	seedAuditRows(rig, pt.ID, 2, "playtest.create", nil)
	seedAuditRows(rig, pt.ID, 5, "applicant.approve", nil)

	resp, err := rig.svr.ListAuditLog(authCtx(uuid.New()), &pb.ListAuditLogRequest{
		Namespace:    testNamespace,
		PlaytestId:   pt.ID.String(),
		ActionFilter: "applicant.approve",
	})
	if err != nil {
		t.Fatalf("ListAuditLog: %v", err)
	}
	if got := len(resp.GetEntries()); got != 5 {
		t.Fatalf("entries = %d, want 5", got)
	}
	for i, e := range resp.GetEntries() {
		if e.GetAction() != "applicant.approve" {
			t.Errorf("entries[%d].action = %q, want applicant.approve", i, e.GetAction())
		}
	}
}

func TestListAuditLog_BadPageToken_InvalidArgument(t *testing.T) {
	rig := withAuditStores(t)
	pt := openPlaytest("audit-bad-tok")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	_, err := rig.svr.ListAuditLog(authCtx(uuid.New()), &pb.ListAuditLogRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		PageToken:  "not-a-cursor",
	})
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "page_token")
}

func TestListAuditLog_BadActorFilter_InvalidArgument(t *testing.T) {
	rig := withAuditStores(t)
	pt := openPlaytest("audit-bad-actor")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	_, err := rig.svr.ListAuditLog(authCtx(uuid.New()), &pb.ListAuditLogRequest{
		Namespace:   testNamespace,
		PlaytestId:  pt.ID.String(),
		ActorFilter: "not-a-uuid",
	})
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "actor_filter")
}

func TestListAuditLog_SoftDeletedPlaytest_NotFound(t *testing.T) {
	rig := withAuditStores(t)
	pt := openPlaytest("audit-gone")
	now := time.Now()
	pt.DeletedAt = &now
	rig.playtests.rows = append(rig.playtests.rows, pt)

	_, err := rig.svr.ListAuditLog(authCtx(uuid.New()), &pb.ListAuditLogRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
	})
	requireStatus(t, err, codes.NotFound)
}

func TestListAuditLog_BadPlaytestID_InvalidArgument(t *testing.T) {
	rig := withAuditStores(t)
	_, err := rig.svr.ListAuditLog(authCtx(uuid.New()), &pb.ListAuditLogRequest{
		Namespace:  testNamespace,
		PlaytestId: "not-a-uuid",
	})
	requireStatus(t, err, codes.InvalidArgument)
}

func TestListAuditLog_Unauthenticated(t *testing.T) {
	rig := withAuditStores(t)
	_, err := rig.svr.ListAuditLog(context.Background(), &pb.ListAuditLogRequest{
		Namespace:  testNamespace,
		PlaytestId: uuid.New().String(),
	})
	requireStatus(t, err, codes.Unauthenticated)
}

func TestListAuditLog_NoStore_Internal(t *testing.T) {
	svr, pt, _ := newTestServer()
	row := openPlaytest("audit-nostore")
	pt.rows = append(pt.rows, row)

	_, err := svr.ListAuditLog(authCtx(uuid.New()), &pb.ListAuditLogRequest{
		Namespace:  testNamespace,
		PlaytestId: row.ID.String(),
	})
	requireStatus(t, err, codes.Internal)
}

// TestListAuditLog_ActorFilterSystemAndAction composes both filters to
// catch a regression where one filter accidentally clobbers the other
// (e.g. a missing AND in the SQL builder). Two payload variants per
// action keep the assertion narrow without needing to re-spell the
// JSONB shape.
func TestListAuditLog_ActorFilterSystemAndAction(t *testing.T) {
	rig := withAuditStores(t)
	pt := openPlaytest("audit-compose")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	human := uuid.New()
	seedAuditRows(rig, pt.ID, 3, "code.grant_orphaned", nil)
	seedAuditRows(rig, pt.ID, 2, "applicant.approve", &human)
	seedAuditRows(rig, pt.ID, 1, "playtest.create", nil)

	resp, err := rig.svr.ListAuditLog(authCtx(uuid.New()), &pb.ListAuditLogRequest{
		Namespace:    testNamespace,
		PlaytestId:   pt.ID.String(),
		ActorFilter:  "system",
		ActionFilter: "code.grant_orphaned",
	})
	if err != nil {
		t.Fatalf("ListAuditLog: %v", err)
	}
	if got := len(resp.GetEntries()); got != 3 {
		t.Fatalf("entries = %d, want 3", got)
	}
	for i, e := range resp.GetEntries() {
		if e.ActorUserId != nil {
			t.Errorf("entries[%d].actor_user_id = %v, want nil", i, e.ActorUserId)
		}
		if e.GetAction() != "code.grant_orphaned" {
			t.Errorf("entries[%d].action = %q, want code.grant_orphaned", i, e.GetAction())
		}
	}
}

// TestListAuditLog_BeforeAfter_OpaqueJSON guards the wire shape contract:
// the before/after JSONB columns ride out as opaque JSON strings the
// client renders the diff over, NOT as protobuf-typed structs.
func TestListAuditLog_BeforeAfter_OpaqueJSON(t *testing.T) {
	rig := withAuditStores(t)
	pt := openPlaytest("audit-shape")
	rig.playtests.rows = append(rig.playtests.rows, pt)
	ptID := pt.ID
	row := &repo.AuditLog{
		ID:         uuid.New(),
		Namespace:  testNamespace,
		PlaytestID: &ptID,
		Action:     "playtest.edit",
		Before:     json.RawMessage(`{"title":"old"}`),
		After:      json.RawMessage(`{"title":"new"}`),
		CreatedAt:  time.Now(),
	}
	rig.audit.rows = append(rig.audit.rows, row)

	resp, err := rig.svr.ListAuditLog(authCtx(uuid.New()), &pb.ListAuditLogRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
	})
	if err != nil {
		t.Fatalf("ListAuditLog: %v", err)
	}
	if got := len(resp.GetEntries()); got != 1 {
		t.Fatalf("entries = %d, want 1", got)
	}
	got := resp.GetEntries()[0]
	if !strings.Contains(got.GetBeforeJson(), `"title":"old"`) {
		t.Errorf("before_json = %q, want JSON containing title=old", got.GetBeforeJson())
	}
	if !strings.Contains(got.GetAfterJson(), `"title":"new"`) {
		t.Errorf("after_json = %q, want JSON containing title=new", got.GetAfterJson())
	}
}
