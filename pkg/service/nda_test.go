package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// fakeNDAAcceptanceStore is an in-memory store that mirrors the natural
// (userId, playtestId, ndaVersionHash) PK of the production table —
// re-accepts on the same key return the original row + replay=true,
// re-accepts after an NDA edit (different hash) write a new row.
type fakeNDAAcceptanceStore struct {
	rows []*repo.NDAAcceptance
}

func (f *fakeNDAAcceptanceStore) AcceptIdempotent(_ context.Context, a *repo.NDAAcceptance) (*repo.NDAAcceptance, bool, error) {
	for _, existing := range f.rows {
		if existing.UserID == a.UserID && existing.PlaytestID == a.PlaytestID && existing.NDAVersionHash == a.NDAVersionHash {
			clone := *existing
			return &clone, true, nil
		}
	}
	clone := *a
	if clone.AcceptedAt.IsZero() {
		clone.AcceptedAt = time.Now()
	}
	f.rows = append(f.rows, &clone)
	ret := clone
	return &ret, false, nil
}

func (f *fakeNDAAcceptanceStore) Get(_ context.Context, userID, playtestID uuid.UUID, hash string) (*repo.NDAAcceptance, error) {
	for _, existing := range f.rows {
		if existing.UserID == userID && existing.PlaytestID == playtestID && existing.NDAVersionHash == hash {
			clone := *existing
			return &clone, nil
		}
	}
	return nil, repo.ErrNotFound
}

func (f *fakeNDAAcceptanceStore) LatestForApplicant(_ context.Context, userID, playtestID uuid.UUID) (*repo.NDAAcceptance, error) {
	var latest *repo.NDAAcceptance
	for _, existing := range f.rows {
		if existing.UserID != userID || existing.PlaytestID != playtestID {
			continue
		}
		if latest == nil || existing.AcceptedAt.After(latest.AcceptedAt) {
			latest = existing
		}
	}
	if latest == nil {
		return nil, repo.ErrNotFound
	}
	clone := *latest
	return &clone, nil
}

// fakeAuditLogStore captures every Append call; tests assert on row
// count, action constant, and JSONB payload contents.
type fakeAuditLogStore struct {
	rows []*repo.AuditLog
}

func (f *fakeAuditLogStore) Append(_ context.Context, row *repo.AuditLog) (*repo.AuditLog, error) {
	clone := *row
	clone.ID = uuid.New()
	clone.CreatedAt = time.Now()
	f.rows = append(f.rows, &clone)
	ret := clone
	return &ret, nil
}

