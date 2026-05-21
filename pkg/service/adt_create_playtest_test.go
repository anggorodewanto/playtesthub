package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	"github.com/anggorodewanto/playtesthub/pkg/adt"
	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// ---------------- fakes ------------------------------------------------------

// fakeADTLinkageStore is the in-memory ADTLinkageStore used by the B5
// CreatePlaytest ADT-branch tests + the EditPlaytest ADT mutability
// tests. Only the surface CreatePlaytest exercises is populated;
// pending-row methods (Insert/ConsumePending) are intentionally
// unimplemented because CreatePlaytest does not call them.
type fakeADTLinkageStore struct {
	live map[string]*repo.ADTLinkage // key = studio + "|" + adtNs

	// Test hooks used by adt_unlink_test.go.
	insertErr       error
	insertedRows    []*repo.ADTLinkage
	softDeleteErr   error
	softDeletedKeys []string
}

func newFakeADTLinkageStore() *fakeADTLinkageStore {
	return &fakeADTLinkageStore{live: map[string]*repo.ADTLinkage{}}
}

func (f *fakeADTLinkageStore) InsertPending(context.Context, *repo.ADTLinkPending) error {
	return errors.New("InsertPending unimplemented in fakeADTLinkageStore")
}
func (f *fakeADTLinkageStore) ConsumePending(context.Context, string, time.Time) (*repo.ADTLinkPending, error) {
	return nil, errors.New("ConsumePending unimplemented in fakeADTLinkageStore")
}
func (f *fakeADTLinkageStore) Insert(_ context.Context, l *repo.ADTLinkage) (*repo.ADTLinkage, error) {
	if f.insertErr != nil {
		return nil, f.insertErr
	}
	row := &repo.ADTLinkage{
		ID:              uuid.New(),
		StudioNamespace: l.StudioNamespace,
		ADTNamespace:    l.ADTNamespace,
		LinkedByUserID:  l.LinkedByUserID,
		LinkedAt:        time.Now(),
	}
	f.live[l.StudioNamespace+"|"+l.ADTNamespace] = row
	f.insertedRows = append(f.insertedRows, row)
	return row, nil
}

func (f *fakeADTLinkageStore) GetLive(_ context.Context, studio, adtNs string) (*repo.ADTLinkage, error) {
	row, ok := f.live[studio+"|"+adtNs]
	if !ok {
		return nil, repo.ErrNotFound
	}
	return row, nil
}

func (f *fakeADTLinkageStore) ListLive(_ context.Context, studio string) ([]*repo.ADTLinkage, error) {
	out := []*repo.ADTLinkage{}
	for _, r := range f.live {
		if r.StudioNamespace == studio {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeADTLinkageStore) GetByID(_ context.Context, studio string, id uuid.UUID) (*repo.ADTLinkage, error) {
	for _, r := range f.live {
		if r.StudioNamespace == studio && r.ID == id {
			return r, nil
		}
	}
	return nil, repo.ErrNotFound
}

func (f *fakeADTLinkageStore) SoftDelete(_ context.Context, studio string, id uuid.UUID) error {
	if f.softDeleteErr != nil {
		return f.softDeleteErr
	}
	for k, r := range f.live {
		if r.StudioNamespace == studio && r.ID == id {
			now := time.Now()
			r.DeletedAt = &now
			f.softDeletedKeys = append(f.softDeletedKeys, k)
			return nil
		}
	}
	return repo.ErrNotFound
}

func (f *fakeADTLinkageStore) seedLive(studio, adtNs string) *repo.ADTLinkage {
	row := &repo.ADTLinkage{
		ID:              uuid.New(),
		StudioNamespace: studio,
		ADTNamespace:    adtNs,
		LinkedByUserID:  uuid.New(),
		LinkedAt:        time.Now(),
	}
	f.live[studio+"|"+adtNs] = row
	return row
}

// ---------------- harness ----------------------------------------------------

const (
	testStudioNamespace = "studio-acme"
	testADTNamespace    = "adt-ns-1"
	testADTGameID       = "game-x"
	testADTBuildID      = "build-001"
)

// adtTestHarness bundles the test server + the stores ADT tests reach
// into. Returning a struct (vs four positional values) keeps the call
// sites short and dodges the dogsled lint on tests that only care about
// `svr`.
type adtTestHarness struct {
	svr     *PlaytesthubServiceServer
	pt      *fakePlaytestStore
	linkage *fakeADTLinkageStore
	mem     *adt.MemClient
}

// newADTTestServer wires the standard test server with a fake linkage
// store, a deterministic studio resolver, and a MemClient pre-loaded with
// the linkage flag + one build.
func newADTTestServer(t *testing.T) *adtTestHarness {
	t.Helper()
	svr, pt, _ := newTestServer()
	link := newFakeADTLinkageStore()
	link.seedLive(testStudioNamespace, testADTNamespace)
	mem := adt.NewMemClient()
	mem.RecordLinkage(testStudioNamespace, testADTNamespace)
	mem.SeedBuilds(testADTNamespace, testADTGameID, []adt.Build{
		{ID: testADTBuildID, Name: "Alpha", Version: "0.1.0", UploadedAt: time.Now()},
	})
	svr.
		WithADTLinkageStore(link).
		WithADTClient(mem).
		WithStudioNamespaceResolver(func(context.Context) (string, error) {
			return testStudioNamespace, nil
		})
	return &adtTestHarness{svr: svr, pt: pt, linkage: link, mem: mem}
}

func validADTCreateRequest(slug string) *pb.CreatePlaytestRequest {
	req := validCreateRequest(slug)
	req.DistributionModel = pb.DistributionModel_DISTRIBUTION_MODEL_ADT
	ns := testADTNamespace
	game := testADTGameID
	build := testADTBuildID
	req.AdtNamespace = &ns
	req.AdtGameId = &game
	req.AdtBuildId = &build
	return req
}

// ---------------- B5 tests ---------------------------------------------------

func TestCreatePlaytest_ADT_HappyPath(t *testing.T) {
	h := newADTTestServer(t)
	resp, err := h.svr.CreatePlaytest(authCtx(uuid.New()), validADTCreateRequest("adt-game"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := resp.GetPlaytest()
	if p.GetDistributionModel() != pb.DistributionModel_DISTRIBUTION_MODEL_ADT {
		t.Errorf("distribution_model = %s, want ADT", p.GetDistributionModel())
	}
	if got := p.GetAdtNamespace(); got != testADTNamespace {
		t.Errorf("adt_namespace = %q, want %q", got, testADTNamespace)
	}
	if got := p.GetAdtGameId(); got != testADTGameID {
		t.Errorf("adt_game_id = %q, want %q", got, testADTGameID)
	}
	if got := p.GetAdtBuildId(); got != testADTBuildID {
		t.Errorf("adt_build_id = %q, want %q", got, testADTBuildID)
	}
}

func TestCreatePlaytest_ADT_MissingIdentifiers_InvalidArgument(t *testing.T) {
	h := newADTTestServer(t)
	req := validADTCreateRequest("adt-missing")
	req.AdtBuildId = nil
	_, err := h.svr.CreatePlaytest(authCtx(uuid.New()), req)
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "adt_namespace, adt_game_id, and adt_build_id are required")
}

func TestCreatePlaytest_ADT_CodePoolField_InvalidArgument(t *testing.T) {
	h := newADTTestServer(t)
	req := validADTCreateRequest("adt-pool")
	qty := int32(10)
	req.InitialCodeQuantity = &qty
	_, err := h.svr.CreatePlaytest(authCtx(uuid.New()), req)
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "no code pool")
}

func TestCreatePlaytest_NonADT_WithADTField_InvalidArgument(t *testing.T) {
	h := newADTTestServer(t)
	req := validCreateRequest("steam-with-adt")
	ns := testADTNamespace
	req.AdtNamespace = &ns
	_, err := h.svr.CreatePlaytest(authCtx(uuid.New()), req)
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "must not be set when distribution_model is not ADT")
}

func TestCreatePlaytest_ADT_BuildNotInNamespace_InvalidArgument(t *testing.T) {
	h := newADTTestServer(t)
	req := validADTCreateRequest("adt-bad-build")
	bogus := "build-does-not-exist"
	req.AdtBuildId = &bogus
	_, err := h.svr.CreatePlaytest(authCtx(uuid.New()), req)
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "is not present under adt_namespace")
}

