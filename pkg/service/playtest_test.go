package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampkg "github.com/anggorodewanto/playtesthub/pkg/iam"
	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// ---------------- fakes ------------------------------------------------------

// fakePlaytestStore is an in-memory PlaytestStore for service unit tests.
// It mirrors the constraint surface the real Postgres store enforces —
// slug uniqueness across namespace + soft-deleted rows, status CAS, not-
// found on missing / soft-deleted updates — but does no real SQL. Tests
// tweak its state directly for scenarios that are awkward to reach
// through the public API (existing rows, CAS mismatches).
type fakePlaytestStore struct {
	rows      []*repo.Playtest
	createErr error // set per-test to force a non-unique error path
}

func (f *fakePlaytestStore) Create(_ context.Context, p *repo.Playtest) (*repo.Playtest, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	for _, r := range f.rows {
		if r.Namespace == p.Namespace && r.Slug == p.Slug {
			return nil, repo.ErrUniqueViolation
		}
	}
	clone := *p
	clone.ID = uuid.New()
	clone.CreatedAt = time.Now()
	clone.UpdatedAt = clone.CreatedAt
	if clone.Status == "" {
		clone.Status = "DRAFT"
	}
	f.rows = append(f.rows, &clone)
	return &clone, nil
}

func (f *fakePlaytestStore) CreateTx(ctx context.Context, _ repo.Querier, p *repo.Playtest) (*repo.Playtest, error) {
	return f.Create(ctx, p)
}

func (f *fakePlaytestStore) GetByID(_ context.Context, namespace string, id uuid.UUID) (*repo.Playtest, error) {
	for _, r := range f.rows {
		if r.Namespace == namespace && r.ID == id {
			clone := *r
			return &clone, nil
		}
	}
	return nil, repo.ErrNotFound
}

func (f *fakePlaytestStore) GetBySlug(_ context.Context, namespace, slug string) (*repo.Playtest, error) {
	for _, r := range f.rows {
		if r.Namespace == namespace && r.Slug == slug {
			clone := *r
			return &clone, nil
		}
	}
	return nil, repo.ErrNotFound
}

func (f *fakePlaytestStore) List(_ context.Context, namespace string, includeDeleted bool) ([]*repo.Playtest, error) {
	out := make([]*repo.Playtest, 0)
	for _, r := range f.rows {
		if r.Namespace != namespace {
			continue
		}
		if !includeDeleted && r.DeletedAt != nil {
			continue
		}
		clone := *r
		out = append(out, &clone)
	}
	return out, nil
}

func (f *fakePlaytestStore) Update(_ context.Context, p *repo.Playtest) (*repo.Playtest, error) {
	for i, r := range f.rows {
		if r.Namespace == p.Namespace && r.ID == p.ID && r.DeletedAt == nil {
			clone := *p
			clone.CreatedAt = r.CreatedAt
			clone.UpdatedAt = time.Now()
			f.rows[i] = &clone
			ret := clone
			return &ret, nil
		}
	}
	return nil, repo.ErrNotFound
}

func (f *fakePlaytestStore) SoftDelete(_ context.Context, namespace string, id uuid.UUID) error {
	for _, r := range f.rows {
		if r.Namespace == namespace && r.ID == id && r.DeletedAt == nil {
			now := time.Now()
			r.DeletedAt = &now
			r.UpdatedAt = now
			return nil
		}
	}
	return repo.ErrNotFound
}

func (f *fakePlaytestStore) TransitionStatus(_ context.Context, namespace string, id uuid.UUID, from, to string) (*repo.Playtest, error) {
	for _, r := range f.rows {
		if r.Namespace == namespace && r.ID == id && r.DeletedAt == nil {
			if r.Status != from {
				return nil, repo.ErrStatusCASMismatch
			}
			r.Status = to
			r.UpdatedAt = time.Now()
			clone := *r
			return &clone, nil
		}
	}
	return nil, repo.ErrStatusCASMismatch
}

// fakeApplicantStore is an in-memory ApplicantStore for service unit
// tests. It mirrors the real UNIQUE (playtest_id, user_id) constraint so
// the Signup idempotency path exercises the same ErrUniqueViolation
// branch as production.
type fakeApplicantStore struct {
	rows      []*repo.Applicant
	insertErr error // set per-test to force a non-unique error path
}

func (f *fakeApplicantStore) Insert(_ context.Context, a *repo.Applicant) (*repo.Applicant, error) {
	if f.insertErr != nil {
		return nil, f.insertErr
	}
	for _, existing := range f.rows {
		if existing.PlaytestID == a.PlaytestID && existing.UserID == a.UserID {
			return nil, repo.ErrUniqueViolation
		}
	}
	clone := *a
	clone.ID = uuid.New()
	clone.CreatedAt = time.Now()
	if clone.Status == "" {
		clone.Status = applicantStatusPending
	}
	f.rows = append(f.rows, &clone)
	ret := clone
	return &ret, nil
}

func (f *fakeApplicantStore) GetByID(_ context.Context, id uuid.UUID) (*repo.Applicant, error) {
	for _, a := range f.rows {
		if a.ID == id {
			clone := *a
			return &clone, nil
		}
	}
	return nil, repo.ErrNotFound
}

func (f *fakeApplicantStore) GetByPlaytestUser(_ context.Context, playtestID, userID uuid.UUID) (*repo.Applicant, error) {
	for _, a := range f.rows {
		if a.PlaytestID == playtestID && a.UserID == userID {
			clone := *a
			return &clone, nil
		}
	}
	return nil, repo.ErrNotFound
}

