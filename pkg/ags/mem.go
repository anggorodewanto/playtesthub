package ags

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"maps"
	"sync"
)

// MemClient is the in-memory Client used by unit tests, the e2e
// harness, and any boot path without a configured AGS namespace
// (development + smoke). It mirrors the AGS happy-path shapes but
// does no network IO. Failure injection knobs let tests reach every
// docs/ags-failure-modes.md cleanup matrix branch.
type MemClient struct {
	mu sync.Mutex

	items     map[string]ItemSpec
	campaigns map[string]CampaignSpec
	codes     map[string][]string

	// CreateItemErr / CreateCampaignErr / CreateCodesErr / FetchCodesErr
	// / DeleteItemErr / DeleteCampaignErr force the next call to that
	// method to return the configured error and consume the slot.
	// Multiple errors per method execute in order (first call returns
	// the first slot, second call the second, etc.).
	CreateItemErr     []error
	CreateCampaignErr []error
	CreateCodesErr    []error
	FetchCodesErr     []error
	DeleteItemErr     []error
	DeleteCampaignErr []error

	// PartialFulfillment, when set for a campaign id, caps CreateCodes
	// at the configured count regardless of the requested quantity.
	// Mirrors docs/ags-failure-modes.md "Partial fulfillment".
	PartialFulfillment map[string]int
}

// NewMemClient constructs an empty MemClient.
func NewMemClient() *MemClient {
	return &MemClient{
		items:              make(map[string]ItemSpec),
		campaigns:          make(map[string]CampaignSpec),
		codes:              make(map[string][]string),
		PartialFulfillment: make(map[string]int),
	}
}

// CreateItem records the spec under a fresh hex id and returns it.
func (c *MemClient) CreateItem(_ context.Context, spec ItemSpec) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := pop(&c.CreateItemErr); err != nil {
		return "", err
	}
	id := "item_" + randHex(8)
	c.items[id] = spec
	return id, nil
}

// CreateCampaign records the spec under a fresh hex id.
func (c *MemClient) CreateCampaign(_ context.Context, spec CampaignSpec) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := pop(&c.CreateCampaignErr); err != nil {
		return "", err
	}
	if _, ok := c.items[spec.ItemID]; !ok {
		return "", &ClientError{StatusCode: 400, Op: "CreateCampaign", Message: "item not found: " + spec.ItemID}
	}
	id := "camp_" + randHex(8)
	c.campaigns[id] = spec
	return id, nil
}

// CreateCodes generates `quantity` deterministic-looking values,
// appends them to the campaign's pool, and returns them. Honors
// PartialFulfillment to exercise the warn-and-commit path.
func (c *MemClient) CreateCodes(_ context.Context, campaignID string, quantity int) (CodeBatchResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := pop(&c.CreateCodesErr); err != nil {
		return CodeBatchResult{}, err
	}
	if _, ok := c.campaigns[campaignID]; !ok {
		return CodeBatchResult{}, &ClientError{StatusCode: 404, Op: "CreateCodes", Message: "campaign not found: " + campaignID}
	}

	actual := quantity
	if limit, ok := c.PartialFulfillment[campaignID]; ok && limit < quantity {
		actual = limit
	}
	out := make([]string, actual)
	existing := len(c.codes[campaignID])
	for i := 0; i < actual; i++ {
		out[i] = fmt.Sprintf("AGS-%s-%05d", campaignID, existing+i+1)
	}
	c.codes[campaignID] = append(c.codes[campaignID], out...)
	return CodeBatchResult{Requested: quantity, Codes: out}, nil
}

// FetchCodes returns a copy of the campaign's pool.
func (c *MemClient) FetchCodes(_ context.Context, campaignID string) ([]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := pop(&c.FetchCodesErr); err != nil {
		return nil, err
	}
	if _, ok := c.campaigns[campaignID]; !ok {
		return nil, &ClientError{StatusCode: 404, Op: "FetchCodes", Message: "campaign not found: " + campaignID}
	}
	src := c.codes[campaignID]
	out := make([]string, len(src))
	copy(out, src)
	return out, nil
}

// DeleteItem removes the item record. Idempotent: a missing id is
// not an error (matches AGS DELETE semantics).
func (c *MemClient) DeleteItem(_ context.Context, itemID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := pop(&c.DeleteItemErr); err != nil {
		return err
	}
	delete(c.items, itemID)
	return nil
}

// DeleteCampaign removes the campaign and its code pool. Idempotent.
func (c *MemClient) DeleteCampaign(_ context.Context, campaignID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := pop(&c.DeleteCampaignErr); err != nil {
		return err
	}
	delete(c.campaigns, campaignID)
	delete(c.codes, campaignID)
	return nil
}

// AllItems returns a snapshot of every registered item id (test
// helper). Map iteration order is non-deterministic.
func (c *MemClient) AllItems() map[string]ItemSpec {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]ItemSpec, len(c.items))
	maps.Copy(out, c.items)
	return out
}

// AllCampaigns returns a snapshot of every registered campaign id
// (test helper).
func (c *MemClient) AllCampaigns() map[string]CampaignSpec {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]CampaignSpec, len(c.campaigns))
	maps.Copy(out, c.campaigns)
	return out
}

// HasItem reports whether itemID is still registered (test helper).
func (c *MemClient) HasItem(itemID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.items[itemID]
	return ok
}

// HasCampaign reports whether campaignID is still registered (test
// helper).
func (c *MemClient) HasCampaign(campaignID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.campaigns[campaignID]
	return ok
}

// pop returns the head of slot (and removes it) or nil when slot is
// empty. Callers hold the MemClient mutex.
func pop(slot *[]error) error {
	if len(*slot) == 0 {
		return nil
	}
	head := (*slot)[0]
	*slot = (*slot)[1:]
	return head
}

func randHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure on a healthy host is a process-level
		// problem; tests fall back to a non-random id so failure
		// classification stays clean.
		return "deadbeef"
	}
	return hex.EncodeToString(b)
}
