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
// Canonical spec reference: the live ADT swagger at
// https://develop.blackbox.accelbyte.io/profiling/apidocs/api.json
// (no auth required, v1.35.0 as of 2026-05-21). Treat this as
// authoritative over any stale local copies (e.g. earlier
// /home/ab/Downloads/adt-spec.txt drops).
//
// Auth: every ADT API call carries the playtesthub AGS service IAM JWT
// (existing AGS_IAM_CLIENT_* env vars) as Authorization: Bearer …; ADT
// validates against AGS JWKS and reads studio identity from the
// token's iss URL + union_namespace claim. No separate credential is
// exchanged at link time — see STATUS_M5.md D2 and the no-credential
// resolution row.
//
// Error envelopes (verified 2026-05-21 against live API): non-2xx
// responses carry `{"errorCode": int, "errorMessage": string}` when
// Content-Type is application/json; mux-level routing misses carry
// plaintext bodies like "404: Page Not Found". The retry classifier
// dispatches on errorCode first (errors.go for the full mapping) and
// falls back to HTTP status for plaintext bodies.
//
// Response envelopes (verified 2026-05-21):
//   - ListGames + ListBuilds: `{"data": [row,...], ...}` (NOT
//     `{"games": [...]}` / `{"builds": [...]}` as earlier drafts read).
//   - downloadUrls: `{"urls": [string,...], "expires_at": "RFC3339"}`
//     — flat string list with a single top-level expiry, NOT per-URL
//     objects.
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
// Per the 2026-05-20 ADT-eng spec (docs/STATUS_M5.md Open Questions §1)
// plus the 2026-05-21 games-list addendum:
//
//	GET <ADT_BASE>/profiling/namespaces/<ADT_NAMESPACE>/agsplaytesthub/games
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
	// ListGames returns every game registered under the given ADT
	// namespace. Drives the create-playtest build-picker's top-level
	// dropdown (STATUS_M5.md B12 + Addendum 2026-05-21) so operators no
	// longer type adt_game_id by hand.
	//
	// studioNamespace is the calling playtesthub studio derived from
	// the backend's AGS service token; ADT validates the linkage flag
	// keyed on (adt_namespace, studio_namespace) and returns 401 →
	// ErrLinkageMissing when the flag is absent.
	ListGames(ctx context.Context, studioNamespace, adtNamespace string) ([]Game, error)

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
	// download URL list. Per the 2026-05-20 ADT spec the URLs are
	// per-build (not per-applicant) and TTL is fixed at 24h on the CDN.
	// ADT may return multiple URLs when a build is split into multiple
	// assets (game binary + patcher + manifest, multi-platform drops,
	// etc.); IssuedDownloadURL.URLs surfaces the full list in ADT's
	// original order. Linkage missing / revoked → ErrLinkageMissing.
	//
	// Per STATUS_M5.md B6, the service layer falls back to the
	// playtest's adtFallbackDownloadUrl when ADT returns a non-401
	// transient error and surfaces Unavailable when retries exhaust.
	IssueDownloadURL(ctx context.Context, params IssueDownloadURLParams) (IssuedDownloadURL, error)

	// DeleteLinkage best-effort drops the ADT-side linkage flag — the
	// unlink half of B4 / PRD §4.8. studioNamespace is for symmetry +
	// test bookkeeping; ADT derives studio identity from the bearer
	// token on every call.
	//
	// Best-effort semantics: ADT eventual-consistency is tolerated.
	// ErrLinkageMissing surfaces on 401/403 (ADT already has no flag
	// for this pair), and callers are expected to swallow it — the
	// post-state is what we want. Transient errors (ErrUnavailable /
	// 5xx-retry exhaustion) also surface; UnlinkADT logs + bumps a
	// metric on those but still completes the local soft-delete so an
	// ADT outage cannot strand the operator (see pkg/service/adt.go
	// UnlinkADT).
	DeleteLinkage(ctx context.Context, studioNamespace, adtNamespace string) error
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

// Game is the minimum game row the admin UI / pth CLI needs to drive
// the create-playtest build-picker's top-level dropdown. Mirrors
// pkg/adt.Build's shape so the proto + admin codegen stay symmetrical.
//
// Field mapping from the 2026-05-21 ADT-eng addendum (STATUS_M5.md
// "Addendum 2026-05-21 — games-list endpoint"):
//
//	ID        ← ADT `id`         (uuid)
//	Name      ← ADT `name`       (operator-visible game label)
//	CreatedAt ← ADT `created_at` (RFC3339)
type Game struct {
	ID        string
	Name      string
	CreatedAt time.Time
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

// IssuedDownloadURL carries the ADT-minted URL list + its expiry.
// ADT returns one URL per build asset (single-file builds → one
// element; multi-asset builds → many) in ADT's original order; the
// service layer surfaces the full list end-to-end (DM body, audit
// log, GetADTDownloadInfo response). ExpiresAt is zero when ADT does
// not surface an expiry (the DM body in docs/dm-queue.md "ADT
// distribution" omits the expiry line in that case).
type IssuedDownloadURL struct {
	URLs      []string
	ExpiresAt time.Time
}
