package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// fakeCodeStore is the M2-phase-5/6 in-memory CodeStore. Covers the
// upload surface (UploadCodes / GetCodePool) and — from phase 6 — the
// approve-flow trio Reserve / FencedFinalize / Reclaim. The fake does
// not actually use the supplied Querier; tests that need real
// transactional semantics use the testcontainers-postgres harness in
// pkg/repo. The fake's job is to make every service-layer code path
// reachable.
type fakeCodeStore struct {
	rows         []*repo.Code
	uploadCalls  int
	uploadErr    error
	listErr      error
	uploadValues []string

	// Approve-flow knobs (phase 6).
	reserveErr           error // when set, Reserve returns this verbatim
	finalizeRowsOverride *int64
	reclaimReleased      int64
	reclaimErr           error
}

func (f *fakeCodeStore) BulkInsert(_ context.Context, playtestID uuid.UUID, values []string) (int, error) {
	for _, v := range values {
		f.rows = append(f.rows, &repo.Code{
			ID:         uuid.New(),
			PlaytestID: playtestID,
			Value:      v,
			State:      repo.CodeStateUnused,
			CreatedAt:  time.Now(),
		})
	}
	return len(values), nil
}

func (f *fakeCodeStore) BulkInsertCSV(ctx context.Context, playtestID uuid.UUID, values []string) (int, error) {
	return f.BulkInsert(ctx, playtestID, values)
}

func (f *fakeCodeStore) BulkInsertGenerated(ctx context.Context, playtestID uuid.UUID, values []string) (int, error) {
	return f.BulkInsert(ctx, playtestID, values)
}

func (f *fakeCodeStore) CountByState(_ context.Context, playtestID uuid.UUID) (map[string]int, error) {
	out := map[string]int{}
	for _, r := range f.rows {
		if r.PlaytestID != playtestID {
			continue
		}
		out[r.State]++
	}
	return out, nil
}

func (f *fakeCodeStore) GetByID(_ context.Context, id uuid.UUID) (*repo.Code, error) {
	for _, r := range f.rows {
		if r.ID == id {
			clone := *r
			return &clone, nil
		}
	}
	return nil, repo.ErrNotFound
}

func (f *fakeCodeStore) ListByPlaytest(_ context.Context, playtestID uuid.UUID) ([]*repo.Code, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]*repo.Code, 0)
	for _, r := range f.rows {
		if r.PlaytestID == playtestID {
			clone := *r
			out = append(out, &clone)
		}
	}
	return out, nil
}

func (f *fakeCodeStore) UploadAtomic(_ context.Context, playtestID uuid.UUID, values []string) (int, []string, error) {
	f.uploadCalls++
	f.uploadValues = append([]string(nil), values...)
	if f.uploadErr != nil {
		return 0, nil, f.uploadErr
	}
	dups := make([]string, 0)
	for _, v := range values {
		for _, existing := range f.rows {
			if existing.PlaytestID == playtestID && existing.Value == v {
				dups = append(dups, v)
				break
			}
		}
	}
	if len(dups) > 0 {
		return 0, dups, nil
	}
	for _, v := range values {
		f.rows = append(f.rows, &repo.Code{
			ID:         uuid.New(),
			PlaytestID: playtestID,
			Value:      v,
			State:      repo.CodeStateUnused,
			CreatedAt:  time.Now(),
		})
	}
	return len(values), nil, nil
}

// Reserve flips the oldest UNUSED row for the playtest to RESERVED,
// stamping reserved_by and reserved_at. Returns ErrPoolEmpty when no
// UNUSED rows remain — same sentinel the real PgCodeStore raises.
func (f *fakeCodeStore) Reserve(_ context.Context, _ repo.Querier, playtestID, userID uuid.UUID) (*repo.Code, error) {
	if f.reserveErr != nil {
		return nil, f.reserveErr
	}
	for _, r := range f.rows {
		if r.PlaytestID != playtestID {
			continue
		}
		if r.State != repo.CodeStateUnused {
			continue
		}
		now := time.Now()
		r.State = repo.CodeStateReserved
		r.ReservedBy = &userID
		r.ReservedAt = &now
		clone := *r
		return &clone, nil
	}
	return nil, repo.ErrPoolEmpty
}