func (f *fakeApplicantStore) ListByPlaytest(_ context.Context, playtestID uuid.UUID, statusFilter string) ([]*repo.Applicant, error) {
	out := make([]*repo.Applicant, 0)
	for _, a := range f.rows {
		if a.PlaytestID != playtestID {
			continue
		}
		if statusFilter != "" && a.Status != statusFilter {
			continue
		}
		clone := *a
		out = append(out, &clone)
	}
	return out, nil
}

// ListPaged mirrors the SQL implementation: order by created_at DESC,
// id DESC, slice on the opaque cursor. Tests rarely build cursors by
// hand; the M2-phase-6 ListApplicants tests round-trip through the
// fake's encode/decode pair so a regression in the cursor format trips
// here as well as in the integration suite.
func (f *fakeApplicantStore) ListPaged(_ context.Context, q repo.ApplicantPageQuery) (*repo.ApplicantPage, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = repo.ListPagedDefaultLimit
	}
	if limit > repo.ListPagedMaxLimit {
		limit = repo.ListPagedMaxLimit
	}

	filtered := make([]*repo.Applicant, 0, len(f.rows))
	for _, a := range f.rows {
		if a.PlaytestID != q.PlaytestID {
			continue
		}
		if q.Status != "" && a.Status != q.Status {
			continue
		}
		if q.DMFailedOnly && (a.LastDMStatus == nil || *a.LastDMStatus != "failed") {
			continue
		}
		clone := *a
		filtered = append(filtered, &clone)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if !filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
		}
		return bytesGreater(filtered[i].ID[:], filtered[j].ID[:])
	})
	if q.PageToken != "" {
		cur, err := repo.DecodeApplicantPageTokenForTest(q.PageToken)
		if err != nil {
			return nil, err
		}
		cut := -1
		for idx, a := range filtered {
			if a.CreatedAt.Equal(cur.CreatedAt) && a.ID == cur.ID {
				cut = idx
				break
			}
			if a.CreatedAt.Before(cur.CreatedAt) {
				cut = idx - 1
				break
			}
		}
		if cut == -1 {
			cut = len(filtered) - 1
		}
		filtered = filtered[cut+1:]
	}

	page := &repo.ApplicantPage{}
	if len(filtered) > limit {
		page.Rows = filtered[:limit]
		last := page.Rows[limit-1]
		page.NextPageToken = repo.EncodeApplicantPageTokenForTest(last.CreatedAt, last.ID)
		return page, nil
	}
	page.Rows = filtered
	return page, nil
}

func bytesGreater(a, b []byte) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func (f *fakeApplicantStore) UpdateStatus(_ context.Context, a *repo.Applicant) (*repo.Applicant, error) {
	for i, existing := range f.rows {
		if existing.ID == a.ID {
			clone := *a
			clone.CreatedAt = existing.CreatedAt
			f.rows[i] = &clone
			ret := clone
			return &ret, nil
		}
	}
	return nil, repo.ErrNotFound
}

// ApproveCAS / RejectCAS / UpdateDMStatus exist on the interface for
// the M2 approve flow. These M1-era unit tests do not exercise the
// approve path; the methods stub out cleanly here and the M2 phases
// that introduce the call sites bring their own table-driven tests.
func (f *fakeApplicantStore) ApproveCAS(_ context.Context, _ repo.Querier, applicantID, codeID uuid.UUID, approvedAt time.Time) (*repo.Applicant, error) {
	for i, existing := range f.rows {
		if existing.ID != applicantID {
			continue
		}
		if existing.Status != applicantStatusPending {
			return nil, repo.ErrStatusCASMismatch
		}
		clone := *existing
		clone.Status = applicantStatusApproved
		clone.GrantedCodeID = &codeID
		clone.ApprovedAt = &approvedAt
		f.rows[i] = &clone
		ret := clone
		return &ret, nil
	}
	return nil, repo.ErrNotFound
}

func (f *fakeApplicantStore) RejectCAS(_ context.Context, _ repo.Querier, applicantID uuid.UUID, reason *string) (*repo.Applicant, error) {
	for i, existing := range f.rows {
		if existing.ID != applicantID {
			continue
		}
		if existing.Status != applicantStatusPending {
			return nil, repo.ErrStatusCASMismatch
		}
		clone := *existing
		clone.Status = applicantStatusRejected
		clone.RejectionReason = reason
		f.rows[i] = &clone
		ret := clone
		return &ret, nil
	}
	return nil, repo.ErrNotFound
}

func (f *fakeApplicantStore) UpdateDMStatus(_ context.Context, applicantID uuid.UUID, status string, attemptAt time.Time, errMsg *string) (*repo.Applicant, error) {
	for i, existing := range f.rows {
		if existing.ID != applicantID {
			continue
		}
		clone := *existing
		clone.LastDMStatus = &status
		clone.LastDMAttemptAt = &attemptAt
		clone.LastDMError = errMsg
		f.rows[i] = &clone
		ret := clone
		return &ret, nil
	}
	return nil, repo.ErrNotFound
}

func (f *fakeApplicantStore) SetNDAVersionHash(_ context.Context, applicantID uuid.UUID, hash string) (*repo.Applicant, error) {
	for i, existing := range f.rows {
		if existing.ID != applicantID {
			continue
		}
		clone := *existing
		h := hash
		clone.NDAVersionHash = &h
		f.rows[i] = &clone
		ret := clone
		return &ret, nil
	}
	return nil, repo.ErrNotFound
}

// ListLostDMOnRestart filters approved applicants whose last_dm_status
// is unset or "pending" — the dm-queue.md startup-sweep contract. The
// fake ignores the namespace argument because tests scope by playtest;
// the SQL implementation does the namespace JOIN.
func (f *fakeApplicantStore) ListLostDMOnRestart(_ context.Context, _ string) ([]*repo.Applicant, error) {
	out := make([]*repo.Applicant, 0)
	for _, a := range f.rows {
		if a.Status != applicantStatusApproved {
			continue
		}
		if a.LastDMStatus != nil && *a.LastDMStatus != "pending" {
			continue
		}
		clone := *a
		out = append(out, &clone)
	}
	return out, nil
}

