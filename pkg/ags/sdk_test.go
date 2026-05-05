package ags_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/AccelByte/accelbyte-go-sdk/platform-sdk/pkg/platformclient/campaign"
	"github.com/AccelByte/accelbyte-go-sdk/platform-sdk/pkg/platformclient/item"
	"github.com/AccelByte/accelbyte-go-sdk/platform-sdk/pkg/platformclientmodels"

	"github.com/anggorodewanto/playtesthub/pkg/ags"
)

// fakeItemSvc + fakeCampaignSvc satisfy the SDK service interfaces
// without touching the platform runtime. Each method consults a queued
// response slot so tests can exercise success, retryable 5xx, 4xx, and
// 429 paths in sequence.
type fakeItemSvc struct {
	createItem func(*item.CreateItemParams) (*platformclientmodels.FullItemInfo, error)
	deleteItem func(*item.DeleteItemParams) error
}

func (f *fakeItemSvc) CreateItemShort(p *item.CreateItemParams) (*platformclientmodels.FullItemInfo, error) {
	return f.createItem(p)
}
func (f *fakeItemSvc) DeleteItemShort(p *item.DeleteItemParams) error { return f.deleteItem(p) }

type fakeCampaignSvc struct {
	createCampaign func(*campaign.CreateCampaignParams) (*platformclientmodels.CampaignInfo, error)
	updateCampaign func(*campaign.UpdateCampaignParams) (*platformclientmodels.CampaignInfo, error)
	getCampaign    func(*campaign.GetCampaignParams) (*platformclientmodels.CampaignInfo, error)
	createCodes    func(*campaign.CreateCodesParams) (*platformclientmodels.CodeCreateResult, error)
	queryCodes     func(*campaign.QueryCodesParams) (*platformclientmodels.CodeInfoPagingSlicedResult, error)
}

func (f *fakeCampaignSvc) CreateCampaignShort(p *campaign.CreateCampaignParams) (*platformclientmodels.CampaignInfo, error) {
	return f.createCampaign(p)
}
func (f *fakeCampaignSvc) UpdateCampaignShort(p *campaign.UpdateCampaignParams) (*platformclientmodels.CampaignInfo, error) {
	return f.updateCampaign(p)
}
func (f *fakeCampaignSvc) GetCampaignShort(p *campaign.GetCampaignParams) (*platformclientmodels.CampaignInfo, error) {
	return f.getCampaign(p)
}
func (f *fakeCampaignSvc) CreateCodesShort(p *campaign.CreateCodesParams) (*platformclientmodels.CodeCreateResult, error) {
	return f.createCodes(p)
}
func (f *fakeCampaignSvc) QueryCodesShort(p *campaign.QueryCodesParams) (*platformclientmodels.CodeInfoPagingSlicedResult, error) {
	return f.queryCodes(p)
}

func newSDKClient(t *testing.T, item *fakeItemSvc, camp *fakeCampaignSvc) *ags.SDKClient {
	t.Helper()
	return ags.NewSDKClient(ags.SDKClientOptions{
		Namespace:   "test-ns",
		StoreID:     "test-store",
		ItemSvc:     item,
		CampaignSvc: camp,
	})
}

func newSDKClientWithLogin(t *testing.T, item *fakeItemSvc, camp *fakeCampaignSvc, login func() error) *ags.SDKClient {
	t.Helper()
	return ags.NewSDKClient(ags.SDKClientOptions{
		Namespace:   "test-ns",
		StoreID:     "test-store",
		ItemSvc:     item,
		CampaignSvc: camp,
		Login:       login,
	})
}

func ptr[T any](v T) *T { return &v }

// sdkBracketErr mimics the SDK typed-response Error format:
// "[METHOD /path][NNN] body".
func sdkBracketErr(method, path string, status int, body string) error {
	return fmt.Errorf("[%s %s][%d] %s", method, path, status, body)
}