func TestCreatePlaytest_ADT_NoLinkage_FailedPrecondition(t *testing.T) {
	h := newADTTestServer(t)
	h.linkage.live = map[string]*repo.ADTLinkage{} // drop seeded link
	_, err := h.svr.CreatePlaytest(authCtx(uuid.New()), validADTCreateRequest("adt-unlinked"))
	requireStatus(t, err, codes.FailedPrecondition)
	requireMsgContains(t, err, "link the ADT namespace first")
}

func TestCreatePlaytest_ADT_FallbackURL_NonHTTPS_InvalidArgument(t *testing.T) {
	h := newADTTestServer(t)
	req := validADTCreateRequest("adt-fallback-http")
	bad := "http://example.com/build.zip"
	req.AdtFallbackDownloadUrl = &bad
	_, err := h.svr.CreatePlaytest(authCtx(uuid.New()), req)
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "adt_fallback_download_url must use https")
}

func TestEditPlaytest_ADT_FallbackURL_OnNonADT_InvalidArgument(t *testing.T) {
	h := newADTTestServer(t)
	row := &repo.Playtest{
		ID:                uuid.New(),
		Namespace:         testNamespace,
		Slug:              "steam-play",
		Title:             "T",
		DistributionModel: distModelSteamKeys,
		Status:            statusDraft,
	}
	h.pt.rows = append(h.pt.rows, row)
	req := &pb.EditPlaytestRequest{
		Namespace:   testNamespace,
		PlaytestId:  row.ID.String(),
		Title:       "T",
		Description: "",
		Platforms:   []pb.Platform{pb.Platform_PLATFORM_STEAM},
	}
	bad := "https://example.com/build.zip"
	req.AdtFallbackDownloadUrl = &bad
	_, err := h.svr.EditPlaytest(authCtx(uuid.New()), req)
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "must not be set when distribution_model is not ADT")
}

func TestEditPlaytest_ADT_FallbackURL_Updates(t *testing.T) {
	h := newADTTestServer(t)
	ns := testADTNamespace
	game := testADTGameID
	build := testADTBuildID
	row := &repo.Playtest{
		ID:                uuid.New(),
		Namespace:         testNamespace,
		Slug:              "adt-play",
		Title:             "T",
		DistributionModel: distModelADT,
		Status:            statusDraft,
		ADTNamespace:      &ns,
		ADTGameID:         &game,
		ADTBuildID:        &build,
	}
	h.pt.rows = append(h.pt.rows, row)
	newURL := "https://example.com/builds/new.zip"
	req := &pb.EditPlaytestRequest{
		Namespace:              testNamespace,
		PlaytestId:             row.ID.String(),
		Title:                  "T",
		Description:            "",
		Platforms:              []pb.Platform{pb.Platform_PLATFORM_STEAM},
		AdtFallbackDownloadUrl: &newURL,
	}
	resp, err := h.svr.EditPlaytest(authCtx(uuid.New()), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := resp.GetPlaytest().GetAdtFallbackDownloadUrl(); got != newURL {
		t.Errorf("adt_fallback_download_url = %q, want %q", got, newURL)
	}
}