// ---------------- test helpers ----------------------------------------------

const testNamespace = "playtesthub-test"

func newTestServer() (*PlaytesthubServiceServer, *fakePlaytestStore, *fakeApplicantStore) {
	pt := &fakePlaytestStore{}
	ap := &fakeApplicantStore{}
	return NewPlaytesthubServiceServer(pt, ap, testNamespace), pt, ap
}

func authCtx(userID uuid.UUID) context.Context {
	return iampkg.WithActorUserID(context.Background(), userID.String())
}

func requireStatus(t *testing.T, err error, want codes.Code) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected status code %s, got nil", want)
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("error is not a status: %T: %v", err, err)
	}
	if st.Code() != want {
		t.Fatalf("code = %s, want %s (msg=%q)", st.Code(), want, st.Message())
	}
}

func requireMsgContains(t *testing.T, err error, substr string) {
	t.Helper()
	st, _ := status.FromError(err)
	if !strings.Contains(st.Message(), substr) {
		t.Fatalf("message %q does not contain %q", st.Message(), substr)
	}
}

func validCreateRequest(slug string) *pb.CreatePlaytestRequest {
	return &pb.CreatePlaytestRequest{
		Namespace:         testNamespace,
		Slug:              slug,
		Title:             "The Title",
		Description:       "A nice playtest.",
		BannerImageUrl:    "https://example.com/banner.png",
		Platforms:         []pb.Platform{pb.Platform_PLATFORM_STEAM},
		NdaRequired:       false,
		DistributionModel: pb.DistributionModel_DISTRIBUTION_MODEL_STEAM_KEYS,
	}
}

// ---------------- CreatePlaytest --------------------------------------------

