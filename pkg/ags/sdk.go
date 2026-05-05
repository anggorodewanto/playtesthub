package ags

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/AccelByte/accelbyte-go-sdk/platform-sdk/pkg/platformclient/campaign"
	"github.com/AccelByte/accelbyte-go-sdk/platform-sdk/pkg/platformclient/category"
	"github.com/AccelByte/accelbyte-go-sdk/platform-sdk/pkg/platformclient/currency"
	"github.com/AccelByte/accelbyte-go-sdk/platform-sdk/pkg/platformclient/item"
	"github.com/AccelByte/accelbyte-go-sdk/platform-sdk/pkg/platformclient/store"
	"github.com/AccelByte/accelbyte-go-sdk/platform-sdk/pkg/platformclientmodels"
	"github.com/AccelByte/accelbyte-go-sdk/services-api/pkg/repository"
	"github.com/AccelByte/accelbyte-go-sdk/services-api/pkg/service/platform"
)

// SDKClient is the production Client implementation. It wraps the
// AccelByte platform SDK to provision Items + Campaigns + Codes against
// a real AGS namespace per docs/ags-failure-modes.md M2 sub-cap matrix.
//
// All SDK errors are converted to types satisfying HTTPStatusCarrier so
// RetryPolicy.Run classifies them per the same rules MemClient and the
// service-layer mappers already use:
//   - HTTP 429 → ErrRateLimited
//   - HTTP 4xx → *ClientError
//   - HTTP 5xx / timeout → ErrUnavailable (after retry exhaustion)
//
// Token plumbing: each *Service holds the same TokenRepository the
// bootapp's OAuth20Service.LoginClient seeded. The SDK's background
// auto-refresh goroutine is process-global (sync.Once) and gets
// claimed by whichever LoginClient ran first (in our case, the inbound
// auth surface), so the platform-side TokenRepo is never auto-refreshed.
// We compensate with a login-on-401 retry: every outbound call that
// returns HTTP 401 triggers Login() and one retry. Login is supplied by
// bootapp via SDKClientOptions.Login (closes over the same OAuth20Service
// and TokenRepository the platform service uses).
// currencyTypeVirtual is AGS's "non-real-money" currency type. Playtest
// items are non-purchasable so the RegionData entry uses VIRTUAL even
// when the existing currency happens to be REAL — except Bootstrap
// reuses an existing REAL currency only as a last resort, preferring a
// VIRTUAL match.
const currencyTypeVirtual = "VIRTUAL"

type SDKClient struct {
	namespace string

	// mu guards storeID + regionCurrencyCode + regionCurrencyType +
	// regionCode. Bootstrap mutates these after auto-discovery; every
	// outbound call reads them under the same mutex so a Bootstrap
	// racing a CreateItem can't see a torn pair (e.g. resolved storeID
	// without resolved currency).
	mu                 sync.RWMutex
	storeID            string
	regionCurrencyCode string
	regionCurrencyType string
	regionCode         string

	itemSvc     ItemService
	campaignSvc CampaignService
	storeSvc    StoreService
	categorySvc CategoryService
	currencySvc CurrencyService
	login       func() error
}

// ItemService is the subset of platform.ItemService SDKClient depends
// on. The interface keeps the SDKClient testable without standing up
// the whole platform-sdk runtime.
type ItemService interface {
	CreateItemShort(input *item.CreateItemParams) (*platformclientmodels.FullItemInfo, error)
	DeleteItemShort(input *item.DeleteItemParams) error
}

// CampaignService is the subset of platform.CampaignService SDKClient
// depends on. Same testability rationale as ItemService.
type CampaignService interface {
	CreateCampaignShort(input *campaign.CreateCampaignParams) (*platformclientmodels.CampaignInfo, error)
	UpdateCampaignShort(input *campaign.UpdateCampaignParams) (*platformclientmodels.CampaignInfo, error)
	GetCampaignShort(input *campaign.GetCampaignParams) (*platformclientmodels.CampaignInfo, error)
	CreateCodesShort(input *campaign.CreateCodesParams) (*platformclientmodels.CodeCreateResult, error)
	QueryCodesShort(input *campaign.QueryCodesParams) (*platformclientmodels.CodeInfoPagingSlicedResult, error)
}

// StoreService is the subset of platform.StoreService SDKClient.Bootstrap
// depends on (list + create). Implementations adapt the SDK service.
type StoreService interface {
	ListStoresShort(input *store.ListStoresParams) ([]*platformclientmodels.StoreInfo, error)
	CreateStoreShort(input *store.CreateStoreParams) (*platformclientmodels.StoreInfo, error)
}

// CategoryService is the subset of platform.CategoryService Bootstrap
// depends on (get + create). GetCategory's 404 is the "create me" signal.
type CategoryService interface {
	GetCategoryShort(input *category.GetCategoryParams) (*platformclientmodels.FullCategoryInfo, error)
	CreateCategoryShort(input *category.CreateCategoryParams) (*platformclientmodels.FullCategoryInfo, error)
}

