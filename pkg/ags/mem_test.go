package ags_test

import (
	"context"
	"errors"
	"testing"

	"github.com/anggorodewanto/playtesthub/pkg/ags"
)

func TestMemClient_HappyPath(t *testing.T) {
	c := ags.NewMemClient()
	ctx := context.Background()

	itemID, err := c.CreateItem(ctx, ags.ItemSpec{Name: "Demo", Description: "desc"})
	if err != nil {
		t.Fatalf("CreateItem: %v", err)
	}
	if !c.HasItem(itemID) {
		t.Fatal("expected item to be registered")
	}

	campaignID, err := c.CreateCampaign(ctx, ags.CampaignSpec{Name: "Demo", ItemID: itemID})
	if err != nil {
		t.Fatalf("CreateCampaign: %v", err)
	}
	if !c.HasCampaign(campaignID) {
		t.Fatal("expected campaign to be registered")
	}

	res, err := c.CreateCodes(ctx, campaignID, 3)
	if err != nil {
		t.Fatalf("CreateCodes: %v", err)
	}
	if len(res.Codes) != 3 || res.Requested != 3 {
		t.Fatalf("CreateCodes shape: req=%d codes=%d", res.Requested, len(res.Codes))
	}

	codes, err := c.FetchCodes(ctx, campaignID)
	if err != nil {
		t.Fatalf("FetchCodes: %v", err)
	}
	if len(codes) != 3 {
		t.Fatalf("FetchCodes len = %d, want 3", len(codes))
	}
}

func TestMemClient_PartialFulfillment(t *testing.T) {
	c := ags.NewMemClient()
	ctx := context.Background()
	itemID, _ := c.CreateItem(ctx, ags.ItemSpec{Name: "x"})
	campID, _ := c.CreateCampaign(ctx, ags.CampaignSpec{Name: "x", ItemID: itemID})

	c.PartialFulfillment[campID] = 4
	res, err := c.CreateCodes(ctx, campID, 10)
	if err != nil {
		t.Fatalf("CreateCodes: %v", err)
	}
	if res.Requested != 10 || len(res.Codes) != 4 {
		t.Fatalf("partial: requested=%d got=%d, want requested=10 got=4", res.Requested, len(res.Codes))
	}
}

func TestMemClient_InjectedFailures(t *testing.T) {
	c := ags.NewMemClient()
	ctx := context.Background()
	c.CreateItemErr = []error{errors.New("boom")}
	if _, err := c.CreateItem(ctx, ags.ItemSpec{Name: "x"}); err == nil {
		t.Fatal("expected injected error")
	}
	// second call succeeds (slot consumed)
	if _, err := c.CreateItem(ctx, ags.ItemSpec{Name: "x"}); err != nil {
		t.Fatalf("second call: %v", err)
	}
}

func TestMemClient_CampaignWithUnknownItem(t *testing.T) {
	c := ags.NewMemClient()
	_, err := c.CreateCampaign(context.Background(), ags.CampaignSpec{ItemID: "nope"})
	if !ags.IsClientError(err) {
		t.Fatalf("expected ClientError, got %v", err)
	}
}

func TestMemClient_DeleteIsIdempotent(t *testing.T) {
	c := ags.NewMemClient()
	itemID, _ := c.CreateItem(context.Background(), ags.ItemSpec{Name: "x"})
	if err := c.DeleteItem(context.Background(), itemID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// second delete on missing id is not an error
	if err := c.DeleteItem(context.Background(), itemID); err != nil {
		t.Fatalf("second delete: %v", err)
	}
}
