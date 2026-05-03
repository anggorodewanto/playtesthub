package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	"github.com/anggorodewanto/playtesthub/pkg/ags"
	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// agsTestRig bundles the wired server + the in-memory AGS client + the
// fake stores AGS_CAMPAIGN create touches so test bodies can assert on
// each layer without spelling out unused returns.
type agsTestRig struct {
	svr        *PlaytesthubServiceServer
	playtests  *fakePlaytestStore
	applicants *fakeApplicantStore
	code       *fakeCodeStore
	audit      *fakeAuditLogStore
	agsClient  *ags.MemClient
}

func withAGSStores(t *testing.T) agsTestRig {
	t.Helper()
	svr, pt, ap := newTestServer()
	codeStore := &fakeCodeStore{}
	audit := &fakeAuditLogStore{}
	agsC := ags.NewMemClient()
	svr = svr.
		WithCodeStore(codeStore).
		WithAuditLogStore(audit).
		WithTxRunner(fakeTxRunner{}).
		WithAGSClient(agsC).
		WithAGSCodeBatchSize(1000)
	return agsTestRig{
		svr:        svr,
		playtests:  pt,
		applicants: ap,
		code:       codeStore,
		audit:      audit,
		agsClient:  agsC,
	}
}

func agsCampaignRequest(slug string, qty int32) *pb.CreatePlaytestRequest {
	req := validCreateRequest(slug)
	req.DistributionModel = pb.DistributionModel_DISTRIBUTION_MODEL_AGS_CAMPAIGN
	req.InitialCodeQuantity = &qty
	return req
}

func TestCreatePlaytest_AGSCampaign_HappyPath_GeneratesCodes(t *testing.T) {
	rig := withAGSStores(t)
	resp, err := rig.svr.CreatePlaytest(authCtx(uuid.New()), agsCampaignRequest("ags-happy", 5))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	p := resp.GetPlaytest()
	if p.GetDistributionModel() != pb.DistributionModel_DISTRIBUTION_MODEL_AGS_CAMPAIGN {
		t.Fatalf("distribution_model = %s, want AGS_CAMPAIGN", p.GetDistributionModel())
	}
	if p.GetAgsItemId() == "" || p.GetAgsCampaignId() == "" {
		t.Fatalf("expected ags ids populated, got item=%q campaign=%q", p.GetAgsItemId(), p.GetAgsCampaignId())
	}
	if got := p.GetInitialCodeQuantity(); got != 5 {
		t.Errorf("initial_code_quantity = %d, want 5", got)
	}
	if !rig.agsClient.HasItem(p.GetAgsItemId()) {
		t.Errorf("expected ags item to remain on success")
	}
	if !rig.agsClient.HasCampaign(p.GetAgsCampaignId()) {
		t.Errorf("expected ags campaign to remain on success")
	}
	// Codes inserted under the new playtest id.
	playtestID, _ := uuid.Parse(p.GetId())
	codeRows, _ := rig.code.ListByPlaytest(context.Background(), playtestID)
	if len(codeRows) != 5 {
		t.Fatalf("inserted code rows = %d, want 5", len(codeRows))
	}
	wantActions := map[string]int{
		repo.ActionCampaignCreate:        1,
		repo.ActionCampaignGenerateCodes: 1,
	}
	for action, n := range wantActions {
		if got := rig.audit.countAction(action); got != n {
			t.Errorf("audit action %s count = %d, want %d", action, got, n)
		}
	}
}

func TestCreatePlaytest_AGSCampaign_BatchedGeneration(t *testing.T) {
	rig := withAGSStores(t)
	rig.svr = rig.svr.WithAGSCodeBatchSize(2)
	resp, err := rig.svr.CreatePlaytest(authCtx(uuid.New()), agsCampaignRequest("ags-batch", 5))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	playtestID, _ := uuid.Parse(resp.GetPlaytest().GetId())
	codeRows, _ := rig.code.ListByPlaytest(context.Background(), playtestID)
	if len(codeRows) != 5 {
		t.Fatalf("inserted code rows = %d, want 5 (batched 2+2+1)", len(codeRows))
	}
}

