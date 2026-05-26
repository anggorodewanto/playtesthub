package service

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	"github.com/anggorodewanto/playtesthub/pkg/adt"
	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

const (
	testADTGameID2  = "game-y"
	testADTBuildID2 = "build-002"
)

// changeBuildHarness extends the standard ADT harness with an audit
// store (ChangeADTBuild writes playtest.adt_build_change) and a second
// game/build under the same linkage so the happy path proves a real
// mutation, not a re-send of the existing ids.
func newChangeBuildHarness(t *testing.T) (*adtTestHarness, *fakeAuditLogStore) {
	t.Helper()
	h := newADTTestServer(t)
	audit := &fakeAuditLogStore{}
	h.svr.WithAuditLogStore(audit)
	h.mem.SeedBuilds(testADTNamespace, testADTGameID2, []adt.Build{
		{ID: testADTBuildID2, Name: "Beta", Version: "0.2.0", UploadedAt: time.Now()},
	})
	return h, audit
}

// seedADTPlaytestRow drops a live ADT playtest pointed at game-x/build-001.
func seedADTPlaytestRow(h *adtTestHarness, slug string) *repo.Playtest {
	ns := testADTNamespace
	game := testADTGameID
	build := testADTBuildID
	row := &repo.Playtest{
		ID:                uuid.New(),
		Namespace:         testNamespace,
		Slug:              slug,
		Title:             "T",
		DistributionModel: distModelADT,
		Status:            statusDraft,
		ADTNamespace:      &ns,
		ADTGameID:         &game,
		ADTBuildID:        &build,
	}
	h.pt.rows = append(h.pt.rows, row)
	return row
}

func TestChangeADTBuild_HappyPath(t *testing.T) {
	h, audit := newChangeBuildHarness(t)
	row := seedADTPlaytestRow(h, "adt-change")

	resp, err := h.svr.ChangeADTBuild(authCtx(uuid.New()), &pb.ChangeADTBuildRequest{
		Namespace:  testNamespace,
		PlaytestId: row.ID.String(),
		AdtGameId:  testADTGameID2,
		AdtBuildId: testADTBuildID2,
	})
	if err != nil {
		t.Fatalf("ChangeADTBuild: %v", err)
	}
	p := resp.GetPlaytest()
	if got := p.GetAdtGameId(); got != testADTGameID2 {
		t.Errorf("adt_game_id = %q, want %q", got, testADTGameID2)
	}
	if got := p.GetAdtBuildId(); got != testADTBuildID2 {
		t.Errorf("adt_build_id = %q, want %q", got, testADTBuildID2)
	}
	// adt_namespace is immutable across a build change.
	if got := p.GetAdtNamespace(); got != testADTNamespace {
		t.Errorf("adt_namespace = %q, want unchanged %q", got, testADTNamespace)
	}
	if got := audit.countAction(repo.ActionPlaytestADTBuildChange); got != 1 {
		t.Fatalf("audit %s count = %d, want 1", repo.ActionPlaytestADTBuildChange, got)
	}
}

func TestChangeADTBuild_BuildNotInNamespace_InvalidArgument(t *testing.T) {
	h, _ := newChangeBuildHarness(t)
	row := seedADTPlaytestRow(h, "adt-change-badbuild")

	_, err := h.svr.ChangeADTBuild(authCtx(uuid.New()), &pb.ChangeADTBuildRequest{
		Namespace:  testNamespace,
		PlaytestId: row.ID.String(),
		AdtGameId:  testADTGameID2,
		AdtBuildId: "build-does-not-exist",
	})
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "is not present under adt_namespace")
}

func TestChangeADTBuild_MissingFields_InvalidArgument(t *testing.T) {
	h, _ := newChangeBuildHarness(t)
	row := seedADTPlaytestRow(h, "adt-change-missing")

	_, err := h.svr.ChangeADTBuild(authCtx(uuid.New()), &pb.ChangeADTBuildRequest{
		Namespace:  testNamespace,
		PlaytestId: row.ID.String(),
		AdtGameId:  testADTGameID2,
		AdtBuildId: "",
	})
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "adt_game_id and adt_build_id are required")
}

func TestChangeADTBuild_NonADTPlaytest_FailedPrecondition(t *testing.T) {
	h, _ := newChangeBuildHarness(t)
	row := &repo.Playtest{
		ID:                uuid.New(),
		Namespace:         testNamespace,
		Slug:              "steam-change",
		Title:             "T",
		DistributionModel: distModelSteamKeys,
		Status:            statusDraft,
	}
	h.pt.rows = append(h.pt.rows, row)

	_, err := h.svr.ChangeADTBuild(authCtx(uuid.New()), &pb.ChangeADTBuildRequest{
		Namespace:  testNamespace,
		PlaytestId: row.ID.String(),
		AdtGameId:  testADTGameID2,
		AdtBuildId: testADTBuildID2,
	})
	requireStatus(t, err, codes.FailedPrecondition)
	requireMsgContains(t, err, "ADT distribution")
}

func TestChangeADTBuild_PlaytestNotFound_NotFound(t *testing.T) {
	h, _ := newChangeBuildHarness(t)

	_, err := h.svr.ChangeADTBuild(authCtx(uuid.New()), &pb.ChangeADTBuildRequest{
		Namespace:  testNamespace,
		PlaytestId: uuid.New().String(),
		AdtGameId:  testADTGameID2,
		AdtBuildId: testADTBuildID2,
	})
	requireStatus(t, err, codes.NotFound)
}

func TestChangeADTBuild_LinkageMissing_FailedPrecondition(t *testing.T) {
	h, _ := newChangeBuildHarness(t)
	row := seedADTPlaytestRow(h, "adt-change-unlinked")
	h.linkage.live = map[string]*repo.ADTLinkage{} // drop the seeded linkage

	_, err := h.svr.ChangeADTBuild(authCtx(uuid.New()), &pb.ChangeADTBuildRequest{
		Namespace:  testNamespace,
		PlaytestId: row.ID.String(),
		AdtGameId:  testADTGameID2,
		AdtBuildId: testADTBuildID2,
	})
	requireStatus(t, err, codes.FailedPrecondition)
}
