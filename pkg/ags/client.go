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
type Client interface {
	// CreateItem provisions an ENTITLEMENT-type Item under the
	// configured namespace and returns the AGS-assigned item id
	// (PRD §4.6 step 2a).
	CreateItem(ctx context.Context, spec ItemSpec) (string, error)

	// CreateCampaign provisions a Campaign referencing itemID and
	// returns the AGS-assigned campaign id (PRD §4.6 step 2b).
	CreateCampaign(ctx context.Context, spec CampaignSpec) (string, error)

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

// CampaignSpec is the input shape for CreateCampaign.
type CampaignSpec struct {
	Name        string
	Description string
	ItemID      string
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