func TestSDK_CreateItem_Success(t *testing.T) {
	calls := 0
	svc := &fakeItemSvc{
		createItem: func(p *item.CreateItemParams) (*platformclientmodels.FullItemInfo, error) {
			calls++
			if got, want := p.Namespace, "test-ns"; got != want {
				t.Fatalf("namespace = %q, want %q", got, want)
			}
			if got, want := p.StoreID, "test-store"; got != want {
				t.Fatalf("storeID = %q, want %q", got, want)
			}
			if p.Body == nil {
				t.Fatal("body is nil")
			}
			if got, want := p.Body.BoothName, "Pong"; got != want {
				t.Fatalf("boothName = %q, want %q (AGS rejects null boothName for CODE items)", got, want)
			}
			return &platformclientmodels.FullItemInfo{ItemID: ptr("item-123")}, nil
		},
	}
	c := newSDKClient(t, svc, &fakeCampaignSvc{})
	got, err := c.CreateItem(context.Background(), ags.ItemSpec{Name: "Pong", Description: "test"})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}
	if got != "item-123" {
		t.Fatalf("got %q want item-123", got)
	}
	if calls != 1 {
		t.Fatalf("expected 1 call, got %d", calls)
	}
}

func TestSDK_CreateItem_4xxSurfacesClientError(t *testing.T) {
	svc := &fakeItemSvc{
		createItem: func(*item.CreateItemParams) (*platformclientmodels.FullItemInfo, error) {
			return nil, sdkBracketErr("POST", "/platform/items", 400, "bad request")
		},
	}
	c := newSDKClient(t, svc, &fakeCampaignSvc{})
	_, err := c.CreateItem(context.Background(), ags.ItemSpec{Name: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
	// RetryPolicy.Run + classify is what maps to ErrRateLimited /
	// ClientError; the adapter only needs to surface a status carrier.
	policy := ags.DefaultRetryPolicy()
	mapped := policy.Run(context.Background(), "CreateItem", func(_ context.Context) error { return err })
	if !ags.IsClientError(mapped) {
		t.Fatalf("expected ClientError, got %T: %v", mapped, mapped)
	}
}

func TestSDK_CreateItem_429SurfacesRateLimited(t *testing.T) {
	svc := &fakeItemSvc{
		createItem: func(*item.CreateItemParams) (*platformclientmodels.FullItemInfo, error) {
			return nil, sdkBracketErr("POST", "/platform/items", 429, "rate limited")
		},
	}
	c := newSDKClient(t, svc, &fakeCampaignSvc{})
	_, err := c.CreateItem(context.Background(), ags.ItemSpec{Name: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
	policy := ags.DefaultRetryPolicy()
	mapped := policy.Run(context.Background(), "CreateItem", func(_ context.Context) error { return err })
	if !ags.IsRateLimited(mapped) {
		t.Fatalf("expected ErrRateLimited, got %v", mapped)
	}
}

func TestSDK_CreateItem_5xxRetriedThenUnavailable(t *testing.T) {
	calls := 0
	svc := &fakeItemSvc{
		createItem: func(*item.CreateItemParams) (*platformclientmodels.FullItemInfo, error) {
			calls++
			return nil, sdkBracketErr("POST", "/platform/items", 503, "upstream down")
		},
	}
	c := newSDKClient(t, svc, &fakeCampaignSvc{})

	policy := ags.DefaultRetryPolicy()
	policy.MaxAttempts = 3
	policy.Sleep = func(_ time.Duration) {}
	policy.InitialBackoff = 0

	mapped := policy.Run(context.Background(), "CreateItem", func(ctx context.Context) error {
		_, err := c.CreateItem(ctx, ags.ItemSpec{Name: "x"})
		return err
	})
	if !errors.Is(mapped, ags.ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable after retries, got %v", mapped)
	}
	if calls != 3 {
		t.Fatalf("expected 3 attempts, got %d", calls)
	}
}

func TestSDK_CreateCodes_PagedQueryByBatchName(t *testing.T) {
	createCalls := 0
	queryCalls := 0
	svc := &fakeCampaignSvc{
		createCodes: func(p *campaign.CreateCodesParams) (*platformclientmodels.CodeCreateResult, error) {
			createCalls++
			if p.Body == nil || p.Body.BatchName == "" {
				t.Fatalf("expected unique batch name on CreateCodes, got empty")
			}
			n := int32(3)
			return &platformclientmodels.CodeCreateResult{NumCreated: &n}, nil
		},
		queryCodes: func(p *campaign.QueryCodesParams) (*platformclientmodels.CodeInfoPagingSlicedResult, error) {
			queryCalls++
			if p.BatchName == nil || *p.BatchName == "" {
				t.Fatalf("expected QueryCodes filtered by batch name")
			}
			// First call returns full page (assumed Limit=100), so
			// the adapter pages once. Returning 3 (< 100) ends loop.
			return &platformclientmodels.CodeInfoPagingSlicedResult{
				Data: []*platformclientmodels.CodeInfo{
					{Value: ptr("CODE-A"), BatchName: *p.BatchName},
					{Value: ptr("CODE-B"), BatchName: *p.BatchName},
					{Value: ptr("CODE-C"), BatchName: *p.BatchName},
				},
			}, nil
		},
	}
	c := newSDKClient(t, &fakeItemSvc{}, svc)

	res, err := c.CreateCodes(context.Background(), "camp-1", 3)
	if err != nil {
		t.Fatalf("CreateCodes: %v", err)
	}
	if got := res.Codes; len(got) != 3 || got[0] != "CODE-A" || got[2] != "CODE-C" {
		t.Fatalf("unexpected codes %v", got)
	}
	if res.Requested != 3 {
		t.Fatalf("Requested = %d, want 3", res.Requested)
	}
	if createCalls != 1 || queryCalls != 1 {
		t.Fatalf("createCalls=%d queryCalls=%d", createCalls, queryCalls)
	}
}

func TestSDK_DeleteItem_404IsNoop(t *testing.T) {
	svc := &fakeItemSvc{
		deleteItem: func(*item.DeleteItemParams) error {
			return sdkBracketErr("DELETE", "/platform/items/X", 404, "not found")
		},
	}
	c := newSDKClient(t, svc, &fakeCampaignSvc{})
	if err := c.DeleteItem(context.Background(), "X"); err != nil {
		t.Fatalf("expected nil on 404, got %v", err)
	}
}

func TestSDK_DeleteCampaign_DeactivatesViaUpdate(t *testing.T) {
	getCalls := 0
	updateCalls := 0
	svc := &fakeCampaignSvc{
		getCampaign: func(p *campaign.GetCampaignParams) (*platformclientmodels.CampaignInfo, error) {
			getCalls++
			return &platformclientmodels.CampaignInfo{
				ID:   ptr("camp-1"),
				Name: ptr("Pong-Playtest"),
			}, nil
		},
		updateCampaign: func(p *campaign.UpdateCampaignParams) (*platformclientmodels.CampaignInfo, error) {
			updateCalls++
			if p.Body == nil || p.Body.Status != "INACTIVE" {
				t.Fatalf("expected status INACTIVE, got %+v", p.Body)
			}
			if p.Body.Name == nil || *p.Body.Name != "Pong-Playtest" {
				t.Fatalf("expected Name carried over from GetCampaign, got %v", p.Body.Name)
			}
			return &platformclientmodels.CampaignInfo{ID: ptr("camp-1")}, nil
		},
	}
	c := newSDKClient(t, &fakeItemSvc{}, svc)
	if err := c.DeleteCampaign(context.Background(), "camp-1"); err != nil {
		t.Fatalf("DeleteCampaign: %v", err)
	}
	if getCalls != 1 || updateCalls != 1 {
		t.Fatalf("getCalls=%d updateCalls=%d", getCalls, updateCalls)
	}
}

func TestSDK_DeleteCampaign_404IsNoop(t *testing.T) {
	svc := &fakeCampaignSvc{
		getCampaign: func(*campaign.GetCampaignParams) (*platformclientmodels.CampaignInfo, error) {
			return nil, sdkBracketErr("GET", "/platform/campaigns/X", 404, "not found")
		},
	}
	c := newSDKClient(t, &fakeItemSvc{}, svc)
	if err := c.DeleteCampaign(context.Background(), "X"); err != nil {
		t.Fatalf("expected nil on 404 from GetCampaign, got %v", err)
	}
}

func TestSDK_FetchCodes_PagesUntilShort(t *testing.T) {
	page := 0
	svc := &fakeCampaignSvc{
		queryCodes: func(p *campaign.QueryCodesParams) (*platformclientmodels.CodeInfoPagingSlicedResult, error) {
			page++
			// Page 1: 100 codes (full). Page 2: 5 codes (short → stop).
			if page == 1 {
				codes := make([]*platformclientmodels.CodeInfo, 100)
				for i := range codes {
					v := fmt.Sprintf("PAGE1-%03d", i)
					codes[i] = &platformclientmodels.CodeInfo{Value: ptr(v)}
				}
				return &platformclientmodels.CodeInfoPagingSlicedResult{Data: codes}, nil
			}
			codes := make([]*platformclientmodels.CodeInfo, 5)
			for i := range codes {
				v := fmt.Sprintf("PAGE2-%d", i)
				codes[i] = &platformclientmodels.CodeInfo{Value: ptr(v)}
			}
			return &platformclientmodels.CodeInfoPagingSlicedResult{Data: codes}, nil
		},
	}
	c := newSDKClient(t, &fakeItemSvc{}, svc)
	got, err := c.FetchCodes(context.Background(), "camp-1")
	if err != nil {
		t.Fatalf("FetchCodes: %v", err)
	}
	if len(got) != 105 {
		t.Fatalf("got %d codes, want 105", len(got))
	}
	if page != 2 {
		t.Fatalf("expected 2 pages, saw %d", page)
	}
}

// TestSDK_CreateItem_401TriggersReloginAndRetry covers the platform-side
// token-expiry path: AGS returns 401 once, the SDKClient invokes the
// configured Login closure, and the second call succeeds. Mirrors the
// production failure mode where the SDK's process-global auto-refresh
// goroutine is claimed by the inbound auth surface and the platform
// TokenRepository never gets refreshed on its own.
func TestSDK_CreateItem_401TriggersReloginAndRetry(t *testing.T) {
	calls := 0
	svc := &fakeItemSvc{
		createItem: func(*item.CreateItemParams) (*platformclientmodels.FullItemInfo, error) {
			calls++
			if calls == 1 {
				return nil, sdkBracketErr("POST", "/platform/items", 401, `{"errorCode":20011,"errorMessage":"token is expired"}`)
			}
			return &platformclientmodels.FullItemInfo{ItemID: ptr("item-after-refresh")}, nil
		},
	}
	logins := 0
	c := newSDKClientWithLogin(t, svc, &fakeCampaignSvc{}, func() error {
		logins++
		return nil
	})
	got, err := c.CreateItem(context.Background(), ags.ItemSpec{Name: "Pong"})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}
	if got != "item-after-refresh" {
		t.Fatalf("got %q, want item-after-refresh", got)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls (initial + retry), got %d", calls)
	}
	if logins != 1 {
		t.Fatalf("expected 1 login, got %d", logins)
	}
}

// TestSDK_CreateItem_401WithoutLoginSurfaces401 checks that when no
// Login closure is wired (legacy callers / tests), 401 is surfaced
// without infinite-looping and with the correct status preserved.
func TestSDK_CreateItem_401WithoutLoginSurfaces401(t *testing.T) {
	calls := 0
	svc := &fakeItemSvc{
		createItem: func(*item.CreateItemParams) (*platformclientmodels.FullItemInfo, error) {
			calls++
			return nil, sdkBracketErr("POST", "/platform/items", 401, "expired")
		},
	}
	c := newSDKClient(t, svc, &fakeCampaignSvc{})
	_, err := c.CreateItem(context.Background(), ags.ItemSpec{Name: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Fatalf("expected 1 call (no retry), got %d", calls)
	}
	var carrier interface{ HTTPStatus() int }
	if !errors.As(err, &carrier) || carrier.HTTPStatus() != 401 {
		t.Fatalf("expected HTTPStatus()=401, got %v", err)
	}
}