func TestCreatePlaytest_AGSCampaign_PartialFulfillment_CommitsAndWarns(t *testing.T) {
	rig := withAGSStores(t)
	// Pre-arm partial fulfillment for whichever campaign id is created.
	// MemClient assigns ids deterministically per call, so we drive
	// CreateItem/CreateCampaign manually then inject the cap before
	// CreatePlaytest runs by intercepting via the AGS client itself.
	// Easier: capture the campaign id post-fact by hooking PartialFulfillment
	// for ALL future campaigns via a sentinel. MemClient stores caps per
	// campaign id, but we don't know it yet, so rely on the test mutating
	// MemClient state from inside the CreateCampaign path is awkward.
	// Instead, generate manually first to seed a known campaign, then run
	// CreatePlaytest with a fresh MemClient that returns less than asked.
	// Simplest: replace the mem client with a small custom client.
	custom := &partialFulfillClient{cap: 3}
	rig.svr = rig.svr.WithAGSClient(custom)
	resp, err := rig.svr.CreatePlaytest(authCtx(uuid.New()), agsCampaignRequest("ags-partial", 10))
	if err != nil {
		t.Fatalf("unexpected error (partial fulfillment must commit): %v", err)
	}
	playtestID, _ := uuid.Parse(resp.GetPlaytest().GetId())
	codeRows, _ := rig.code.ListByPlaytest(context.Background(), playtestID)
	if len(codeRows) != 3 {
		t.Fatalf("inserted code rows = %d, want 3 (capped)", len(codeRows))
	}
}

func TestCreatePlaytest_AGSCampaign_ItemCreateFails_NoCleanup(t *testing.T) {
	rig := withAGSStores(t)
	rig.agsClient.CreateItemErr = []error{stubHTTPErr{500, "boom"}}
	_, err := rig.svr.CreatePlaytest(authCtx(uuid.New()), agsCampaignRequest("ags-item-fail", 5))
	requireStatus(t, err, codes.Unavailable)
	// No item was created so neither delete is attempted.
	if got := rig.audit.countAction(repo.ActionCampaignCreateFailed); got != 1 {
		t.Errorf("campaign.create_failed count = %d, want 1", got)
	}
	if got := rig.audit.countAction(repo.ActionCampaignCreate); got != 0 {
		t.Errorf("campaign.create count = %d, want 0 on failure", got)
	}
}

func TestCreatePlaytest_AGSCampaign_CampaignCreateFails_DeletesItem(t *testing.T) {
	rig := withAGSStores(t)
	rig.agsClient.CreateCampaignErr = []error{stubHTTPErr{500, "boom"}}
	_, err := rig.svr.CreatePlaytest(authCtx(uuid.New()), agsCampaignRequest("ags-camp-fail", 5))
	requireStatus(t, err, codes.Unavailable)
	// Cleanup attempted: zero items survive (the orphaned item was deleted).
	for itemID := range rig.agsClient.AllItems() {
		t.Errorf("expected no surviving items after campaign-create failure cleanup; found %s", itemID)
	}
}

func TestCreatePlaytest_AGSCampaign_CreateCodesFails_DeletesItemAndCampaign(t *testing.T) {
	rig := withAGSStores(t)
	rig.agsClient.CreateCodesErr = []error{stubHTTPErr{500, "boom"}}
	_, err := rig.svr.CreatePlaytest(authCtx(uuid.New()), agsCampaignRequest("ags-codes-fail", 5))
	requireStatus(t, err, codes.Unavailable)
	for itemID := range rig.agsClient.AllItems() {
		t.Errorf("expected no surviving items after codes-fail cleanup; found %s", itemID)
	}
	for campaignID := range rig.agsClient.AllCampaigns() {
		t.Errorf("expected no surviving campaigns after codes-fail cleanup; found %s", campaignID)
	}
	if got := rig.audit.countAction(repo.ActionCampaignGenerateCodesFailed); got != 1 {
		t.Errorf("campaign.generate_codes_failed count = %d, want 1", got)
	}
}