// FencedFinalize matches the canonical schema.md fenced UPDATE: a row
// flips RESERVED → GRANTED only if the (codeID, userID, reservedAt)
// triple still matches the stored row. The override knob lets tests
// force the 0-row case explicitly without crafting a reclaim race.
func (f *fakeCodeStore) FencedFinalize(_ context.Context, _ repo.Querier, codeID, userID uuid.UUID, originalReservedAt time.Time) (int64, error) {
	if f.finalizeRowsOverride != nil {
		return *f.finalizeRowsOverride, nil
	}
	for _, r := range f.rows {
		if r.ID != codeID || r.State != repo.CodeStateReserved {
			continue
		}
		if r.ReservedBy == nil || *r.ReservedBy != userID {
			continue
		}
		if r.ReservedAt == nil || !r.ReservedAt.Equal(originalReservedAt) {
			continue
		}
		now := time.Now()
		r.State = repo.CodeStateGranted
		r.GrantedAt = &now
		return 1, nil
	}
	return 0, nil
}

// Reclaim is a stub returning the configured count; phase 6 only
// exercises it through the reclaim worker tests, which use the real
// repo.LeaderStore + this fake CodeStore so the count plumbing reaches
// the log line under assertion.
func (f *fakeCodeStore) Reclaim(_ context.Context, _ time.Duration) (int64, error) {
	if f.reclaimErr != nil {
		return 0, f.reclaimErr
	}
	return f.reclaimReleased, nil
}

// codeTestRig bundles the test server + every fake the upload flow uses
// so test bodies do not have to spell out unused returns.
type codeTestRig struct {
	svr        *PlaytesthubServiceServer
	playtests  *fakePlaytestStore
	applicants *fakeApplicantStore
	code       *fakeCodeStore
	audit      *fakeAuditLogStore
}

func withCodeStores(t *testing.T) codeTestRig {
	t.Helper()
	svr, pt, ap := newTestServer()
	code := &fakeCodeStore{}
	audit := &fakeAuditLogStore{}
	svr = svr.WithCodeStore(code).WithAuditLogStore(audit)
	return codeTestRig{svr: svr, playtests: pt, applicants: ap, code: code, audit: audit}
}

func steamKeysPlaytest(slug string) *repo.Playtest {
	pt := openPlaytest(slug)
	pt.DistributionModel = distModelSteamKeys
	return pt
}

// ---------------- UploadCodes ----------------------------------------------

func TestUploadCodes_HappyPath_InsertsAndAuditsCodeUpload(t *testing.T) {
	rig := withCodeStores(t)
	pt := steamKeysPlaytest("upload-happy")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	csv := []byte("KEY-A1\nKEY-B2\nKEY-C3\n")
	resp, err := rig.svr.UploadCodes(authCtx(uuid.New()), &pb.UploadCodesRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		CsvContent: csv,
		Filename:   "keys.csv",
	})
	if err != nil {
		t.Fatalf("UploadCodes: %v", err)
	}
	if resp.GetInserted() != 3 {
		t.Errorf("inserted = %d, want 3", resp.GetInserted())
	}
	if got := len(resp.GetRejections()); got != 0 {
		t.Errorf("rejections = %d, want 0", got)
	}
	if rig.code.uploadCalls != 1 {
		t.Errorf("UploadAtomic call count = %d, want 1", rig.code.uploadCalls)
	}
	if got := len(rig.audit.rows); got != 1 || rig.audit.rows[0].Action != repo.ActionCodeUpload {
		t.Fatalf("audit rows = %d (want 1 code.upload), got actions=%v", len(rig.audit.rows), auditActions(rig.audit.rows))
	}
}

// PRD §6 redaction rule + schema.md L51: code.upload audit row must
// NOT carry raw code values. Counted explicitly because the audit
// payload is a free-form JSONB blob and a regression that adds a
// `value` field would not break any other test.
func TestUploadCodes_AuditPayloadCarriesNoRawValues(t *testing.T) {
	rig := withCodeStores(t)
	pt := steamKeysPlaytest("upload-redaction")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	canary := "REDACTION-CANARY-KEY"
	csv := []byte(canary + "\n")
	if _, err := rig.svr.UploadCodes(authCtx(uuid.New()), &pb.UploadCodesRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		CsvContent: csv,
		Filename:   "f.csv",
	}); err != nil {
		t.Fatalf("UploadCodes: %v", err)
	}
	if len(rig.audit.rows) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(rig.audit.rows))
	}
	if strings.Contains(string(rig.audit.rows[0].After), canary) {
		t.Errorf("code.upload audit payload leaked raw value: %s", rig.audit.rows[0].After)
	}
}

