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
// Campaign is then updated to redeem the just-created Item.
type Client interface {
	// CreateItem provisions an ENTITLEMENT-type Item under the
	// configured namespace and returns the AGS-assigned item id
	// (PRD §4.6 step 2a). The Item's BoothName is set to spec.Name,
	// which must already exist as a Campaign — call CreateCampaign
	// first.
	CreateItem(ctx context.Context, spec ItemSpec) (string, error)

	// CreateCampaign provisions an empty REDEMPTION campaign and
	// returns the AGS-assigned campaign id (PRD §4.6 step 2b). The
	// campaign is created without redeemable items; use
	// LinkItemToCampaign to attach the Item once it has been created.
	CreateCampaign(ctx context.Context, spec CampaignSpec) (string, error)

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
// title/description per PRD §4.6 step 2a.
type ItemSpec struct {
	Name        string
	Description string
}

// CampaignSpec is the input shape for CreateCampaign. The campaign is
// created empty — items are attached later via LinkItemToCampaign once
// the Item itself exists.
type CampaignSpec struct {
	Name        string
	Description string
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