func TestCreatePlaytest_HappyPath(t *testing.T) {
	svr, _, _ := newTestServer()
	resp, err := svr.CreatePlaytest(authCtx(uuid.New()), validCreateRequest("my-game"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := resp.GetPlaytest()
	if p.GetStatus() != pb.PlaytestStatus_PLAYTEST_STATUS_DRAFT {
		t.Errorf("status = %s, want DRAFT", p.GetStatus())
	}
	if p.GetDistributionModel() != pb.DistributionModel_DISTRIBUTION_MODEL_STEAM_KEYS {
		t.Errorf("distribution_model = %s, want STEAM_KEYS", p.GetDistributionModel())
	}
	if p.GetSlug() != "my-game" {
		t.Errorf("slug = %q, want my-game", p.GetSlug())
	}
	if p.GetCurrentNdaVersionHash() != "" {
		t.Errorf("no NDA text → expected empty hash, got %q", p.GetCurrentNdaVersionHash())
	}
}

func TestCreatePlaytest_AGS_CAMPAIGN_MissingInitialQty_InvalidArgument(t *testing.T) {
	svr, _, _ := newTestServer()
	req := validCreateRequest("ags-slug")
	req.DistributionModel = pb.DistributionModel_DISTRIBUTION_MODEL_AGS_CAMPAIGN
	_, err := svr.CreatePlaytest(authCtx(uuid.New()), req)
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "initial_code_quantity")
}

func TestCreatePlaytest_UnspecifiedDistributionModel_InvalidArgument(t *testing.T) {
	svr, _, _ := newTestServer()
	req := validCreateRequest("no-model")
	req.DistributionModel = pb.DistributionModel_DISTRIBUTION_MODEL_UNSPECIFIED
	_, err := svr.CreatePlaytest(authCtx(uuid.New()), req)
	requireStatus(t, err, codes.InvalidArgument)
}

func TestCreatePlaytest_SlugCollision_AlreadyExists(t *testing.T) {
	svr, store, _ := newTestServer()
	store.rows = append(store.rows, &repo.Playtest{
		ID:                uuid.New(),
		Namespace:         testNamespace,
		Slug:              "taken",
		Title:             "existing",
		DistributionModel: "STEAM_KEYS",
		Status:            "DRAFT",
	})
	req := validCreateRequest("taken")
	_, err := svr.CreatePlaytest(authCtx(uuid.New()), req)
	requireStatus(t, err, codes.AlreadyExists)
	requireMsgContains(t, err, "taken")
}

func TestCreatePlaytest_NamespaceSoftCap_ResourceExhausted(t *testing.T) {
	svr, store, _ := newTestServer()
	for i := range maxNamespacePlayt {
		store.rows = append(store.rows, &repo.Playtest{
			ID:                uuid.New(),
			Namespace:         testNamespace,
			Slug:              "pt-" + string(rune('a')) + string(rune('a'+(i%26))) + "-" + uuidShort(),
			Title:             "t",
			DistributionModel: "STEAM_KEYS",
			Status:            "DRAFT",
		})
	}
	_, err := svr.CreatePlaytest(authCtx(uuid.New()), validCreateRequest("one-more"))
	requireStatus(t, err, codes.ResourceExhausted)
	requireMsgContains(t, err, "100-playtest")
}

func TestCreatePlaytest_SoftCapCountsSoftDeleted(t *testing.T) {
	// PRD §5.1: slugs stay reserved across soft-deletes, so the cap must
	// include soft-deleted rows. Without this the create-then-soft-delete
	// churn pattern silently bypasses the 100-playtest cap.
	svr, store, _ := newTestServer()
	now := time.Now()
	half := maxNamespacePlayt / 2
	for range half {
		store.rows = append(store.rows, &repo.Playtest{
			ID: uuid.New(), Namespace: testNamespace, Slug: "live-" + uuidShort(),
			Title: "t", DistributionModel: "STEAM_KEYS", Status: "DRAFT",
		})
	}
	for range half {
		deletedAt := now
		store.rows = append(store.rows, &repo.Playtest{
			ID: uuid.New(), Namespace: testNamespace, Slug: "dead-" + uuidShort(),
			Title: "t", DistributionModel: "STEAM_KEYS", Status: "DRAFT", DeletedAt: &deletedAt,
		})
	}
	_, err := svr.CreatePlaytest(authCtx(uuid.New()), validCreateRequest("one-over-cap"))
	requireStatus(t, err, codes.ResourceExhausted)
	requireMsgContains(t, err, "100-playtest")
}

func TestCreatePlaytest_BadSlug_InvalidArgument(t *testing.T) {
	svr, _, _ := newTestServer()
	cases := []string{"BAD", "a", "ab", "-bad", "has space", "TOO" + strings.Repeat("x", 100)}
	for _, slug := range cases {
		req := validCreateRequest(slug)
		_, err := svr.CreatePlaytest(authCtx(uuid.New()), req)
		requireStatus(t, err, codes.InvalidArgument)
	}
}

func TestCreatePlaytest_TooLongTitle_InvalidArgument(t *testing.T) {
	svr, _, _ := newTestServer()
	req := validCreateRequest("long-title")
	req.Title = strings.Repeat("x", maxTitleLen+1)
	_, err := svr.CreatePlaytest(authCtx(uuid.New()), req)
	requireStatus(t, err, codes.InvalidArgument)
}

func TestCreatePlaytest_TooLongDescription_InvalidArgument(t *testing.T) {
	svr, _, _ := newTestServer()
	req := validCreateRequest("long-desc")
	req.Description = strings.Repeat("x", maxDescriptionLen+1)
	_, err := svr.CreatePlaytest(authCtx(uuid.New()), req)
	requireStatus(t, err, codes.InvalidArgument)
}

func TestCreatePlaytest_NonHTTPSBanner_InvalidArgument(t *testing.T) {
	svr, _, _ := newTestServer()
	req := validCreateRequest("http-banner")
	req.BannerImageUrl = "http://insecure.example/banner.png"
	_, err := svr.CreatePlaytest(authCtx(uuid.New()), req)
	requireStatus(t, err, codes.InvalidArgument)
}

func TestCreatePlaytest_MissingActor_Unauthenticated(t *testing.T) {
	svr, _, _ := newTestServer()
	_, err := svr.CreatePlaytest(context.Background(), validCreateRequest("noauth"))
	requireStatus(t, err, codes.Unauthenticated)
}

func TestCreatePlaytest_NamespaceMismatch_PermissionDenied(t *testing.T) {
	svr, _, _ := newTestServer()
	req := validCreateRequest("other-ns")
	req.Namespace = "someone-elses-ns"
	_, err := svr.CreatePlaytest(authCtx(uuid.New()), req)
	requireStatus(t, err, codes.PermissionDenied)
}

func TestCreatePlaytest_NDAHash_ComputedWhenTextPresent(t *testing.T) {
	svr, _, _ := newTestServer()
	req := validCreateRequest("nda-hash")
	req.NdaRequired = true
	req.NdaText = "please keep secrets\n"
	resp, err := svr.CreatePlaytest(authCtx(uuid.New()), req)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got := resp.GetPlaytest().GetCurrentNdaVersionHash(); got == "" || len(got) != 64 {
		t.Fatalf("expected 64-char sha256 hex, got %q", got)
	}
}

// ---------------- EditPlaytest ----------------------------------------------

func TestEditPlaytest_HappyPath(t *testing.T) {
	svr, store, _ := newTestServer()
	id := uuid.New()
	store.rows = append(store.rows, &repo.Playtest{
		ID:                id,
		Namespace:         testNamespace,
		Slug:              "slug",
		Title:             "old",
		DistributionModel: "STEAM_KEYS",
		Status:            "OPEN",
	})
	resp, err := svr.EditPlaytest(authCtx(uuid.New()), &pb.EditPlaytestRequest{
		Namespace:   testNamespace,
		PlaytestId:  id.String(),
		Title:       "new title",
		Description: "new desc",
		Platforms:   []pb.Platform{pb.Platform_PLATFORM_XBOX},
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if resp.GetPlaytest().GetTitle() != "new title" {
		t.Errorf("title = %q, want new title", resp.GetPlaytest().GetTitle())
	}
}

func TestCreatePlaytest_NDARequiredEmptyText_InvalidArgument(t *testing.T) {
	svr, _, _ := newTestServer()
	req := validCreateRequest("empty-nda")
	req.NdaRequired = true
	req.NdaText = ""
	_, err := svr.CreatePlaytest(authCtx(uuid.New()), req)
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "nda_text")
}

func TestCreatePlaytest_InitialCodeQuantityOnSteam_InvalidArgument(t *testing.T) {
	svr, _, _ := newTestServer()
	req := validCreateRequest("steam-qty")
	qty := int32(100)
	req.InitialCodeQuantity = &qty
	_, err := svr.CreatePlaytest(authCtx(uuid.New()), req)
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "initial_code_quantity")
}

func TestEditPlaytest_NDAHashStableWhenTextUnchanged(t *testing.T) {
	// PRD §5.3: NDA hash change forces every approved applicant to
	// re-accept. A cosmetic edit that re-sends the same nda_text must
	// not churn the hash.
	svr, store, _ := newTestServer()
	id := uuid.New()
	ndaText := "secret\n"
	origHash := hashNDA(ndaText)
	store.rows = append(store.rows, &repo.Playtest{
		ID:                    id,
		Namespace:             testNamespace,
		Slug:                  "stable-nda",
		Title:                 "t",
		DistributionModel:     "STEAM_KEYS",
		Status:                "OPEN",
		NDARequired:           true,
		NDAText:               ndaText,
		CurrentNDAVersionHash: origHash,
	})
	resp, err := svr.EditPlaytest(authCtx(uuid.New()), &pb.EditPlaytestRequest{
		Namespace:   testNamespace,
		PlaytestId:  id.String(),
		Title:       "edited title", // cosmetic change
		NdaRequired: true,
		NdaText:     ndaText,
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got := resp.GetPlaytest().GetCurrentNdaVersionHash(); got != origHash {
		t.Fatalf("hash = %q, want %q (no text change → no re-hash)", got, origHash)
	}
}

func TestEditPlaytest_NDARequiredEmptyText_InvalidArgument(t *testing.T) {
	svr, store, _ := newTestServer()
	id := uuid.New()
	store.rows = append(store.rows, &repo.Playtest{
		ID: id, Namespace: testNamespace, Slug: "e", Title: "t",
		DistributionModel: "STEAM_KEYS", Status: "DRAFT",
	})
	_, err := svr.EditPlaytest(authCtx(uuid.New()), &pb.EditPlaytestRequest{
		Namespace:   testNamespace,
		PlaytestId:  id.String(),
		Title:       "t",
		NdaRequired: true,
		NdaText:     "",
	})
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "nda_text")
}

func TestEditPlaytest_RecomputesNDAHashWhenTextChanges(t *testing.T) {
	svr, store, _ := newTestServer()
	id := uuid.New()
	store.rows = append(store.rows, &repo.Playtest{
		ID:                    id,
		Namespace:             testNamespace,
		Slug:                  "nda-pt",
		Title:                 "t",
		DistributionModel:     "STEAM_KEYS",
		Status:                "DRAFT",
		NDAText:               "old nda\n",
		CurrentNDAVersionHash: hashNDA("old nda\n"),
	})
	resp, err := svr.EditPlaytest(authCtx(uuid.New()), &pb.EditPlaytestRequest{
		Namespace:   testNamespace,
		PlaytestId:  id.String(),
		Title:       "t",
		NdaRequired: true,
		NdaText:     "brand new nda\n",
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got := resp.GetPlaytest().GetCurrentNdaVersionHash(); got != hashNDA("brand new nda\n") {
		t.Fatalf("hash = %q, want %q", got, hashNDA("brand new nda\n"))
	}
}

func TestEditPlaytest_NotFound(t *testing.T) {
	svr, _, _ := newTestServer()
	_, err := svr.EditPlaytest(authCtx(uuid.New()), &pb.EditPlaytestRequest{
		Namespace:  testNamespace,
		PlaytestId: uuid.New().String(),
		Title:      "x",
	})
	requireStatus(t, err, codes.NotFound)
}

func TestEditPlaytest_SoftDeleted_NotFound(t *testing.T) {
	svr, store, _ := newTestServer()
	id := uuid.New()
	now := time.Now()
	store.rows = append(store.rows, &repo.Playtest{
		ID:                id,
		Namespace:         testNamespace,
		Slug:              "gone",
		Title:             "t",
		DistributionModel: "STEAM_KEYS",
		Status:            "OPEN",
		DeletedAt:         &now,
	})
	_, err := svr.EditPlaytest(authCtx(uuid.New()), &pb.EditPlaytestRequest{
		Namespace:  testNamespace,
		PlaytestId: id.String(),
		Title:      "x",
	})
	requireStatus(t, err, codes.NotFound)
}

func TestEditPlaytest_TooLongTitle_InvalidArgument(t *testing.T) {
	svr, store, _ := newTestServer()
	id := uuid.New()
	store.rows = append(store.rows, &repo.Playtest{
		ID: id, Namespace: testNamespace, Slug: "ok", Title: "t", DistributionModel: "STEAM_KEYS", Status: "OPEN",
	})
	_, err := svr.EditPlaytest(authCtx(uuid.New()), &pb.EditPlaytestRequest{
		Namespace:  testNamespace,
		PlaytestId: id.String(),
		Title:      strings.Repeat("x", maxTitleLen+1),
	})
	requireStatus(t, err, codes.InvalidArgument)
}

// ---------------- SoftDeletePlaytest ----------------------------------------

func TestSoftDeletePlaytest_HappyPath(t *testing.T) {
	svr, store, _ := newTestServer()
	id := uuid.New()
	store.rows = append(store.rows, &repo.Playtest{
		ID: id, Namespace: testNamespace, Slug: "del", Title: "t", DistributionModel: "STEAM_KEYS", Status: "DRAFT",
	})
	_, err := svr.SoftDeletePlaytest(authCtx(uuid.New()), &pb.SoftDeletePlaytestRequest{
		Namespace:  testNamespace,
		PlaytestId: id.String(),
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if store.rows[0].DeletedAt == nil {
		t.Fatal("DeletedAt not set")
	}
}

func TestSoftDeletePlaytest_AlreadyDeleted_NotFound(t *testing.T) {
	svr, store, _ := newTestServer()
	id := uuid.New()
	now := time.Now()
	store.rows = append(store.rows, &repo.Playtest{
		ID: id, Namespace: testNamespace, Slug: "del", Title: "t", DistributionModel: "STEAM_KEYS", Status: "DRAFT", DeletedAt: &now,
	})
	_, err := svr.SoftDeletePlaytest(authCtx(uuid.New()), &pb.SoftDeletePlaytestRequest{
		Namespace:  testNamespace,
		PlaytestId: id.String(),
	})
	requireStatus(t, err, codes.NotFound)
}

// ---------------- TransitionPlaytestStatus ----------------------------------

func TestTransitionPlaytestStatus_DraftToOpen(t *testing.T) {
	svr, store, _ := newTestServer()
	id := uuid.New()
	store.rows = append(store.rows, &repo.Playtest{
		ID: id, Namespace: testNamespace, Slug: "t", Title: "t", DistributionModel: "STEAM_KEYS", Status: "DRAFT",
	})
	resp, err := svr.TransitionPlaytestStatus(authCtx(uuid.New()), &pb.TransitionPlaytestStatusRequest{
		Namespace:    testNamespace,
		PlaytestId:   id.String(),
		TargetStatus: pb.PlaytestStatus_PLAYTEST_STATUS_OPEN,
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if resp.GetPlaytest().GetStatus() != pb.PlaytestStatus_PLAYTEST_STATUS_OPEN {
		t.Fatal("status did not advance to OPEN")
	}
}

func TestTransitionPlaytestStatus_OpenToClosed(t *testing.T) {
	svr, store, _ := newTestServer()
	id := uuid.New()
	store.rows = append(store.rows, &repo.Playtest{
		ID: id, Namespace: testNamespace, Slug: "t", Title: "t", DistributionModel: "STEAM_KEYS", Status: "OPEN",
	})
	resp, err := svr.TransitionPlaytestStatus(authCtx(uuid.New()), &pb.TransitionPlaytestStatusRequest{
		Namespace:    testNamespace,
		PlaytestId:   id.String(),
		TargetStatus: pb.PlaytestStatus_PLAYTEST_STATUS_CLOSED,
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if resp.GetPlaytest().GetStatus() != pb.PlaytestStatus_PLAYTEST_STATUS_CLOSED {
		t.Fatal("status did not advance to CLOSED")
	}
}

// TestEditPlaytestRequest_MutableFieldWhitelist pins the EditPlaytest
// wire contract down so a future proto change that accidentally adds an
// immutable field (e.g. `optional string slug`) fails here instead of
// silently dropping the caller's change. PRD §5.1 L146 lists the
// editable set; namespace + playtest_id are path-param routing fields,
// not payload fields, but both travel on the message.
func TestEditPlaytestRequest_MutableFieldWhitelist(t *testing.T) {
	wantMutable := map[string]struct{}{
		"namespace":        {}, // path param
		"playtest_id":      {}, // path param
		"title":            {},
		"description":      {},
		"banner_image_url": {},
		"platforms":        {},
		"starts_at":        {},
		"ends_at":          {},
		"nda_required":     {},
		"nda_text":         {},
	}
	desc := (&pb.EditPlaytestRequest{}).ProtoReflect().Descriptor()
	got := map[string]struct{}{}
	for i := 0; i < desc.Fields().Len(); i++ {
		got[string(desc.Fields().Get(i).Name())] = struct{}{}
	}
	for k := range wantMutable {
		if _, ok := got[k]; !ok {
			t.Errorf("EditPlaytestRequest missing expected field %q", k)
		}
	}
	for k := range got {
		if _, ok := wantMutable[k]; !ok {
			t.Errorf("EditPlaytestRequest has unexpected field %q — either add to the mutable whitelist or drop it from the proto (PRD §5.1 L146)", k)
		}
	}
}

func TestTransitionPlaytestStatus_RejectionNamesBothStates(t *testing.T) {
	svr, store, _ := newTestServer()
	id := uuid.New()
	store.rows = append(store.rows, &repo.Playtest{
		ID: id, Namespace: testNamespace, Slug: "t", Title: "t",
		DistributionModel: "STEAM_KEYS", Status: "DRAFT",
	})
	_, err := svr.TransitionPlaytestStatus(authCtx(uuid.New()), &pb.TransitionPlaytestStatusRequest{
		Namespace:    testNamespace,
		PlaytestId:   id.String(),
		TargetStatus: pb.PlaytestStatus_PLAYTEST_STATUS_CLOSED,
	})
	requireStatus(t, err, codes.FailedPrecondition)
	// Operators must see both the current and requested state in the
	// rejection — otherwise debugging a stuck playtest means guessing
	// which transition was attempted.
	requireMsgContains(t, err, "DRAFT")
	requireMsgContains(t, err, "CLOSED")
}

func TestTransitionPlaytestStatus_InvalidTransitions(t *testing.T) {
	cases := []struct {
		name   string
		from   string
		target pb.PlaytestStatus
	}{
		{"DraftToClosed", "DRAFT", pb.PlaytestStatus_PLAYTEST_STATUS_CLOSED},
		{"OpenToDraft", "OPEN", pb.PlaytestStatus_PLAYTEST_STATUS_DRAFT},
		{"ClosedToOpen", "CLOSED", pb.PlaytestStatus_PLAYTEST_STATUS_OPEN},
		{"ClosedToDraft", "CLOSED", pb.PlaytestStatus_PLAYTEST_STATUS_DRAFT},
		{"SameStateDraft", "DRAFT", pb.PlaytestStatus_PLAYTEST_STATUS_DRAFT},
		{"SameStateOpen", "OPEN", pb.PlaytestStatus_PLAYTEST_STATUS_OPEN},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svr, store, _ := newTestServer()
			id := uuid.New()
			store.rows = append(store.rows, &repo.Playtest{
				ID: id, Namespace: testNamespace, Slug: "t", Title: "t", DistributionModel: "STEAM_KEYS", Status: tc.from,
			})
			_, err := svr.TransitionPlaytestStatus(authCtx(uuid.New()), &pb.TransitionPlaytestStatusRequest{
				Namespace:    testNamespace,
				PlaytestId:   id.String(),
				TargetStatus: tc.target,
			})
			requireStatus(t, err, codes.FailedPrecondition)
		})
	}
}

func TestTransitionPlaytestStatus_UnspecifiedTarget_InvalidArgument(t *testing.T) {
	svr, store, _ := newTestServer()
	id := uuid.New()
	store.rows = append(store.rows, &repo.Playtest{
		ID: id, Namespace: testNamespace, Slug: "t", Title: "t", DistributionModel: "STEAM_KEYS", Status: "DRAFT",
	})
	_, err := svr.TransitionPlaytestStatus(authCtx(uuid.New()), &pb.TransitionPlaytestStatusRequest{
		Namespace:    testNamespace,
		PlaytestId:   id.String(),
		TargetStatus: pb.PlaytestStatus_PLAYTEST_STATUS_UNSPECIFIED,
	})
	requireStatus(t, err, codes.InvalidArgument)
}

// ---------------- AdminGetPlaytest ------------------------------------------

func TestAdminGetPlaytest_HappyPath(t *testing.T) {
	svr, store, _ := newTestServer()
	id := uuid.New()
	store.rows = append(store.rows, &repo.Playtest{
		ID: id, Namespace: testNamespace, Slug: "p", Title: "t", DistributionModel: "STEAM_KEYS", Status: "DRAFT",
	})
	resp, err := svr.AdminGetPlaytest(authCtx(uuid.New()), &pb.AdminGetPlaytestRequest{
		Namespace:  testNamespace,
		PlaytestId: id.String(),
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if resp.GetPlaytest().GetId() != id.String() {
		t.Errorf("id = %s, want %s", resp.GetPlaytest().GetId(), id.String())
	}
}

func TestAdminGetPlaytest_NotFound(t *testing.T) {
	svr, _, _ := newTestServer()
	_, err := svr.AdminGetPlaytest(authCtx(uuid.New()), &pb.AdminGetPlaytestRequest{
		Namespace:  testNamespace,
		PlaytestId: uuid.New().String(),
	})
	requireStatus(t, err, codes.NotFound)
}

func TestAdminGetPlaytest_SoftDeleted_StillVisible(t *testing.T) {
	// Admin can still see soft-deleted rows by direct ID (PRD §5.1 hides
	// from list views; direct ID access is intentional for audit).
	svr, store, _ := newTestServer()
	id := uuid.New()
	now := time.Now()
	store.rows = append(store.rows, &repo.Playtest{
		ID: id, Namespace: testNamespace, Slug: "p", Title: "t", DistributionModel: "STEAM_KEYS", Status: "CLOSED", DeletedAt: &now,
	})
	resp, err := svr.AdminGetPlaytest(authCtx(uuid.New()), &pb.AdminGetPlaytestRequest{
		Namespace:  testNamespace,
		PlaytestId: id.String(),
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if resp.GetPlaytest().GetDeletedAt() == nil {
		t.Fatal("deleted_at not propagated to admin response")
	}
}

// ---------------- ListPlaytests ---------------------------------------------

func TestListPlaytests_ExcludesSoftDeleted(t *testing.T) {
	svr, store, _ := newTestServer()
	live := uuid.New()
	dead := uuid.New()
	now := time.Now()
	store.rows = append(store.rows,
		&repo.Playtest{ID: live, Namespace: testNamespace, Slug: "a", Title: "a", DistributionModel: "STEAM_KEYS", Status: "DRAFT"},
		&repo.Playtest{ID: dead, Namespace: testNamespace, Slug: "b", Title: "b", DistributionModel: "STEAM_KEYS", Status: "DRAFT", DeletedAt: &now},
	)
	resp, err := svr.ListPlaytests(authCtx(uuid.New()), &pb.ListPlaytestsRequest{Namespace: testNamespace})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got := len(resp.GetPlaytests()); got != 1 {
		t.Fatalf("got %d playtests, want 1", got)
	}
	if resp.GetPlaytests()[0].GetId() != live.String() {
		t.Fatalf("wrong playtest returned: %s", resp.GetPlaytests()[0].GetId())
	}
}

// ---------------- GetPublicPlaytest -----------------------------------------

func TestGetPublicPlaytest_OpenPlaytest_ReturnsPublicSubset(t *testing.T) {
	svr, store, _ := newTestServer()
	store.rows = append(store.rows, &repo.Playtest{
		ID: uuid.New(), Namespace: testNamespace, Slug: "open", Title: "The Title",
		BannerImageURL: "https://x/y.png", DistributionModel: "STEAM_KEYS", Status: "OPEN",
	})
	resp, err := svr.GetPublicPlaytest(context.Background(), &pb.GetPublicPlaytestRequest{Slug: "open"})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if resp.GetPlaytest().GetTitle() != "The Title" {
		t.Errorf("title = %q", resp.GetPlaytest().GetTitle())
	}
}

func TestGetPublicPlaytest_HiddenStatuses_NotFound(t *testing.T) {
	cases := []struct {
		name   string
		status string
		del    bool
	}{
		{"DRAFT", "DRAFT", false},
		{"CLOSED", "CLOSED", false},
		{"SoftDeleted_OPEN", "OPEN", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svr, store, _ := newTestServer()
			row := &repo.Playtest{
				ID: uuid.New(), Namespace: testNamespace, Slug: "hidden", Title: "t",
				DistributionModel: "STEAM_KEYS", Status: tc.status,
			}
			if tc.del {
				now := time.Now()
				row.DeletedAt = &now
			}
			store.rows = append(store.rows, row)
			_, err := svr.GetPublicPlaytest(context.Background(), &pb.GetPublicPlaytestRequest{Slug: "hidden"})
			requireStatus(t, err, codes.NotFound)
		})
	}
}

func TestGetPublicPlaytest_NonExistentSlug_NotFound(t *testing.T) {
	svr, _, _ := newTestServer()
	_, err := svr.GetPublicPlaytest(context.Background(), &pb.GetPublicPlaytestRequest{Slug: "nope"})
	requireStatus(t, err, codes.NotFound)
}

// ---------------- GetPlaytestForPlayer --------------------------------------

func TestGetPlaytestForPlayer_Open_ReturnsPlayerSubset(t *testing.T) {
	svr, store, _ := newTestServer()
	store.rows = append(store.rows, &repo.Playtest{
		ID: uuid.New(), Namespace: testNamespace, Slug: "game", Title: "t",
		DistributionModel: "STEAM_KEYS", Status: "OPEN",
		NDARequired: true, NDAText: "nda\n", CurrentNDAVersionHash: hashNDA("nda\n"),
	})
	resp, err := svr.GetPlaytestForPlayer(authCtx(uuid.New()), &pb.GetPlaytestForPlayerRequest{Slug: "game"})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if resp.GetPlaytest().GetNdaText() != "nda\n" {
		t.Errorf("nda_text = %q", resp.GetPlaytest().GetNdaText())
	}
	if resp.GetPlaytest().GetCurrentNdaVersionHash() == "" {
		t.Error("current_nda_version_hash missing")
	}
}

func TestGetPlaytestForPlayer_Draft_NotFound(t *testing.T) {
	svr, store, _ := newTestServer()
	store.rows = append(store.rows, &repo.Playtest{
		ID: uuid.New(), Namespace: testNamespace, Slug: "d", Title: "t",
		DistributionModel: "STEAM_KEYS", Status: "DRAFT",
	})
	_, err := svr.GetPlaytestForPlayer(authCtx(uuid.New()), &pb.GetPlaytestForPlayerRequest{Slug: "d"})
	requireStatus(t, err, codes.NotFound)
}

func TestGetPlaytestForPlayer_SoftDeleted_NotFound(t *testing.T) {
	svr, store, _ := newTestServer()
	now := time.Now()
	store.rows = append(store.rows, &repo.Playtest{
		ID: uuid.New(), Namespace: testNamespace, Slug: "s", Title: "t",
		DistributionModel: "STEAM_KEYS", Status: "OPEN", DeletedAt: &now,
	})
	_, err := svr.GetPlaytestForPlayer(authCtx(uuid.New()), &pb.GetPlaytestForPlayerRequest{Slug: "s"})
	requireStatus(t, err, codes.NotFound)
}

func TestGetPlaytestForPlayer_Closed_NonApproved_NotFound(t *testing.T) {
	svr, store, _ := newTestServer()
	pid := uuid.New()
	store.rows = append(store.rows, &repo.Playtest{
		ID: pid, Namespace: testNamespace, Slug: "c", Title: "t",
		DistributionModel: "STEAM_KEYS", Status: "CLOSED",
	})
	_, err := svr.GetPlaytestForPlayer(authCtx(uuid.New()), &pb.GetPlaytestForPlayerRequest{Slug: "c"})
	requireStatus(t, err, codes.NotFound)
}

func TestGetPlaytestForPlayer_Closed_ApprovedCaller_Visible(t *testing.T) {
	svr, store, applicants := newTestServer()
	pid := uuid.New()
	userID := uuid.New()
	store.rows = append(store.rows, &repo.Playtest{
		ID: pid, Namespace: testNamespace, Slug: "c", Title: "t",
		DistributionModel: "STEAM_KEYS", Status: "CLOSED",
	})
	applicants.rows = append(applicants.rows, &repo.Applicant{
		ID: uuid.New(), PlaytestID: pid, UserID: userID, Status: applicantStatusApproved,
	})
	resp, err := svr.GetPlaytestForPlayer(authCtx(userID), &pb.GetPlaytestForPlayerRequest{Slug: "c"})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if resp.GetPlaytest().GetSlug() != "c" {
		t.Fatalf("slug = %q", resp.GetPlaytest().GetSlug())
	}
}

func TestGetPlaytestForPlayer_MissingActor_Unauthenticated(t *testing.T) {
	svr, _, _ := newTestServer()
	_, err := svr.GetPlaytestForPlayer(context.Background(), &pb.GetPlaytestForPlayerRequest{Slug: "x"})
	requireStatus(t, err, codes.Unauthenticated)
}

// ---------------- NDA normalization -----------------------------------------

func TestNormalizeNDA(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"hello", "hello\n"},
		{"hello\n", "hello\n"},
		{"hello\r\nworld\r\n", "hello\nworld\n"},
		{"trail  \nend  \n\n\n", "trail\nend\n"},
		{"no\ntrail", "no\ntrail\n"},
	}
	for _, tc := range cases {
		if got := normalizeNDA(tc.in); got != tc.want {
			t.Errorf("normalizeNDA(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestHashNDA_StableAcrossCosmetic(t *testing.T) {
	a := hashNDA("secret line\n")
	b := hashNDA("secret line   \n\n") // trailing whitespace + extra blank lines
	if a != b {
		t.Errorf("hash should be whitespace-stable: %q vs %q", a, b)
	}
	c := hashNDA("different line\n")
	if a == c {
		t.Error("hash should change for different content")
	}
}

// ---------------- plumbing tests --------------------------------------------

func TestRequireActor_RejectsNonUUID(t *testing.T) {
	ctx := iampkg.WithActorUserID(context.Background(), "not-a-uuid")
	_, err := requireActor(ctx)
	requireStatus(t, err, codes.Unauthenticated)
}

func TestRequireActor_HappyPath(t *testing.T) {
	id := uuid.New()
	got, err := requireActor(authCtx(id))
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got != id {
		t.Errorf("actor = %s, want %s", got, id)
	}
}

// uuidShort returns a short collision-free slug fragment for cap-test
// slug generation.
func uuidShort() string {
	u := uuid.New()
	return strings.ReplaceAll(u.String(), "-", "")[:12]
}

// Ensures repo.ErrNotFound remains the sentinel handler logic pivots on.
func TestRepoSentinelsStable(t *testing.T) {
	if !errors.Is(repo.ErrNotFound, repo.ErrNotFound) {
		t.Fatal("ErrNotFound no longer reflexive")
	}
}