// PRD §4.3 file-size cap: file >10MB rejects without DB roundtrip and
// emits code.upload_rejected with reason="size_exceeded".
func TestUploadCodes_FileSizeExceededRejectsBeforeParse(t *testing.T) {
	rig := withCodeStores(t)
	pt := steamKeysPlaytest("upload-size")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	csv := make([]byte, 10*1024*1024+1)
	for i := range csv {
		csv[i] = 'A'
	}
	_, err := rig.svr.UploadCodes(authCtx(uuid.New()), &pb.UploadCodesRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		CsvContent: csv,
	})
	requireStatus(t, err, codes.InvalidArgument)
	if rig.code.uploadCalls != 0 {
		t.Errorf("UploadAtomic was called for oversized file (calls = %d, want 0)", rig.code.uploadCalls)
	}
	if got := uploadRejectedActions(rig.audit.rows); len(got) != 1 || got[0] != "size_exceeded" {
		t.Errorf("audit code.upload_rejected reasons = %v, want [size_exceeded]", got)
	}
}

// PRD §4.3: non-UTF-8 → InvalidArgument.
func TestUploadCodes_NonUTF8Rejects(t *testing.T) {
	rig := withCodeStores(t)
	pt := steamKeysPlaytest("upload-bad-encoding")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	// 0xFF is not valid UTF-8.
	_, err := rig.svr.UploadCodes(authCtx(uuid.New()), &pb.UploadCodesRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		CsvContent: []byte{0xFF, 0xFE, 'A'},
	})
	requireStatus(t, err, codes.InvalidArgument)
	if got := uploadRejectedActions(rig.audit.rows); len(got) != 1 || got[0] != "non_utf8" {
		t.Errorf("audit code.upload_rejected reasons = %v, want [non_utf8]", got)
	}
}

// PRD §4.3: row count >50,000 rejects (count_exceeded).
func TestUploadCodes_TooManyCodesRejects(t *testing.T) {
	rig := withCodeStores(t)
	pt := steamKeysPlaytest("upload-count")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	// 50001 valid lines.
	var b strings.Builder
	for i := 0; i < 50_001; i++ {
		b.WriteString("KEY")
		// width-padded so each line is unique
		b.WriteString(strings.Repeat("0", 5))
		b.WriteString("\n")
	}
	_, err := rig.svr.UploadCodes(authCtx(uuid.New()), &pb.UploadCodesRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		CsvContent: []byte(b.String()),
	})
	requireStatus(t, err, codes.InvalidArgument)
	if rig.code.uploadCalls != 0 {
		t.Errorf("UploadAtomic was called for oversize-count file (calls = %d)", rig.code.uploadCalls)
	}
	if got := uploadRejectedActions(rig.audit.rows); len(got) != 1 || got[0] != "count_exceeded" {
		t.Errorf("audit code.upload_rejected reasons = %v, want [count_exceeded]", got)
	}
}

// PRD §4.3 charset/length per-line rejects: response carries per-line
// rejections, code is OK (200 with structured rejections in body).
// Whole-file reject — zero codes inserted.
func TestUploadCodes_PerLineCharsetAndLengthRejections(t *testing.T) {
	rig := withCodeStores(t)
	pt := steamKeysPlaytest("upload-perline")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	csv := []byte(strings.Join([]string{
		"GOODKEY-1",              // line 1: ok
		"BAD KEY",                // line 2: charset (space)
		"",                       // line 3: empty
		strings.Repeat("X", 129), // line 4: length (>128)
		"OK-KEY/2",               // line 5: charset (/)
	}, "\n"))

	resp, err := rig.svr.UploadCodes(authCtx(uuid.New()), &pb.UploadCodesRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		CsvContent: csv,
		Filename:   "mixed.csv",
	})
	if err != nil {
		t.Fatalf("UploadCodes: %v", err)
	}
	if resp.GetInserted() != 0 {
		t.Errorf("inserted = %d, want 0 (whole-file reject)", resp.GetInserted())
	}
	if got := len(resp.GetRejections()); got != 4 {
		t.Fatalf("rejections = %d, want 4 (lines 2/3/4/5)", got)
	}

	want := map[int]string{
		2: "charset_violation",
		3: "empty_line",
		4: "length_violation",
		5: "charset_violation",
	}
	for _, r := range resp.GetRejections() {
		if want[int(r.GetLineNumber())] != r.GetReason() {
			t.Errorf("line %d reason = %q, want %q", r.GetLineNumber(), r.GetReason(), want[int(r.GetLineNumber())])
		}
	}
	if rig.code.uploadCalls != 0 {
		t.Errorf("UploadAtomic should not run when parse rejections exist (calls = %d)", rig.code.uploadCalls)
	}
	// audit records one rejected row, with dominant reason = charset_violation.
	if got := uploadRejectedActions(rig.audit.rows); len(got) != 1 || got[0] != "charset_violation" {
		t.Errorf("audit code.upload_rejected reasons = %v, want [charset_violation]", got)
	}
}