// CurrencyService is the subset of platform.CurrencyService Bootstrap
// depends on (list + create). Bootstrap prefers any existing VIRTUAL
// currency and only creates the fallback when none exist.
type CurrencyService interface {
	ListCurrenciesShort(input *currency.ListCurrenciesParams) ([]*platformclientmodels.CurrencyInfo, error)
	CreateCurrencyShort(input *currency.CreateCurrencyParams) (*platformclientmodels.CurrencyInfo, error)
}

// SDKClientOptions configures a fresh SDKClient.
type SDKClientOptions struct {
	Namespace string
	// StoreID is optional. When empty, Bootstrap discovers an existing
	// store by title (or creates one); subsequent CreateItem calls use
	// the resolved id. Operators who want explicit control can still
	// set this from AGS_STORE_ID.
	StoreID     string
	ItemSvc     ItemService
	CampaignSvc CampaignService
	// StoreSvc / CategorySvc / CurrencySvc are required by Bootstrap.
	// They may be nil at construction time; calls that need them
	// surface ClientError(500) so wiring regressions fail loudly.
	StoreSvc    StoreService
	CategorySvc CategoryService
	CurrencySvc CurrencyService
	// Login re-runs the client-credentials grant and stores the new
	// token in the same TokenRepository the Item/Campaign services
	// consume. Optional — when nil, 401 responses surface as ClientError
	// without a retry attempt.
	Login func() error
	// RegionCurrencyCode / RegionCurrencyType / RegionCode populate the
	// RegionData entry CreateItem sends so AGS's "Default region
	// required" validation passes (errorCode 30022). When
	// RegionCurrencyCode is empty, Bootstrap auto-detects an existing
	// VIRTUAL currency (or creates the fallback) and stores the
	// resolved value here; CreateItem then includes RegionData.
	// See docs/engineering.md "AGS namespace prerequisites".
	RegionCurrencyCode string
	RegionCurrencyType string
	RegionCode         string
}

// NewSDKClient constructs an SDKClient. Only Namespace is required; a
// missing namespace is a programmer error and panics. StoreID and
// RegionCurrencyCode are auto-resolved by Bootstrap when left empty
// (see docs/STATUS.md M2 phase 16).
func NewSDKClient(opts SDKClientOptions) *SDKClient {
	if opts.Namespace == "" {
		panic("ags.NewSDKClient: Namespace is required")
	}
	regionCode := opts.RegionCode
	if regionCode == "" {
		regionCode = "US"
	}
	regionCurrencyType := opts.RegionCurrencyType
	if regionCurrencyType == "" {
		regionCurrencyType = currencyTypeVirtual
	}
	return &SDKClient{
		namespace:          opts.Namespace,
		storeID:            opts.StoreID,
		itemSvc:            opts.ItemSvc,
		campaignSvc:        opts.CampaignSvc,
		storeSvc:           opts.StoreSvc,
		categorySvc:        opts.CategorySvc,
		currencySvc:        opts.CurrencySvc,
		login:              opts.Login,
		regionCurrencyCode: opts.RegionCurrencyCode,
		regionCurrencyType: regionCurrencyType,
		regionCode:         regionCode,
	}
}

// NewPlatformItemService adapts the SDK's *platform.ItemService into
// the narrower ItemService interface. Lives here so bootapp doesn't
// import platformclientmodels directly.
func NewPlatformItemService(svc *platform.ItemService) ItemService {
	return &platformItemService{svc: svc}
}

// NewPlatformCampaignService adapts the SDK's *platform.CampaignService
// into the narrower CampaignService interface.
func NewPlatformCampaignService(svc *platform.CampaignService) CampaignService {
	return &platformCampaignService{svc: svc}
}

// NewPlatformStoreService adapts the SDK's *platform.StoreService into
// the narrower StoreService interface required by Bootstrap.
func NewPlatformStoreService(svc *platform.StoreService) StoreService {
	return &platformStoreService{svc: svc}
}

// NewPlatformCategoryService adapts the SDK's *platform.CategoryService
// into the narrower CategoryService interface.
func NewPlatformCategoryService(svc *platform.CategoryService) CategoryService {
	return &platformCategoryService{svc: svc}
}

// NewPlatformCurrencyService adapts the SDK's *platform.CurrencyService
// into the narrower CurrencyService interface.
func NewPlatformCurrencyService(svc *platform.CurrencyService) CurrencyService {
	return &platformCurrencyService{svc: svc}
}

type platformItemService struct{ svc *platform.ItemService }

func (p *platformItemService) CreateItemShort(input *item.CreateItemParams) (*platformclientmodels.FullItemInfo, error) {
	return p.svc.CreateItemShort(input)
}

func (p *platformItemService) DeleteItemShort(input *item.DeleteItemParams) error {
	return p.svc.DeleteItemShort(input)
}

type platformCampaignService struct{ svc *platform.CampaignService }

