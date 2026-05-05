// Package ags wraps the AccelByte Platform Item + Campaign API surface
// playtesthub depends on for the AGS_CAMPAIGN distribution model
// (PRD §4.6 / docs/ags-failure-modes.md).
//
// The service layer talks to Client (an interface) so unit tests can
// drop in MemClient without standing up the SDK / IAM client. Production
// wires SDKClient in main.go (or bootapp once a live ISC namespace is
// configured); MemClient is the default in bootapp until then so the
// boot path + smoke harness exercise the full AGS_CAMPAIGN code path
// without a real AGS round-trip.
package ags

import (
	"context"
)

// Client is the minimum AGS Platform API surface playtesthub needs.
// Method semantics match docs/PRD.md §4.6 + docs/ags-failure-modes.md;
// retry + classification policy is the implementation's job.
//
// Provisioning order (per AGS validation rules — see CreateItem):
//
//	CreateCampaign → CreateItem → LinkItemToCampaign → CreateCodes
//
// AGS rejects CODE-type Items whose BoothName does not refer to an
// existing Campaign, so the Campaign must be created first. The
// Campaign is then updated to redeem the just-created Item. AGS
// auto-derives the campaign's boothName (e.g. "C_<name>"), so the
// caller must pass through the value returned by CreateCampaign rather
// than reusing the raw name.
type Client interface {
	// Bootstrap ensures the namespace prerequisites required by
	// CreateItem exist (PRD §4.6 / docs/engineering.md "AGS namespace
	// prerequisites"): a Store, a `/playtesthub` Category in that
	// store, and a currency the CreateItem RegionData entry can
	// reference. Idempotent — every step treats "already exists"
	// (HTTP 409 / errorCode 30201/30207/etc.) as success and resolves
	// the existing resource id instead.
	//
	// Bootstrap mutates the client's resolved store / currency state
	// when no values were supplied at construction; subsequent
	// CreateItem calls use those resolved values. Service callers run
	// Bootstrap once per process lifetime (sync.Once gate) before the
	// first CreateCampaign of an AGS_CAMPAIGN playtest.
	Bootstrap(ctx context.Context, params BootstrapParams) error

	// CreateItem provisions an ENTITLEMENT-type Item under the
	// configured namespace and returns the AGS-assigned item id
	// (PRD §4.6 step 2a). spec.BoothName must equal the
	// CreatedCampaign.BoothName returned from CreateCampaign — AGS
	// rejects 404 / 37041 if the booth does not resolve.
	CreateItem(ctx context.Context, spec ItemSpec) (string, error)

	// CreateCampaign provisions an empty REDEMPTION campaign and
	// returns the AGS-assigned campaign id + auto-derived boothName
	// (PRD §4.6 step 2b). The campaign is created without redeemable
	// items; use LinkItemToCampaign to attach the Item once it has been
	// created.
	CreateCampaign(ctx context.Context, spec CampaignSpec) (CreatedCampaign, error)

	// LinkItemToCampaign sets the campaign's redeemable Items list to
	// the single given (itemID, itemName) pair. Required after
	// CreateItem: codes generated against a campaign with no items
	// redeem nothing. Idempotent — repeated calls overwrite the list.
	LinkItemToCampaign(ctx context.Context, campaignID, itemID, itemName string) error

	// CreateCodes asks AGS to generate `quantity` codes against the
	// campaign and returns the values inline. The implementation is
	// responsible for paginating CreateCodes if the server splits the
	// batch (docs/ags-failure-modes.md "Code generation batch size &
	// pagination"); callers issue one CreateCodes per agsCodeBatchSize
	// chunk (docs/PRD.md §4.6 step 2c).
	CreateCodes(ctx context.Context, campaignID string, quantity int) (CodeBatchResult, error)

	// FetchCodes returns all codes ever generated for the campaign,
	// paging through the AGS QueryCodes endpoint until exhausted. Used
	// by SyncFromAGS (PRD §4.6 step 5; phase 9).
	FetchCodes(ctx context.Context, campaignID string) ([]string, error)

	// DeleteItem removes an Item created during a failed CreatePlaytest
	// auto-provision (cleanup matrix step 1, docs/ags-failure-modes.md).
	// Errors are non-fatal — the service layer logs them at WARN.
	DeleteItem(ctx context.Context, itemID string) error

	// DeleteCampaign removes a Campaign created during a failed
	// CreatePlaytest auto-provision (cleanup matrix step 2). Same
	// best-effort semantics as DeleteItem.
	DeleteCampaign(ctx context.Context, campaignID string) error
}

// ItemSpec is the input shape for CreateItem. Derived from playtest
// title/description per PRD §4.6 step 2a. BoothName must be the value
// AGS returned in CreatedCampaign.BoothName (AGS prefixes the campaign
// name with "C_" so the raw name will not resolve).
type ItemSpec struct {
	Name        string
	Description string
	BoothName   string
}

// CampaignSpec is the input shape for CreateCampaign. The campaign is
// created empty — items are attached later via LinkItemToCampaign once
// the Item itself exists.
type CampaignSpec struct {
	Name        string
	Description string
}

// CreatedCampaign is the output shape of CreateCampaign. BoothName is
// the AGS-assigned ticket-booth identifier the next CreateItem call
// must reference.
type CreatedCampaign struct {
	ID        string
	BoothName string
}

// CodeBatchResult is the output shape of CreateCodes. Codes is the
// list AGS actually returned, which may be shorter than the requested
// quantity — see docs/ags-failure-modes.md "Partial fulfillment".
type CodeBatchResult struct {
	// Requested mirrors the quantity the caller asked for so partial
	// fulfillment surfaces deterministically.
	Requested int
	Codes     []string
}

// BootstrapParams seeds the resources Bootstrap will create when they
// are missing. Existing resources are reused regardless of these values
// — only first-time provisioning consumes them.
type BootstrapParams struct {
	// StoreTitle names the store created when no store exists in the
	// namespace and the SDKClient was constructed without an explicit
	// StoreID. AGS rejects titles outside the documented charset
	// (alphanumeric + ',.- + space, max 127); callers should pass a
	// safe value like "playtesthub".
	StoreTitle string
	// DefaultRegion is the store's defaultRegion (also used as the
	// region key inside CreateItem RegionData). Defaults to "US".
	DefaultRegion string
	// DefaultLanguage is the store's defaultLanguage. Defaults to "en".
	DefaultLanguage string
	// CategoryPath names the category Bootstrap ensures exists in the
	// resolved store. Hardcoded to "/playtesthub" by the service layer
	// to match SDKClient.CreateItem's CategoryPath.
	CategoryPath string
	// FallbackCurrencyCode names the currency Bootstrap creates when
	// no VIRTUAL currency exists in the namespace and the SDKClient was
	// constructed without an explicit RegionCurrencyCode.
	FallbackCurrencyCode string
	// FallbackCurrencyType is "VIRTUAL" or "REAL". Defaults to "VIRTUAL"
	// since playtest items are non-purchasable.
	FallbackCurrencyType string
}
