package adt

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"sync"
	"time"
)

// MemClient is the in-memory Client used by unit tests, the e2e
// harness, and any boot path without a configured ADT base URL
// (development + smoke). It mirrors the ADT happy-path shapes but
// does no network IO.
//
// State organisation:
//   - linkage: set of (studio_namespace, adt_namespace) pairs that
//     ADT would have a "linked = true" flag for (STATUS_M5.md D2).
//     ListBuilds + IssueDownloadURL refuse calls whose pair is
//     missing with ErrLinkageMissing, matching the production
//     contract (B6).
//   - builds[(adt_namespace, adt_game_id)] = []Build returned by
//     ListBuilds. Seeded by tests + smoke fixtures.
//   - issued: log of every minted URL so tests assert per-applicant
//     uniqueness.
type MemClient struct {
	mu sync.Mutex

	linkage map[linkKey]bool
	builds  map[buildsKey][]Build
	issued  []IssuedDownloadURLLog

	// ListBuildsErr / IssueDownloadURLErr force the next call to that
	// method to return the configured error and consume the slot
	// (mirrors pkg/ags.MemClient).
	ListBuildsErr       []error
	IssueDownloadURLErr []error

	// URLTTL is the synthetic expiry MemClient stamps on every
	// IssuedDownloadURL. Zero (the default) leaves ExpiresAt zero so
	// tests can pin the no-expiry DM-body branch from
	// docs/dm-queue.md.
	URLTTL time.Duration
}

type linkKey struct {
	studio string
	adt    string
}

type buildsKey struct {
	adt  string
	game string
}

// IssuedDownloadURLLog records one minted URL for test assertions.
type IssuedDownloadURLLog struct {
	StudioNamespace string
	ADTNamespace    string
	ADTGameID       string
	ADTBuildID      string
	ApplicantIdent  string
	URL             string
	IssuedAt        time.Time
}

// NewMemClient constructs an empty MemClient.
func NewMemClient() *MemClient {
	return &MemClient{
		linkage: make(map[linkKey]bool),
		builds:  make(map[buildsKey][]Build),
	}
}

// RecordLinkage marks (studio_namespace, adt_namespace) as linked on
// the simulated ADT side. The real CompleteADTLink flow flips this
// flag implicitly on ADT; MemClient exposes it as an explicit helper
// so tests + the linkage-completion fixture can stand in for the
// redirect round-trip.
func (c *MemClient) RecordLinkage(studioNamespace, adtNamespace string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.linkage[linkKey{studio: studioNamespace, adt: adtNamespace}] = true
}

// ClearLinkage drops the linkage flag (mirrors the ADT side of
// UnlinkADT — B4). Used by tests that assert ErrLinkageMissing on
// post-unlink calls.
func (c *MemClient) ClearLinkage(studioNamespace, adtNamespace string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.linkage, linkKey{studio: studioNamespace, adt: adtNamespace})
}

// IsLinked reports whether (studio_namespace, adt_namespace) currently
// carries a linkage flag (test helper).
func (c *MemClient) IsLinked(studioNamespace, adtNamespace string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.linkage[linkKey{studio: studioNamespace, adt: adtNamespace}]
}

// SeedBuilds registers the build fixture ListBuilds returns for the
// (adt_namespace, adt_game_id) pair. Test fixtures call this before
// driving ListBuilds.
func (c *MemClient) SeedBuilds(adtNamespace, adtGameID string, builds []Build) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := buildsKey{adt: adtNamespace, game: adtGameID}
	cp := make([]Build, len(builds))
	copy(cp, builds)
	c.builds[key] = cp
}

// IssuedURLs returns a snapshot of every URL minted by
// IssueDownloadURL (test helper).
func (c *MemClient) IssuedURLs() []IssuedDownloadURLLog {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]IssuedDownloadURLLog, len(c.issued))
	copy(out, c.issued)
	return out
}

// ListBuilds returns the seeded builds for (adt_namespace,
// adt_game_id) or ErrLinkageMissing when the linkage flag is absent.
func (c *MemClient) ListBuilds(_ context.Context, studioNamespace, adtNamespace, adtGameID string) ([]Build, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := pop(&c.ListBuildsErr); err != nil {
		return nil, err
	}
	if !c.linkage[linkKey{studio: studioNamespace, adt: adtNamespace}] {
		return nil, ErrLinkageMissing
	}
	src := c.builds[buildsKey{adt: adtNamespace, game: adtGameID}]
	out := make([]Build, len(src))
	copy(out, src)
	// Stable sort newest-first so the admin picker can show "latest
	// build at the top" without re-sorting client-side.
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].UploadedAt.After(out[j].UploadedAt)
	})
	return out, nil
}

// IssueDownloadURL synthesises a deterministic-shape URL of the form
// adt-mem://<adt-ns>/<game>/<build>/<random-hex>?applicant=<ident>
// and logs it for test assertions. ErrLinkageMissing surfaces when
// the linkage flag is absent.
func (c *MemClient) IssueDownloadURL(_ context.Context, params IssueDownloadURLParams) (IssuedDownloadURL, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := pop(&c.IssueDownloadURLErr); err != nil {
		return IssuedDownloadURL{}, err
	}
	if !c.linkage[linkKey{studio: params.StudioNamespace, adt: params.ADTNamespace}] {
		return IssuedDownloadURL{}, ErrLinkageMissing
	}
	nonce := randHex(8)
	url := fmt.Sprintf("adt-mem://%s/%s/%s/%s?applicant=%s",
		params.ADTNamespace, params.ADTGameID, params.ADTBuildID, nonce, params.ApplicantIdent)
	var expires time.Time
	if c.URLTTL > 0 {
		expires = time.Now().Add(c.URLTTL)
	}
	c.issued = append(c.issued, IssuedDownloadURLLog{
		StudioNamespace: params.StudioNamespace,
		ADTNamespace:    params.ADTNamespace,
		ADTGameID:       params.ADTGameID,
		ADTBuildID:      params.ADTBuildID,
		ApplicantIdent:  params.ApplicantIdent,
		URL:             url,
		IssuedAt:        time.Now(),
	})
	return IssuedDownloadURL{URL: url, ExpiresAt: expires}, nil
}

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
		return "deadbeef"
	}
	return hex.EncodeToString(b)
}