func (p *platformCampaignService) CreateCampaignShort(input *campaign.CreateCampaignParams) (*platformclientmodels.CampaignInfo, error) {
	return p.svc.CreateCampaignShort(input)
}

func (p *platformCampaignService) UpdateCampaignShort(input *campaign.UpdateCampaignParams) (*platformclientmodels.CampaignInfo, error) {
	return p.svc.UpdateCampaignShort(input)
}

func (p *platformCampaignService) GetCampaignShort(input *campaign.GetCampaignParams) (*platformclientmodels.CampaignInfo, error) {
	return p.svc.GetCampaignShort(input)
}

func (p *platformCampaignService) CreateCodesShort(input *campaign.CreateCodesParams) (*platformclientmodels.CodeCreateResult, error) {
	return p.svc.CreateCodesShort(input)
}

func (p *platformCampaignService) QueryCodesShort(input *campaign.QueryCodesParams) (*platformclientmodels.CodeInfoPagingSlicedResult, error) {
	return p.svc.QueryCodesShort(input)
}

type platformStoreService struct{ svc *platform.StoreService }

func (p *platformStoreService) ListStoresShort(input *store.ListStoresParams) ([]*platformclientmodels.StoreInfo, error) {
	return p.svc.ListStoresShort(input)
}

func (p *platformStoreService) CreateStoreShort(input *store.CreateStoreParams) (*platformclientmodels.StoreInfo, error) {
	return p.svc.CreateStoreShort(input)
}

type platformCategoryService struct{ svc *platform.CategoryService }

func (p *platformCategoryService) GetCategoryShort(input *category.GetCategoryParams) (*platformclientmodels.FullCategoryInfo, error) {
	return p.svc.GetCategoryShort(input)
}

func (p *platformCategoryService) CreateCategoryShort(input *category.CreateCategoryParams) (*platformclientmodels.FullCategoryInfo, error) {
	return p.svc.CreateCategoryShort(input)
}

type platformCurrencyService struct{ svc *platform.CurrencyService }

func (p *platformCurrencyService) ListCurrenciesShort(input *currency.ListCurrenciesParams) ([]*platformclientmodels.CurrencyInfo, error) {
	return p.svc.ListCurrenciesShort(input)
}

func (p *platformCurrencyService) CreateCurrencyShort(input *currency.CreateCurrencyParams) (*platformclientmodels.CurrencyInfo, error) {
	return p.svc.CreateCurrencyShort(input)
}

// CreateItem provisions a CODE-type, DURABLE Item in the configured
// store. Items are non-listable / non-purchasable — they exist only as
// the redeem target for the campaign codes (PRD §4.6 step 2a).
//
// BoothName names the Campaign that owns the redeem codes for this
// item. AGS validates it is non-null at item-create time even though
// the linked Campaign hasn't been created yet — the link is resolved
// at redeem time. We pass the same name we'll use for the Campaign so
// the two stay paired.
func (c *SDKClient) CreateItem(ctx context.Context, spec ItemSpec) (string, error) {
	if spec.BoothName == "" {
		return "", &ClientError{StatusCode: 500, Op: "CreateItem", Message: "ItemSpec.BoothName is required (AGS returns it as CreatedCampaign.BoothName)"}
	}
	c.mu.RLock()
	storeID := c.storeID
	c.mu.RUnlock()
	if storeID == "" {
		return "", &ClientError{StatusCode: 500, Op: "CreateItem", Message: "store id is unresolved — Bootstrap must run before CreateItem"}
	}
	body := &platformclientmodels.ItemCreate{
		Name:            ptrString(spec.Name),
		ItemType:        ptrString("CODE"),
		EntitlementType: ptrString("DURABLE"),
		Status:          ptrString("ACTIVE"),
		CategoryPath:    ptrString("/playtesthub"),
		BoothName:       spec.BoothName,
		Listable:        false,
		Purchasable:     false,
		Localizations: map[string]platformclientmodels.Localization{
			"en": {Title: ptrString(spec.Name), Description: spec.Description},
		},
		RegionData: c.regionData(),
	}
	params := &item.CreateItemParams{
		Body:      body,
		Namespace: c.namespace,
		StoreID:   storeID,
	}
	params = params.WithContext(ctx)

	got, err := callWithRelogin(c, "CreateItem", func() (*platformclientmodels.FullItemInfo, error) {
		return c.itemSvc.CreateItemShort(params)
	})
	if err != nil {
		return "", err
	}
	if got == nil || got.ItemID == nil {
		return "", &ClientError{StatusCode: 500, Op: "CreateItem", Message: "AGS returned empty item id"}
	}
	return *got.ItemID, nil
}

