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

	"github.com/AccelByte/accelbyte-go-sdk/platform-sdk/pkg/platformclient/campaign"
	"github.com/AccelByte/accelbyte-go-sdk/platform-sdk/pkg/platformclient/item"
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
type SDKClient struct {
	namespace string
	storeID   string

	itemSvc     ItemService
	campaignSvc CampaignService
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

// SDKClientOptions configures a fresh SDKClient.
type SDKClientOptions struct {
	Namespace   string
	StoreID     string
	ItemSvc     ItemService
	CampaignSvc CampaignService
	// Login re-runs the client-credentials grant and stores the new
	// token in the same TokenRepository the Item/Campaign services
	// consume. Optional — when nil, 401 responses surface as ClientError
	// without a retry attempt.
	Login func() error
}

// NewSDKClient constructs an SDKClient. Namespace and StoreID are
// required; missing either is a programmer error and panics — bootapp
// is expected to gate construction on a non-empty AGSStoreID.
func NewSDKClient(opts SDKClientOptions) *SDKClient {
	if opts.Namespace == "" {
		panic("ags.NewSDKClient: Namespace is required")
	}
	if opts.StoreID == "" {
		panic("ags.NewSDKClient: StoreID is required")
	}
	return &SDKClient{
		namespace:   opts.Namespace,
		storeID:     opts.StoreID,
		itemSvc:     opts.ItemSvc,
		campaignSvc: opts.CampaignSvc,
		login:       opts.Login,
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
	body := &platformclientmodels.ItemCreate{
		Name:            ptrString(spec.Name),
		ItemType:        ptrString("CODE"),
		EntitlementType: ptrString("DURABLE"),
		Status:          ptrString("ACTIVE"),
		CategoryPath:    ptrString("/playtesthub"),
		BoothName:       spec.Name,
		Listable:        false,
		Purchasable:     false,
		Localizations: map[string]platformclientmodels.Localization{
			"en": {Title: ptrString(spec.Name), Description: spec.Description},
		},
		RegionData: map[string][]platformclientmodels.RegionDataItemDTO{
			"US": {},
		},
	}
	params := &item.CreateItemParams{
		Body:      body,
		Namespace: c.namespace,
		StoreID:   c.storeID,
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

// CreateCampaign provisions a REDEMPTION campaign that grants exactly
// one of the supplied item per code (PRD §4.6 step 2b).
func (c *SDKClient) CreateCampaign(ctx context.Context, spec CampaignSpec) (string, error) {
	body := &platformclientmodels.CampaignCreate{
		Name:                  ptrString(spec.Name),
		Description:           spec.Description,
		Type:                  "REDEMPTION",
		RedeemType:            "ITEM",
		Status:                "ACTIVE",
		MaxRedeemCountPerCode: 1,
		Items: []*platformclientmodels.RedeemableItem{
			{ItemID: ptrString(spec.ItemID), ItemName: ptrString(spec.Name), Quantity: 1},
		},
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
		return "", err
	}
	if got == nil || got.ID == nil {
		return "", &ClientError{StatusCode: 500, Op: "CreateCampaign", Message: "AGS returned empty campaign id"}
	}
	return *got.ID, nil
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
	force := true
	params := &item.DeleteItemParams{
		ItemID:    itemID,
		Namespace: c.namespace,
		StoreID:   &c.storeID,
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