func (f *fakeAuditLogStore) ListByPlaytest(_ context.Context, playtestID uuid.UUID, limit int) ([]*repo.AuditLog, error) {
	out := make([]*repo.AuditLog, 0)
	for _, r := range f.rows {
		if r.PlaytestID != nil && *r.PlaytestID == playtestID {
			clone := *r
			out = append(out, &clone)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// countAction returns the number of captured rows whose action equals
// the supplied value. Test helper for AGS_CAMPAIGN audit assertions.
func (f *fakeAuditLogStore) countAction(action string) int {
	n := 0
	for _, r := range f.rows {
		if r.Action == action {
			n++
		}
	}
	return n
}

// ndaTestRig bundles the test server with every fake store the M2 NDA
// flow exercises, so call sites don't have to spell out unused returns
// in tuple-destructure form (golangci-lint dogsled threshold).
type ndaTestRig struct {
	svr        *PlaytesthubServiceServer
	playtests  *fakePlaytestStore
	applicants *fakeApplicantStore
	nda        *fakeNDAAcceptanceStore
	audit      *fakeAuditLogStore
}

func withNDAStores(t *testing.T) ndaTestRig {
	t.Helper()
	svr, pt, ap := newTestServer()
	nda := &fakeNDAAcceptanceStore{}
	audit := &fakeAuditLogStore{}
	svr = svr.WithNDAStore(nda).WithAuditLogStore(audit)
	return ndaTestRig{svr: svr, playtests: pt, applicants: ap, nda: nda, audit: audit}
}

// ---------------- AcceptNDA -------------------------------------------------

func TestAcceptNDA_HappyPath_WritesRowAndStampsApplicant(t *testing.T) {
	rig := withNDAStores(t)
	svr, store, applicants, nda, audit := rig.svr, rig.playtests, rig.applicants, rig.nda, rig.audit

	pt := openPlaytest("nda-game")
	pt.NDARequired = true
	pt.NDAText = "the NDA prose v1"
	pt.CurrentNDAVersionHash = hashNDA(pt.NDAText)
	store.rows = append(store.rows, pt)

	userID := uuid.New()
	applicants.rows = append(applicants.rows, &repo.Applicant{
		ID:         uuid.New(),
		PlaytestID: pt.ID,
		UserID:     userID,
		Status:     applicantStatusPending,
		CreatedAt:  time.Now(),
	})

	resp, err := svr.AcceptNDA(authCtx(userID), &pb.AcceptNDARequest{PlaytestId: pt.ID.String()})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if resp.GetAcceptance().GetNdaVersionHash() != pt.CurrentNDAVersionHash {
		t.Errorf("hash on response = %q, want %q", resp.GetAcceptance().GetNdaVersionHash(), pt.CurrentNDAVersionHash)
	}
	if got := len(nda.rows); got != 1 {
		t.Fatalf("nda rows = %d, want 1", got)
	}
	stamped := applicants.rows[0]
	if stamped.NDAVersionHash == nil || *stamped.NDAVersionHash != pt.CurrentNDAVersionHash {
		t.Errorf("applicant.nda_version_hash not stamped: got %v", stamped.NDAVersionHash)
	}
	if got := len(audit.rows); got != 1 || audit.rows[0].Action != repo.ActionNDAAccept {
		t.Fatalf("audit rows = %d (want 1 nda.accept), got actions=%v", len(audit.rows), auditActions(audit.rows))
	}
	if audit.rows[0].ActorUserID == nil || *audit.rows[0].ActorUserID != userID {
		t.Errorf("nda.accept actor = %v, want %s", audit.rows[0].ActorUserID, userID)
	}
}

func TestAcceptNDA_Replay_SameHash_NoNewAuditRow(t *testing.T) {
	rig := withNDAStores(t)
	svr, store, applicants, nda, audit := rig.svr, rig.playtests, rig.applicants, rig.nda, rig.audit

	pt := openPlaytest("nda-replay")
	pt.NDARequired = true
	pt.NDAText = "v1"
	pt.CurrentNDAVersionHash = hashNDA(pt.NDAText)
	store.rows = append(store.rows, pt)

	userID := uuid.New()
	applicants.rows = append(applicants.rows, &repo.Applicant{
		ID:         uuid.New(),
		PlaytestID: pt.ID,
		UserID:     userID,
		Status:     applicantStatusPending,
		CreatedAt:  time.Now(),
	})

	if _, err := svr.AcceptNDA(authCtx(userID), &pb.AcceptNDARequest{PlaytestId: pt.ID.String()}); err != nil {
		t.Fatalf("first accept: %v", err)
	}
	if _, err := svr.AcceptNDA(authCtx(userID), &pb.AcceptNDARequest{PlaytestId: pt.ID.String()}); err != nil {
		t.Fatalf("second accept (replay): %v", err)
	}

	if got := len(nda.rows); got != 1 {
		t.Errorf("nda rows = %d, want 1 (idempotent on natural key)", got)
	}
	if got := len(audit.rows); got != 1 {
		t.Errorf("audit rows = %d, want 1 (replay must not re-emit nda.accept)", got)
	}
}

func TestAcceptNDA_AfterEdit_NewRowAndAdvancedHash(t *testing.T) {
	rig := withNDAStores(t)
	svr, store, applicants, nda, audit := rig.svr, rig.playtests, rig.applicants, rig.nda, rig.audit

	pt := openPlaytest("nda-edit")
	pt.NDARequired = true
	pt.NDAText = "old prose"
	pt.CurrentNDAVersionHash = hashNDA(pt.NDAText)
	store.rows = append(store.rows, pt)

	userID := uuid.New()
	applicants.rows = append(applicants.rows, &repo.Applicant{
		ID:         uuid.New(),
		PlaytestID: pt.ID,
		UserID:     userID,
		Status:     applicantStatusPending,
		CreatedAt:  time.Now(),
	})

	if _, err := svr.AcceptNDA(authCtx(userID), &pb.AcceptNDARequest{PlaytestId: pt.ID.String()}); err != nil {
		t.Fatalf("first accept: %v", err)
	}

	// Admin edits NDA text — currentNdaVersionHash advances.
	pt.NDAText = "new prose"
	pt.CurrentNDAVersionHash = hashNDA(pt.NDAText)
	store.rows[0] = pt

	if _, err := svr.AcceptNDA(authCtx(userID), &pb.AcceptNDARequest{PlaytestId: pt.ID.String()}); err != nil {
		t.Fatalf("re-accept after edit: %v", err)
	}

	if got := len(nda.rows); got != 2 {
		t.Errorf("nda rows = %d, want 2 (one per accepted hash)", got)
	}
	stamped := applicants.rows[0]
	if stamped.NDAVersionHash == nil || *stamped.NDAVersionHash != pt.CurrentNDAVersionHash {
		t.Errorf("applicant.nda_version_hash not advanced: got %v want %s", stamped.NDAVersionHash, pt.CurrentNDAVersionHash)
	}
	if got := len(audit.rows); got != 2 {
		t.Errorf("audit rows = %d, want 2 (one nda.accept per first-time accept)", got)
	}
}

func TestAcceptNDA_NdaNotRequired_InvalidArgument(t *testing.T) {
	rig := withNDAStores(t)
	svr, store, applicants := rig.svr, rig.playtests, rig.applicants
	pt := openPlaytest("no-nda")
	pt.NDARequired = false
	store.rows = append(store.rows, pt)

	userID := uuid.New()
	applicants.rows = append(applicants.rows, &repo.Applicant{
		ID: uuid.New(), PlaytestID: pt.ID, UserID: userID, Status: applicantStatusPending, CreatedAt: time.Now(),
	})

	_, err := svr.AcceptNDA(authCtx(userID), &pb.AcceptNDARequest{PlaytestId: pt.ID.String()})
	requireStatus(t, err, codes.InvalidArgument)
	requireMsgContains(t, err, "nda")
}

func TestAcceptNDA_DraftPlaytest_NotFound(t *testing.T) {
	rig := withNDAStores(t)
	svr, store, applicants := rig.svr, rig.playtests, rig.applicants
	pt := openPlaytest("drafty")
	pt.Status = statusDraft
	pt.NDARequired = true
	pt.NDAText = "x"
	pt.CurrentNDAVersionHash = hashNDA(pt.NDAText)
	store.rows = append(store.rows, pt)

	userID := uuid.New()
	applicants.rows = append(applicants.rows, &repo.Applicant{
		ID: uuid.New(), PlaytestID: pt.ID, UserID: userID, Status: applicantStatusPending, CreatedAt: time.Now(),
	})

	_, err := svr.AcceptNDA(authCtx(userID), &pb.AcceptNDARequest{PlaytestId: pt.ID.String()})
	requireStatus(t, err, codes.NotFound)
}

func TestAcceptNDA_SoftDeletedPlaytest_NotFound(t *testing.T) {
	rig := withNDAStores(t)
	svr, store, applicants := rig.svr, rig.playtests, rig.applicants
	pt := openPlaytest("gone")
	pt.NDARequired = true
	pt.NDAText = "x"
	pt.CurrentNDAVersionHash = hashNDA(pt.NDAText)
	now := time.Now()
	pt.DeletedAt = &now
	store.rows = append(store.rows, pt)

	userID := uuid.New()
	applicants.rows = append(applicants.rows, &repo.Applicant{
		ID: uuid.New(), PlaytestID: pt.ID, UserID: userID, Status: applicantStatusPending, CreatedAt: time.Now(),
	})

	_, err := svr.AcceptNDA(authCtx(userID), &pb.AcceptNDARequest{PlaytestId: pt.ID.String()})
	requireStatus(t, err, codes.NotFound)
}

func TestAcceptNDA_NoApplicant_FailedPrecondition(t *testing.T) {
	rig := withNDAStores(t)
	svr, store := rig.svr, rig.playtests
	pt := openPlaytest("must-signup")
	pt.NDARequired = true
	pt.NDAText = "x"
	pt.CurrentNDAVersionHash = hashNDA(pt.NDAText)
	store.rows = append(store.rows, pt)

	_, err := svr.AcceptNDA(authCtx(uuid.New()), &pb.AcceptNDARequest{PlaytestId: pt.ID.String()})
	requireStatus(t, err, codes.FailedPrecondition)
	requireMsgContains(t, err, "signup")
}

func TestAcceptNDA_ClosedPlaytest_NonApprovedApplicant_NotFound(t *testing.T) {
	rig := withNDAStores(t)
	svr, store, applicants := rig.svr, rig.playtests, rig.applicants
	pt := openPlaytest("closed-pt")
	pt.Status = statusClosed
	pt.NDARequired = true
	pt.NDAText = "x"
	pt.CurrentNDAVersionHash = hashNDA(pt.NDAText)
	store.rows = append(store.rows, pt)

	userID := uuid.New()
	applicants.rows = append(applicants.rows, &repo.Applicant{
		ID: uuid.New(), PlaytestID: pt.ID, UserID: userID, Status: applicantStatusPending, CreatedAt: time.Now(),
	})

	_, err := svr.AcceptNDA(authCtx(userID), &pb.AcceptNDARequest{PlaytestId: pt.ID.String()})
	requireStatus(t, err, codes.NotFound)
}

func TestAcceptNDA_ClosedPlaytest_ApprovedApplicant_AllowsReaccept(t *testing.T) {
	rig := withNDAStores(t)
	svr, store, applicants, nda := rig.svr, rig.playtests, rig.applicants, rig.nda
	pt := openPlaytest("closed-pt-approved")
	pt.Status = statusClosed
	pt.NDARequired = true
	pt.NDAText = "x"
	pt.CurrentNDAVersionHash = hashNDA(pt.NDAText)
	store.rows = append(store.rows, pt)

	userID := uuid.New()
	applicants.rows = append(applicants.rows, &repo.Applicant{
		ID: uuid.New(), PlaytestID: pt.ID, UserID: userID, Status: applicantStatusApproved, CreatedAt: time.Now(),
	})

	if _, err := svr.AcceptNDA(authCtx(userID), &pb.AcceptNDARequest{PlaytestId: pt.ID.String()}); err != nil {
		t.Fatalf("approved player should be able to re-accept on CLOSED: %v", err)
	}
	if got := len(nda.rows); got != 1 {
		t.Errorf("nda rows = %d, want 1", got)
	}
}

func TestAcceptNDA_BadUUID_InvalidArgument(t *testing.T) {
	svr := withNDAStores(t).svr
	_, err := svr.AcceptNDA(authCtx(uuid.New()), &pb.AcceptNDARequest{PlaytestId: "not-a-uuid"})
	requireStatus(t, err, codes.InvalidArgument)
}

func TestAcceptNDA_Unauthenticated(t *testing.T) {
	svr := withNDAStores(t).svr
	_, err := svr.AcceptNDA(context.Background(), &pb.AcceptNDARequest{PlaytestId: uuid.New().String()})
	requireStatus(t, err, codes.Unauthenticated)
}

// ---------------- NdaReacceptRequired surface check -------------------------
//
// PRD §5.3 says clients compute NdaReacceptRequired by comparing
// GetPlaytestForPlayer.currentNdaVersionHash with
// GetApplicantStatus.ndaVersionHash. The handler test verifies both
// fields are populated on the wire so the comparison is computable
// client-side. Server-side surfacing of a derived bool is intentionally
// **not** added per the PRD.

func TestGetApplicantStatus_AndPlaytest_ExposeNdaHashesForClientComparison(t *testing.T) {
	rig := withNDAStores(t)
	svr, store, applicants := rig.svr, rig.playtests, rig.applicants

	pt := openPlaytest("expose-hashes")
	pt.NDARequired = true
	pt.NDAText = "v1"
	pt.CurrentNDAVersionHash = hashNDA(pt.NDAText)
	store.rows = append(store.rows, pt)

	userID := uuid.New()
	applicantHash := hashNDA("v0") // applicant accepted an older version
	applicants.rows = append(applicants.rows, &repo.Applicant{
		ID:             uuid.New(),
		PlaytestID:     pt.ID,
		UserID:         userID,
		Status:         applicantStatusApproved,
		CreatedAt:      time.Now(),
		NDAVersionHash: &applicantHash,
	})

	statusResp, err := svr.GetApplicantStatus(authCtx(userID), &pb.GetApplicantStatusRequest{Slug: pt.Slug})
	if err != nil {
		t.Fatalf("GetApplicantStatus: %v", err)
	}
	if got := statusResp.GetApplicant().GetNdaVersionHash(); got != applicantHash {
		t.Errorf("applicant.nda_version_hash on wire = %q, want %q", got, applicantHash)
	}

	playtestResp, err := svr.GetPlaytestForPlayer(authCtx(userID), &pb.GetPlaytestForPlayerRequest{Slug: pt.Slug})
	if err != nil {
		t.Fatalf("GetPlaytestForPlayer: %v", err)
	}
	if got := playtestResp.GetPlaytest().GetCurrentNdaVersionHash(); got != pt.CurrentNDAVersionHash {
		t.Errorf("playtest.current_nda_version_hash on wire = %q, want %q", got, pt.CurrentNDAVersionHash)
	}
	if statusResp.GetApplicant().GetNdaVersionHash() == playtestResp.GetPlaytest().GetCurrentNdaVersionHash() {
		t.Errorf("hashes match — test fixture broken; comparison must be possible")
	}
}

// ---------------- EditPlaytest nda.edit audit -------------------------------

func TestEditPlaytest_NDATextChange_WritesNdaEditAudit(t *testing.T) {
	rig := withNDAStores(t)
	svr, store, audit := rig.svr, rig.playtests, rig.audit

	pt := openPlaytest("nda-edit-audit")
	pt.NDARequired = true
	pt.NDAText = "before text"
	pt.CurrentNDAVersionHash = hashNDA(pt.NDAText)
	store.rows = append(store.rows, pt)

	_, err := svr.EditPlaytest(authCtx(uuid.New()), &pb.EditPlaytestRequest{
		Namespace:   testNamespace,
		PlaytestId:  pt.ID.String(),
		Title:       pt.Title,
		Description: pt.Description,
		NdaRequired: true,
		NdaText:     "after text",
	})
	if err != nil {
		t.Fatalf("EditPlaytest: %v", err)
	}

	if got := len(audit.rows); got != 1 || audit.rows[0].Action != repo.ActionNDAEdit {
		t.Fatalf("audit rows = %d (want 1 nda.edit), got actions=%v", len(audit.rows), auditActions(audit.rows))
	}
	row := audit.rows[0]
	beforePayload := mustJSON(t, row.Before)
	afterPayload := mustJSON(t, row.After)
	if got := beforePayload["ndaText"]; got != "before text" {
		t.Errorf("before.ndaText = %v, want %q", got, "before text")
	}
	if got := afterPayload["ndaText"]; got != "after text" {
		t.Errorf("after.ndaText = %v, want %q", got, "after text")
	}
}

func TestEditPlaytest_NDATextUnchanged_NoAuditRow(t *testing.T) {
	rig := withNDAStores(t)
	svr, store, audit := rig.svr, rig.playtests, rig.audit

	pt := openPlaytest("nda-no-edit-audit")
	pt.NDARequired = true
	pt.NDAText = "same text"
	pt.CurrentNDAVersionHash = hashNDA(pt.NDAText)
	store.rows = append(store.rows, pt)

	_, err := svr.EditPlaytest(authCtx(uuid.New()), &pb.EditPlaytestRequest{
		Namespace:   testNamespace,
		PlaytestId:  pt.ID.String(),
		Title:       "different title",
		Description: pt.Description,
		NdaRequired: true,
		NdaText:     "same text",
	})
	if err != nil {
		t.Fatalf("EditPlaytest: %v", err)
	}
	if got := len(audit.rows); got != 0 {
		t.Errorf("audit rows = %d, want 0 (nda text unchanged)", got)
	}
}

// ---------------- helpers ----------------------------------------------------

func auditActions(rows []*repo.AuditLog) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Action)
	}
	return out
}

func mustJSON(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	if len(raw) == 0 {
		return map[string]any{}
	}
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("json: %v", err)
	}
	return out
}