// PRD §4.3 dedup-against-DB: the per-line rejections list back-maps
// duplicates to their CSV line numbers; whole file rejected.
func TestUploadCodes_DuplicateAgainstDBRejectsWithLineNumbers(t *testing.T) {
	rig := withCodeStores(t)
	pt := steamKeysPlaytest("upload-dedup-db")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	// Pre-seed an existing code via the fake.
	rig.code.rows = append(rig.code.rows, &repo.Code{
		ID:         uuid.New(),
		PlaytestID: pt.ID,
		Value:      "OLD-KEY",
		State:      repo.CodeStateUnused,
		CreatedAt:  time.Now(),
	})

	csv := []byte("NEW-A\nOLD-KEY\nNEW-B\n")
	resp, err := rig.svr.UploadCodes(authCtx(uuid.New()), &pb.UploadCodesRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		CsvContent: csv,
		Filename:   "dup.csv",
	})
	if err != nil {
		t.Fatalf("UploadCodes: %v", err)
	}
	if resp.GetInserted() != 0 {
		t.Errorf("inserted = %d, want 0", resp.GetInserted())
	}
	if got := len(resp.GetRejections()); got != 1 {
		t.Fatalf("rejections = %d, want 1", got)
	}
	r := resp.GetRejections()[0]
	if r.GetLineNumber() != 2 || r.GetReason() != "duplicate_in_db" || r.GetValue() != "OLD-KEY" {
		t.Errorf("rejection = %+v, want line=2 reason=duplicate_in_db value=OLD-KEY", r)
	}
	if got := uploadRejectedActions(rig.audit.rows); len(got) != 1 || got[0] != "duplicate_in_db" {
		t.Errorf("audit code.upload_rejected reasons = %v, want [duplicate_in_db]", got)
	}
}

// PRD §4.3 BOM stripping: a UTF-8 BOM at the start of the file does
// not cause a charset rejection on line 1.
func TestUploadCodes_StripsLeadingBOM(t *testing.T) {
	rig := withCodeStores(t)
	pt := steamKeysPlaytest("upload-bom")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	csv := append([]byte{0xEF, 0xBB, 0xBF}, []byte("KEY-1\nKEY-2\n")...)
	resp, err := rig.svr.UploadCodes(authCtx(uuid.New()), &pb.UploadCodesRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		CsvContent: csv,
	})
	if err != nil {
		t.Fatalf("UploadCodes: %v", err)
	}
	if resp.GetInserted() != 2 || len(resp.GetRejections()) != 0 {
		t.Errorf("inserted=%d rejections=%d, want 2/0 (BOM ignored)", resp.GetInserted(), len(resp.GetRejections()))
	}
}

// AGS_CAMPAIGN playtests reject UploadCodes — codes come via TopUp /
// Sync (PRD §4.6).
func TestUploadCodes_RejectsAGSCampaignPlaytest(t *testing.T) {
	rig := withCodeStores(t)
	pt := steamKeysPlaytest("upload-wrong-model")
	pt.DistributionModel = distModelAGSCampaign
	rig.playtests.rows = append(rig.playtests.rows, pt)

	_, err := rig.svr.UploadCodes(authCtx(uuid.New()), &pb.UploadCodesRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		CsvContent: []byte("KEY-1\n"),
	})
	requireStatus(t, err, codes.FailedPrecondition)
	if rig.code.uploadCalls != 0 {
		t.Errorf("UploadAtomic should not run for AGS_CAMPAIGN playtests (calls = %d)", rig.code.uploadCalls)
	}
}

// Soft-deleted playtest hides as NotFound — same visibility rule as
// EditPlaytest (PRD §5.1).
func TestUploadCodes_SoftDeletedPlaytestNotFound(t *testing.T) {
	rig := withCodeStores(t)
	pt := steamKeysPlaytest("upload-deleted")
	now := time.Now()
	pt.DeletedAt = &now
	rig.playtests.rows = append(rig.playtests.rows, pt)

	_, err := rig.svr.UploadCodes(authCtx(uuid.New()), &pb.UploadCodesRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
		CsvContent: []byte("KEY-1\n"),
	})
	requireStatus(t, err, codes.NotFound)
}