// CreateCampaign provisions an empty REDEMPTION campaign (no
// redeemable items). The Item is created next with BoothName equal to
// this campaign's name; LinkItemToCampaign then attaches the item via
// UpdateCampaign. AGS rejects CODE-type Items whose BoothName is null
// or refers to a missing Campaign, so the Campaign-first ordering is
// load-bearing — see Client interface docs.
func (c *SDKClient) CreateCampaign(ctx context.Context, spec CampaignSpec) (CreatedCampaign, error) {
	body := &platformclientmodels.CampaignCreate{
		Name:                  ptrString(spec.Name),
		Description:           spec.Description,
		Type:                  "REDEMPTION",
		RedeemType:            "ITEM",
		Status:                "ACTIVE",
		MaxRedeemCountPerCode: 1,
	}
	params := &campaign.CreateCampaignParams{
		Body:      body,
		Namespace: c.namespace,
	}
	params = params.WithContext(ctx)

	got, err := callWithRelogin(c, "CreateCampaign", func() (*platformclientmodels.CampaignInfo, error) {
		return c.campaignSvc.CreateCampaignShort(params)
	})
	if err != nil {
		return CreatedCampaign{}, err
	}
	if got == nil || got.ID == nil {
		return CreatedCampaign{}, &ClientError{StatusCode: 500, Op: "CreateCampaign", Message: "AGS returned empty campaign id"}
	}
	if got.BoothName == nil || *got.BoothName == "" {
		return CreatedCampaign{}, &ClientError{StatusCode: 500, Op: "CreateCampaign", Message: "AGS returned campaign without boothName"}
	}
	return CreatedCampaign{ID: *got.ID, BoothName: *got.BoothName}, nil
}

// LinkItemToCampaign updates the campaign's redeemable-items list to
// the single (itemID, itemName) pair. Each redeem grants 1 unit
// (MaxRedeemCountPerCode=1 is enforced at CreateCampaign time).
func (c *SDKClient) LinkItemToCampaign(ctx context.Context, campaignID, itemID, itemName string) error {
	getParams := (&campaign.GetCampaignParams{
		CampaignID: campaignID,
		Namespace:  c.namespace,
	}).WithContext(ctx)
	existing, err := callWithRelogin(c, "GetCampaign", func() (*platformclientmodels.CampaignInfo, error) {
		return c.campaignSvc.GetCampaignShort(getParams)
	})
	if err != nil {
		return err
	}
	if existing == nil || existing.Name == nil {
		return &ClientError{StatusCode: 500, Op: "GetCampaign", Message: "AGS returned campaign without name"}
	}
	body := &platformclientmodels.CampaignUpdate{
		Name:   existing.Name,
		Status: "ACTIVE",
		Items: []*platformclientmodels.RedeemableItem{
			{ItemID: ptrString(itemID), ItemName: ptrString(itemName), Quantity: 1},
		},
	}
	updateParams := (&campaign.UpdateCampaignParams{
		Body:       body,
		CampaignID: campaignID,
		Namespace:  c.namespace,
	}).WithContext(ctx)
	if _, err := callWithRelogin(c, "UpdateCampaign", func() (*platformclientmodels.CampaignInfo, error) {
		return c.campaignSvc.UpdateCampaignShort(updateParams)
	}); err != nil {
		return err
	}
	return nil
}

// CreateCodes asks AGS to generate `quantity` codes against the
// campaign, then queries them back to recover the values (the SDK's
// CreateCodes only returns NumCreated). Each call uses a unique batch
// name so QueryCodes can isolate just-generated values from any older
// pool entries (PRD §4.6 step 2c, docs/ags-failure-modes.md).
//
// Partial fulfillment: if AGS returns NumCreated < quantity without an
// error, that is treated as the §"Partial fulfillment" warn-and-commit
// path — the caller (service layer) sees CodeBatchResult.Codes shorter
// than Requested and surfaces the warning.
func (c *SDKClient) CreateCodes(ctx context.Context, campaignID string, quantity int) (CodeBatchResult, error) {
	if quantity <= 0 {
		return CodeBatchResult{Requested: quantity}, nil
	}
	batchName, err := newBatchName()
	if err != nil {
		return CodeBatchResult{}, fmt.Errorf("ags: generate batch name: %w", err)
	}

	createParams := &campaign.CreateCodesParams{
		Body: &platformclientmodels.CodeCreate{
			BatchName: batchName,
			Quantity:  int32(quantity),
		},
		CampaignID: campaignID,
		Namespace:  c.namespace,
	}
	createParams = createParams.WithContext(ctx)

	createRes, err := callWithRelogin(c, "CreateCodes", func() (*platformclientmodels.CodeCreateResult, error) {
		return c.campaignSvc.CreateCodesShort(createParams)
	})
	if err != nil {
		return CodeBatchResult{}, err
	}
	created := 0
	if createRes != nil && createRes.NumCreated != nil {
		created = int(*createRes.NumCreated)
	}
	if created == 0 {
		return CodeBatchResult{Requested: quantity}, nil
	}

	// Pull only the values that belong to this batch. AGS QueryCodes
	// recommends Limit ≤ 100 for stability; we pass that cap explicitly.
	values, err := c.queryCodesByBatchName(ctx, campaignID, batchName)
	if err != nil {
		return CodeBatchResult{}, err
	}
	return CodeBatchResult{Requested: quantity, Codes: values}, nil
}

