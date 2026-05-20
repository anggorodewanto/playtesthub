// Package adt wraps the AccelByte Development Toolkit (ADT) API surface
// playtesthub depends on for the ADT distribution model (PRD §4.8 /
// docs/STATUS_M5.md Track B).
//
// The service layer talks to Client (an interface) so unit tests can
// drop in MemClient without standing up the SDK. Production wires the
// SDK-backed adapter once ADT engineering publishes endpoint URLs +
// the per-applicant URL surface (STATUS_M5.md Open Questions §1–2);
// MemClient is the default in bootapp until then so the full ADT code
// path runs through the smoke harness without a real ADT round-trip.
//
// Auth: every ADT API call carries the playtesthub AGS service IAM JWT
// (existing AGS_IAM_CLIENT_* env vars) as Authorization: Bearer …; ADT
// validates against AGS JWKS and reads studio identity from the
// token's iss URL + union_namespace claim. No separate credential is
// exchanged at link time — see STATUS_M5.md D2 and the no-credential
// resolution row.
package adt

import (
	"context"
	"time"
)

// Client is the minimum ADT API surface playtesthub needs.
//
// Linkage state on the ADT side is a flag keyed (adt_namespace,
// studio_namespace) — see STATUS_M5.md D2. ListBuilds and
// IssueDownloadURL refuse calls whose (adt_namespace, studio_namespace)
// pair has no flag with a ClientError so the service layer can surface
// FailedPrecondition "adt linkage no longer exists or service token
// rejected, re-link required" per docs/errors.md.
//
// Per the 2026-05-20 ADT-eng spec (docs/STATUS_M5.md Open Questions §1):
//
//	GET <ADT_BASE>/profiling/namespaces/<ADT_NAMESPACE>/agsplaytesthub/games/<ADT_GAME_ID>/builds
//	GET <ADT_BASE>/profiling/namespaces/<ADT_NAMESPACE>/agsplaytesthub/games/<ADT_GAME_ID>/builds/<ADT_BUILD_ID>/downloadUrls
//	DELETE <ADT_BASE>/profiling/namespaces/<ADT_NAMESPACE>/agsplaytesthub/linkage
//
// Per-applicant URLs are NOT supported — IssueDownloadURL returns the
// same per-build URL for every applicant; ADT bounds the URL with a
// 24-hour CDN TTL. The IssueDownloadURLParams.ApplicantIdent field is
// retained for forward compatibility but is unused by both the
// MemClient and the live adapter; the service layer continues to pass
// the applicant id through so audit logs can still attribute the
// download attempt to a specific applicant.
type Client interface {
	// ListBuilds returns every build under the given ADT namespace +
	// game. Used by the admin create-playtest form's build picker and
	// by CreatePlaytest's defense-in-depth check that the supplied
	// adt_build_id belongs to the (adt_namespace, adt_game_id) pair.
	//
	// studioNamespace is the calling playtesthub studio derived from
	// the backend's AGS service token (union_namespace ?? namespace).
	// ADT validates the (adt_namespace, studio_namespace) linkage flag
	// exists before returning rows.
	ListBuilds(ctx context.Context, studioNamespace, adtNamespace, adtGameID string) ([]Build, error)

	// IssueDownloadURL asks ADT to mint a per-build, time-bounded
	// download URL. Per the 2026-05-20 ADT spec, the URL is per-build
	// (not per-applicant) and TTL is fixed at 24h on the CDN. Returns
	// the first URL when ADT surfaces multiple files (build assets) —
	// playtest builds are expected to be a single file but the API
	// allows multiple. Linkage missing / revoked → ErrLinkageMissing.
	//
	// Per STATUS_M5.md B6, the service layer falls back to the
	// playtest's adtFallbackDownloadUrl when ADT returns a non-401
	// transient error and surfaces Unavailable when retries exhaust.
	IssueDownloadURL(ctx context.Context, params IssueDownloadURLParams) (IssuedDownloadURL, error)
}

// Build is the minimum build row the admin UI / pth CLI needs to drive
// the build picker. ADT's full build payload is larger; we copy only
// what we display.
//
// Field mapping from the 2026-05-20 ADT spec:
//
//	ID         ← ADT `id`                (uuid)
//	Name       ← ADT `game_version_name` (operator-visible build label)
//	Version    ← ADT `game_version_id`   (uuid; rendered as a
//	                                       short-hash-style version tag)
//	UploadedAt ← ADT `created_at`        (RFC3339)
//	Platform   ← ADT `platform_name`     (e.g. "windows")
type Build struct {
	ID         string
	Name       string
	Version    string
	UploadedAt time.Time
	// Platform is the ADT-reported target platform ("windows" /
	// "linux" / etc.). Empty when ADT does not surface it.
	Platform string
}

// IssueDownloadURLParams is the input shape for IssueDownloadURL.
// Carrying every identifier on a struct keeps the call site readable
// and lets ADT's eventual SDK signature evolve without churning every
// service-layer call site.
type IssueDownloadURLParams struct {
	StudioNamespace string
	ADTNamespace    string
	ADTGameID       string
	ADTBuildID      string
	// ApplicantIdent is the playtesthub-side applicant id used for
	// audit attribution only — per the 2026-05-20 ADT spec ADT does
	// NOT scope the issued URL on this value (per-build URLs only).
	ApplicantIdent string
}

// IssuedDownloadURL carries the ADT-minted URL + its expiry. ExpiresAt
// is zero when ADT does not surface an expiry (the DM body in
// docs/dm-queue.md "ADT distribution" omits the expiry line in that
// case).
type IssuedDownloadURL struct {
	URL       string
	ExpiresAt time.Time
}