func TestUploadCodes_RequiresActor(t *testing.T) {
	rig := withCodeStores(t)
	_, err := rig.svr.UploadCodes(context.Background(), &pb.UploadCodesRequest{
		Namespace:  testNamespace,
		PlaytestId: uuid.New().String(),
		CsvContent: []byte("KEY-1\n"),
	})
	requireStatus(t, err, codes.Unauthenticated)
}

func TestUploadCodes_NamespaceMismatchPermissionDenied(t *testing.T) {
	rig := withCodeStores(t)
	_, err := rig.svr.UploadCodes(authCtx(uuid.New()), &pb.UploadCodesRequest{
		Namespace:  "wrong-namespace",
		PlaytestId: uuid.New().String(),
		CsvContent: []byte("KEY-1\n"),
	})
	requireStatus(t, err, codes.PermissionDenied)
}

// ---------------- GetCodePool ----------------------------------------------

func TestGetCodePool_HappyPathReturnsStatsAndCodes(t *testing.T) {
	rig := withCodeStores(t)
	pt := steamKeysPlaytest("pool-happy")
	rig.playtests.rows = append(rig.playtests.rows, pt)

	// Seed via UploadAtomic so created_at is populated.
	if _, _, err := rig.code.UploadAtomic(context.Background(), pt.ID, []string{"K1", "K2", "K3"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Flip one row to GRANTED to exercise the stats counter.
	rig.code.rows[0].State = repo.CodeStateGranted

	resp, err := rig.svr.GetCodePool(authCtx(uuid.New()), &pb.GetCodePoolRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
	})
	if err != nil {
		t.Fatalf("GetCodePool: %v", err)
	}
	stats := resp.GetStats()
	if stats.GetTotal() != 3 || stats.GetUnused() != 2 || stats.GetGranted() != 1 {
		t.Errorf("stats = %+v, want total=3 unused=2 granted=1", stats)
	}
	if got := len(resp.GetCodes()); got != 3 {
		t.Errorf("codes = %d, want 3", got)
	}
	// Admin response carries raw values (PRD §5.7).
	values := map[string]bool{}
	for _, c := range resp.GetCodes() {
		values[c.GetValue()] = true
	}
	if !values["K1"] || !values["K2"] || !values["K3"] {
		t.Errorf("admin response missing raw values: got %+v", values)
	}
}

func TestGetCodePool_RequiresActor(t *testing.T) {
	rig := withCodeStores(t)
	_, err := rig.svr.GetCodePool(context.Background(), &pb.GetCodePoolRequest{
		Namespace:  testNamespace,
		PlaytestId: uuid.New().String(),
	})
	requireStatus(t, err, codes.Unauthenticated)
}

func TestGetCodePool_NotFoundOnSoftDeleted(t *testing.T) {
	rig := withCodeStores(t)
	pt := steamKeysPlaytest("pool-deleted")
	now := time.Now()
	pt.DeletedAt = &now
	rig.playtests.rows = append(rig.playtests.rows, pt)

	_, err := rig.svr.GetCodePool(authCtx(uuid.New()), &pb.GetCodePoolRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
	})
	requireStatus(t, err, codes.NotFound)
}

func TestGetCodePool_StoreNotWiredInternal(t *testing.T) {
	svr, store, _ := newTestServer()
	pt := steamKeysPlaytest("pool-no-store")
	store.rows = append(store.rows, pt)

	_, err := svr.GetCodePool(authCtx(uuid.New()), &pb.GetCodePoolRequest{
		Namespace:  testNamespace,
		PlaytestId: pt.ID.String(),
	})
	requireStatus(t, err, codes.Internal)
}

// uploadRejectedActions extracts the reason field from each
// code.upload_rejected audit row, so tests can assert on the audit
// trail without unmarshalling JSON.
func uploadRejectedActions(rows []*repo.AuditLog) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.Action != repo.ActionCodeUploadRejected {
			continue
		}
		// Cheap parse — the JSONB shape is `{filename, reason, rowCount}`.
		// Tests only need the reason; build a tiny ad-hoc decoder.
		needle := []byte(`"reason":"`)
		idx := indexOf(r.After, needle)
		if idx < 0 {
			out = append(out, "")
			continue
		}
		end := idx + len(needle)
		stop := end
		for stop < len(r.After) && r.After[stop] != '"' {
			stop++
		}
		out = append(out, string(r.After[end:stop]))
	}
	return out
}

func indexOf(haystack, needle []byte) int {
	if len(needle) == 0 {
		return 0
	}
outer:
	for i := 0; i+len(needle) <= len(haystack); i++ {
		for j := range needle {
			if haystack[i+j] != needle[j] {
				continue outer
			}
		}
		return i
	}
	return -1
}