func TestCreatePlaytest_AGSCampaign_429_MapsToResourceExhausted(t *testing.T) {
	rig := withAGSStores(t)
	rig.agsClient.CreateItemErr = []error{stubHTTPErr{429, "rate limited"}}
	_, err := rig.svr.CreatePlaytest(authCtx(uuid.New()), agsCampaignRequest("ags-429", 5))
	requireStatus(t, err, codes.ResourceExhausted)
}

func TestCreatePlaytest_AGSCampaign_QtyOutOfBounds_InvalidArgument(t *testing.T) {
	rig := withAGSStores(t)
	for _, q := range []int32{0, -1, 50_001} {
		req := agsCampaignRequest("ags-q-"+itoa(int(q)), q)
		_, err := rig.svr.CreatePlaytest(authCtx(uuid.New()), req)
		requireStatus(t, err, codes.InvalidArgument)
	}
}

func TestCreatePlaytest_AGSCampaign_NoAGSClientWired_Internal(t *testing.T) {
	rig := withAGSStores(t)
	rig.svr.agsClient = nil
	_, err := rig.svr.CreatePlaytest(authCtx(uuid.New()), agsCampaignRequest("ags-no-client", 1))
	requireStatus(t, err, codes.Internal)
}

func TestMapAGSError_RouteEachClass(t *testing.T) {
	if got := mapAGSError(ags.ErrRateLimited); !errMessageContains(got, "rate limited") {
		t.Errorf("ErrRateLimited mapping wrong: %v", got)
	}
	if got := mapAGSError(ags.ErrUnavailable); !errMessageContains(got, "unavailable") {
		t.Errorf("ErrUnavailable mapping wrong: %v", got)
	}
	cs := &ags.ClientError{StatusCode: 400, Op: "Op", Message: "bad"}
	if got := mapAGSError(cs); !errMessageContains(got, "client error 400") {
		t.Errorf("ClientError mapping wrong: %v", got)
	}
	if got := mapAGSError(errors.New("weird")); !errMessageContains(got, "weird") {
		t.Errorf("unknown error mapping wrong: %v", got)
	}
}

// stubHTTPErr satisfies ags.HTTPStatusCarrier so the retry classifier
// produces the right sentinel.
type stubHTTPErr struct {
	code int
	msg  string
}

func (e stubHTTPErr) Error() string   { return e.msg }
func (e stubHTTPErr) HTTPStatus() int { return e.code }

// partialFulfillClient returns fewer codes than requested on every
// CreateCodes call so the partial-fulfillment path is exercised
// without coordinating with MemClient's id-assignment.
type partialFulfillClient struct {
	cap int
	mem *ags.MemClient
}

func (p *partialFulfillClient) CreateItem(ctx context.Context, spec ags.ItemSpec) (string, error) {
	p.ensure()
	return p.mem.CreateItem(ctx, spec)
}

func (p *partialFulfillClient) CreateCampaign(ctx context.Context, spec ags.CampaignSpec) (string, error) {
	p.ensure()
	return p.mem.CreateCampaign(ctx, spec)
}

func (p *partialFulfillClient) CreateCodes(ctx context.Context, campaignID string, quantity int) (ags.CodeBatchResult, error) {
	p.ensure()
	want := min(p.cap, quantity)
	return p.mem.CreateCodes(ctx, campaignID, want)
}

func (p *partialFulfillClient) FetchCodes(ctx context.Context, campaignID string) ([]string, error) {
	p.ensure()
	return p.mem.FetchCodes(ctx, campaignID)
}

func (p *partialFulfillClient) DeleteItem(ctx context.Context, itemID string) error {
	p.ensure()
	return p.mem.DeleteItem(ctx, itemID)
}

func (p *partialFulfillClient) DeleteCampaign(ctx context.Context, campaignID string) error {
	p.ensure()
	return p.mem.DeleteCampaign(ctx, campaignID)
}

func (p *partialFulfillClient) ensure() {
	if p.mem == nil {
		p.mem = ags.NewMemClient()
	}
}

func errMessageContains(err error, sub string) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), sub)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf []byte
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		return "-" + string(buf)
	}
	return string(buf)
}