// FetchCodes pages every code on the campaign via QueryCodes (no batch
// filter) — used by SyncFromAGS recovery (PRD §4.6 step 5).
func (c *SDKClient) FetchCodes(ctx context.Context, campaignID string) ([]string, error) {
	const pageLimit int32 = 100
	var (
		offset int32
		out    []string
	)
	for {
		params := &campaign.QueryCodesParams{
			CampaignID: campaignID,
			Namespace:  c.namespace,
			Limit:      ptrInt32(pageLimit),
			Offset:     ptrInt32(offset),
		}
		params = params.WithContext(ctx)
		res, err := callWithRelogin(c, "QueryCodes", func() (*platformclientmodels.CodeInfoPagingSlicedResult, error) {
			return c.campaignSvc.QueryCodesShort(params)
		})
		if err != nil {
			return nil, err
		}
		if res == nil || len(res.Data) == 0 {
			return out, nil
		}
		for _, info := range res.Data {
			if info != nil && info.Value != nil {
				out = append(out, *info.Value)
			}
		}
		if int32(len(res.Data)) < pageLimit {
			return out, nil
		}
		offset += int32(len(res.Data))
	}
}

// DeleteItem removes the item with Force=true so cleanup works against
// already-published draft entries. 404 is treated as success — matches
// MemClient idempotency and the cleanup matrix's best-effort intent
// (docs/ags-failure-modes.md cleanup matrix).
func (c *SDKClient) DeleteItem(ctx context.Context, itemID string) error {
	c.mu.RLock()
	storeID := c.storeID
	c.mu.RUnlock()
	force := true
	params := &item.DeleteItemParams{
		ItemID:    itemID,
		Namespace: c.namespace,
		StoreID:   &storeID,
		Force:     &force,
	}
	params = params.WithContext(ctx)
	if err := callVoidWithRelogin(c, "DeleteItem", func() error {
		return c.itemSvc.DeleteItemShort(params)
	}); err != nil {
		if isNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// DeleteCampaign deactivates the campaign (AGS has no DELETE on
// campaigns — UpdateCampaign Status=INACTIVE is the documented soft
// delete). 404 is treated as success.
func (c *SDKClient) DeleteCampaign(ctx context.Context, campaignID string) error {
	getParams := (&campaign.GetCampaignParams{
		CampaignID: campaignID,
		Namespace:  c.namespace,
	}).WithContext(ctx)
	existing, err := callWithRelogin(c, "GetCampaign", func() (*platformclientmodels.CampaignInfo, error) {
		return c.campaignSvc.GetCampaignShort(getParams)
	})
	if err != nil {
		if isNotFound(err) {
			return nil
		}
		return err
	}
	if existing == nil || existing.Name == nil {
		return &ClientError{StatusCode: 500, Op: "GetCampaign", Message: "AGS returned campaign without name"}
	}

	body := &platformclientmodels.CampaignUpdate{
		Name:   existing.Name,
		Status: "INACTIVE",
	}
	updateParams := (&campaign.UpdateCampaignParams{
		Body:       body,
		CampaignID: campaignID,
		Namespace:  c.namespace,
	}).WithContext(ctx)
	if _, err := callWithRelogin(c, "UpdateCampaign", func() (*platformclientmodels.CampaignInfo, error) {
		return c.campaignSvc.UpdateCampaignShort(updateParams)
	}); err != nil {
		if isNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// Bootstrap ensures the namespace prerequisites (store + `/playtesthub`
// category + currency for RegionData) exist before AGS_CAMPAIGN
// provisioning. Each step treats HTTP 409 conflict as success: another
// process raced us to the same resource, which is the desired outcome.
//
// Resolution rules:
//   - Store: when SDKClient.storeID is empty, ListStores is consulted
//     and the first match (by namespace order) is reused. Empty list →
//     CreateStore using params.StoreTitle / DefaultRegion / DefaultLanguage.
//   - Category: GetCategory `/playtesthub`; 404 triggers CreateCategory
//     with `localizationDisplayNames["en"]="Playtesthub"`.
//   - Currency: when SDKClient.regionCurrencyCode is empty, ListCurrencies
//     is consulted and the first VIRTUAL entry wins. Empty (or no VIRTUAL)
//     → CreateCurrency using params.FallbackCurrencyCode.
//
// On success, the resolved storeID + currency code/type are written to
// SDKClient state under c.mu so subsequent CreateItem calls see the
// fresh values.
func (c *SDKClient) Bootstrap(ctx context.Context, params BootstrapParams) error {
	if c.storeSvc == nil || c.categorySvc == nil || c.currencySvc == nil {
		return &ClientError{StatusCode: 500, Op: "Bootstrap", Message: "store/category/currency SDK services not wired"}
	}

	defaultRegion := params.DefaultRegion
	if defaultRegion == "" {
		c.mu.RLock()
		defaultRegion = c.regionCode
		c.mu.RUnlock()
	}
	if defaultRegion == "" {
		defaultRegion = "US"
	}
	defaultLanguage := params.DefaultLanguage
	if defaultLanguage == "" {
		defaultLanguage = "en"
	}
	categoryPath := params.CategoryPath
	if categoryPath == "" {
		categoryPath = "/playtesthub"
	}
	storeTitle := params.StoreTitle
	if storeTitle == "" {
		storeTitle = "playtesthub"
	}

	storeID, err := c.bootstrapStore(ctx, storeTitle, defaultRegion, defaultLanguage)
	if err != nil {
		return fmt.Errorf("bootstrap store: %w", err)
	}
	if err := c.bootstrapCategory(ctx, storeID, categoryPath); err != nil {
		return fmt.Errorf("bootstrap category: %w", err)
	}
	currencyCode, currencyType, err := c.bootstrapCurrency(ctx, params.FallbackCurrencyCode, params.FallbackCurrencyType)
	if err != nil {
		return fmt.Errorf("bootstrap currency: %w", err)
	}

	c.mu.Lock()
	c.storeID = storeID
	if c.regionCurrencyCode == "" {
		c.regionCurrencyCode = currencyCode
		if currencyType != "" {
			c.regionCurrencyType = currencyType
		}
	}
	c.mu.Unlock()
	return nil
}

func (c *SDKClient) bootstrapStore(ctx context.Context, title, defaultRegion, defaultLanguage string) (string, error) {
	c.mu.RLock()
	preset := c.storeID
	c.mu.RUnlock()
	if preset != "" {
		return preset, nil
	}

	listParams := (&store.ListStoresParams{Namespace: c.namespace}).WithContext(ctx)
	stores, err := callWithRelogin(c, "ListStores", func() ([]*platformclientmodels.StoreInfo, error) {
		return c.storeSvc.ListStoresShort(listParams)
	})
	if err != nil {
		return "", err
	}
	for _, s := range stores {
		if s != nil && s.StoreID != nil && *s.StoreID != "" {
			return *s.StoreID, nil
		}
	}

	body := &platformclientmodels.StoreCreate{
		Title:              ptrString(title),
		DefaultRegion:      defaultRegion,
		DefaultLanguage:    defaultLanguage,
		SupportedRegions:   []string{defaultRegion},
		SupportedLanguages: []string{defaultLanguage},
	}
	createParams := (&store.CreateStoreParams{Body: body, Namespace: c.namespace}).WithContext(ctx)
	created, err := callWithRelogin(c, "CreateStore", func() (*platformclientmodels.StoreInfo, error) {
		return c.storeSvc.CreateStoreShort(createParams)
	})
	if err == nil && created != nil && created.StoreID != nil {
		return *created.StoreID, nil
	}
	if err != nil && !isConflict(err) {
		return "", err
	}
	// 409 (or empty body): re-list to pick up the existing store.
	stores, listErr := callWithRelogin(c, "ListStores", func() ([]*platformclientmodels.StoreInfo, error) {
		return c.storeSvc.ListStoresShort(listParams)
	})
	if listErr != nil {
		return "", listErr
	}
	for _, s := range stores {
		if s != nil && s.StoreID != nil && *s.StoreID != "" {
			return *s.StoreID, nil
		}
	}
	return "", &ClientError{StatusCode: 500, Op: "Bootstrap", Message: "store creation conflicted but ListStores returned empty"}
}

func (c *SDKClient) bootstrapCategory(ctx context.Context, storeID, categoryPath string) error {
	getParams := (&category.GetCategoryParams{
		CategoryPath: categoryPath,
		Namespace:    c.namespace,
		StoreID:      &storeID,
	}).WithContext(ctx)
	_, err := callWithRelogin(c, "GetCategory", func() (*platformclientmodels.FullCategoryInfo, error) {
		return c.categorySvc.GetCategoryShort(getParams)
	})
	if err == nil {
		return nil
	}
	if !isNotFound(err) {
		return err
	}
	body := &platformclientmodels.CategoryCreate{
		CategoryPath:             ptrString(categoryPath),
		LocalizationDisplayNames: map[string]string{"en": "Playtesthub"},
	}
	createParams := (&category.CreateCategoryParams{
		Body:      body,
		Namespace: c.namespace,
		StoreID:   storeID,
	}).WithContext(ctx)
	if _, err := callWithRelogin(c, "CreateCategory", func() (*platformclientmodels.FullCategoryInfo, error) {
		return c.categorySvc.CreateCategoryShort(createParams)
	}); err != nil && !isConflict(err) {
		return err
	}
	return nil
}

func (c *SDKClient) bootstrapCurrency(ctx context.Context, fallbackCode, fallbackType string) (string, string, error) {
	c.mu.RLock()
	preset := c.regionCurrencyCode
	presetType := c.regionCurrencyType
	c.mu.RUnlock()
	if preset != "" {
		return preset, presetType, nil
	}

	listParams := (&currency.ListCurrenciesParams{Namespace: c.namespace}).WithContext(ctx)
	existing, err := callWithRelogin(c, "ListCurrencies", func() ([]*platformclientmodels.CurrencyInfo, error) {
		return c.currencySvc.ListCurrenciesShort(listParams)
	})
	if err != nil {
		return "", "", err
	}
	for _, cur := range existing {
		if cur == nil || cur.CurrencyCode == nil || *cur.CurrencyCode == "" {
			continue
		}
		curType := ""
		if cur.CurrencyType != nil {
			curType = *cur.CurrencyType
		}
		// Prefer VIRTUAL — it matches the playtest item's non-purchasable
		// nature and avoids surprising operators with REAL pricing.
		if curType == currencyTypeVirtual {
			return *cur.CurrencyCode, curType, nil
		}
	}
	// No VIRTUAL existed; fall through to creating one.
	if fallbackCode == "" {
		fallbackCode = "PTH_VIRTUAL"
	}
	if fallbackType == "" {
		fallbackType = currencyTypeVirtual
	}
	body := &platformclientmodels.CurrencyCreate{
		CurrencyCode:             ptrString(fallbackCode),
		CurrencyType:             fallbackType,
		CurrencySymbol:           "P",
		Decimals:                 0,
		LocalizationDescriptions: map[string]string{"en": "playtesthub virtual currency (auto-provisioned)"},
	}
	createParams := (&currency.CreateCurrencyParams{Body: body, Namespace: c.namespace}).WithContext(ctx)
	if _, err := callWithRelogin(c, "CreateCurrency", func() (*platformclientmodels.CurrencyInfo, error) {
		return c.currencySvc.CreateCurrencyShort(createParams)
	}); err != nil && !isConflict(err) {
		return "", "", err
	}
	return fallbackCode, fallbackType, nil
}

func (c *SDKClient) queryCodesByBatchName(ctx context.Context, campaignID, batchName string) ([]string, error) {
	const pageLimit int32 = 100
	withBatchName := true
	var (
		offset int32
		out    []string
	)
	for {
		params := &campaign.QueryCodesParams{
			BatchName:     ptrString(batchName),
			CampaignID:    campaignID,
			Namespace:     c.namespace,
			Limit:         ptrInt32(pageLimit),
			Offset:        ptrInt32(offset),
			WithBatchName: &withBatchName,
		}
		params = params.WithContext(ctx)
		res, err := c.campaignSvc.QueryCodesShort(params)
		if err != nil {
			return nil, wrapSDKError("QueryCodes", err)
		}
		if res == nil || len(res.Data) == 0 {
			return out, nil
		}
		for _, info := range res.Data {
			if info != nil && info.Value != nil {
				out = append(out, *info.Value)
			}
		}
		// Some SDK builds page via Paging.Next (cursor) and others by
		// offset. Stop when the page is short OR when no Next pointer
		// is present.
		if res.Paging == nil || res.Paging.Next == "" {
			if int32(len(res.Data)) < pageLimit {
				return out, nil
			}
		}
		offset += int32(len(res.Data))
	}
}

// sdkError carries the upstream HTTP status alongside the original
// SDK error so RetryPolicy.Run can classify it via HTTPStatusCarrier.
type sdkError struct {
	status int
	op     string
	cause  error
}

func (e *sdkError) Error() string {
	if e.op == "" {
		return fmt.Sprintf("ags: sdk error (status %d): %v", e.status, e.cause)
	}
	return fmt.Sprintf("ags: %s: sdk error (status %d): %v", e.op, e.status, e.cause)
}

func (e *sdkError) Unwrap() error   { return e.cause }
func (e *sdkError) HTTPStatus() int { return e.status }

// statusFromErrPattern matches the "[METHOD /path][NNN] ..." prefix that
// every SDK typed-response Error() produces, plus the fallback
// "returns an error NNN: ..." emitted by ReadResponse default cases.
var (
	statusFromBracketPattern  = regexp.MustCompile(`\]\[(\d{3})\]`)
	statusFromFallbackPattern = regexp.MustCompile(`returns an error (\d{3})`)
)

// wrapSDKError extracts the HTTP status from the SDK error string and
// returns either an *sdkError (carrying the status) or the original err
// when no status can be parsed (transport / context errors). RetryPolicy
// already handles the no-status path by treating it as a retryable
// transport failure.
func wrapSDKError(op string, err error) error {
	if err == nil {
		return nil
	}
	// Context cancellation / deadline already classifies cleanly via
	// errors.Is in retry.go.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	msg := err.Error()
	if status := matchStatus(msg); status > 0 {
		return &sdkError{status: status, op: op, cause: err}
	}
	return err
}

func matchStatus(msg string) int {
	if m := statusFromBracketPattern.FindStringSubmatch(msg); len(m) == 2 {
		if v, perr := strconv.Atoi(m[1]); perr == nil {
			return v
		}
	}
	if m := statusFromFallbackPattern.FindStringSubmatch(msg); len(m) == 2 {
		if v, perr := strconv.Atoi(m[1]); perr == nil {
			return v
		}
	}
	return 0
}

// isNotFound reports whether err is a *ClientError with status 404 or
// an *sdkError with status 404.
func isNotFound(err error) bool {
	var ce *ClientError
	if errors.As(err, &ce) {
		return ce.StatusCode == http.StatusNotFound
	}
	var se *sdkError
	if errors.As(err, &se) {
		return se.status == http.StatusNotFound
	}
	return false
}

// isConflict reports whether err carries HTTP 409. Bootstrap treats
// conflict as success: the resource being created already exists, which
// is exactly what we wanted. The conflict body's errorCode (e.g. 30174
// for stores, 30271 for categories, 36171 for currencies) is consumed as
// the success signal.
func isConflict(err error) bool {
	var se *sdkError
	if errors.As(err, &se) {
		return se.status == http.StatusConflict
	}
	var ce *ClientError
	if errors.As(err, &ce) {
		return ce.StatusCode == http.StatusConflict
	}
	return false
}

// isUnauthorized reports whether err carries HTTP 401. The platform-side
// TokenRepository never auto-refreshes (see SDKClient docs), so 401 is
// the only signal we have that the token has expired and re-login is
// needed.
func isUnauthorized(err error) bool {
	var se *sdkError
	if errors.As(err, &se) {
		return se.status == http.StatusUnauthorized
	}
	var ce *ClientError
	if errors.As(err, &ce) {
		return ce.StatusCode == http.StatusUnauthorized
	}
	return false
}

// callWithRelogin runs fn, and if the SDK returns HTTP 401 it triggers
// SDKClient.login (when set) and retries fn once. The retry is
// intentionally bounded to one attempt: 401 should clear after a fresh
// client-credentials grant; any further 401 indicates a credential
// problem the caller needs to surface.
func callWithRelogin[T any](c *SDKClient, op string, fn func() (T, error)) (T, error) {
	got, err := fn()
	if err == nil {
		return got, nil
	}
	wrapped := wrapSDKError(op, err)
	if !isUnauthorized(wrapped) || c.login == nil {
		return got, wrapped
	}
	if loginErr := c.login(); loginErr != nil {
		return got, fmt.Errorf("ags: %s: relogin after 401: %w", op, loginErr)
	}
	got, err = fn()
	if err != nil {
		return got, wrapSDKError(op, err)
	}
	return got, nil
}

// callVoidWithRelogin is the no-result variant for SDK calls that
// return only an error (e.g. DeleteItemShort).
func callVoidWithRelogin(c *SDKClient, op string, fn func() error) error {
	_, err := callWithRelogin(c, op, func() (struct{}, error) {
		return struct{}{}, fn()
	})
	return err
}

// NewPlatformConfigRepository is a small bridge so bootapp can construct
// the platform-sdk repository.ConfigRepository without re-importing
// the SDK config types in two places.
func NewPlatformConfigRepository(baseURL, clientID, clientSecret string) repository.ConfigRepository {
	return platformConfigRepo{baseURL: baseURL, clientID: clientID, clientSecret: clientSecret}
}

type platformConfigRepo struct {
	baseURL      string
	clientID     string
	clientSecret string
}

func (r platformConfigRepo) GetClientId() string       { return r.clientID }
func (r platformConfigRepo) GetClientSecret() string   { return r.clientSecret }
func (r platformConfigRepo) GetJusticeBaseUrl() string { return r.baseURL }

func ptrString(s string) *string { return &s }
func ptrInt32(v int32) *int32    { return &v }

// regionData builds the RegionData map CreateItem includes on the body.
// Returns nil when no currency is configured so the request omits the
// field entirely (matches docs/engineering.md "AGS namespace
// prerequisites": pricing currency is optional for namespaces that do
// not enforce it). When set, returns one entry keyed by regionCode with
// a zero-priced item using the configured currency — playtest items are
// non-purchasable so price is a placeholder to satisfy AGS validation.
func (c *SDKClient) regionData() map[string][]platformclientmodels.RegionDataItemDTO {
	c.mu.RLock()
	currencyCode := c.regionCurrencyCode
	currencyType := c.regionCurrencyType
	regionCode := c.regionCode
	c.mu.RUnlock()
	if currencyCode == "" {
		return nil
	}
	zero := int32(0)
	return map[string][]platformclientmodels.RegionDataItemDTO{
		regionCode: {
			{
				CurrencyCode:      ptrString(currencyCode),
				CurrencyType:      ptrString(currencyType),
				CurrencyNamespace: ptrString(c.namespace),
				Price:             &zero,
			},
		},
	}
}

// newBatchName returns a unique 32-char-ish batch name AGS will accept
// (alphanumeric + hyphen, 3..60 chars). Format: "pth-<16-hex>" so logs
// can grep playtesthub-issued batches.
func newBatchName() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	var sb strings.Builder
	sb.WriteString("pth-")
	sb.WriteString(hex.EncodeToString(b))
	return sb.String(), nil
}
