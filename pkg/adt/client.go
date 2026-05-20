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
type Client interface {
	// ListBuilds returns every build under the given ADT namespace +
	// game. Used by the admin create-playtest form's build picker and
	// by CreatePlaytest's defense-in-depth check that the supplied
	// adt_build_id belongs to the (adt_namespace, adt_game_id) pair.
	//
	// studioNamespace is the calling playtesthub studio derived from
	// the admin's AGS token (union_namespace ?? namespace). ADT
	// validates the (adt_namespace, studio_namespace) linkage flag
	// exists before returning rows.
	ListBuilds(ctx context.Context, studioNamespace, adtNamespace, adtGameID string) ([]Build, error)

	// IssueDownloadURL asks ADT to mint a per-applicant, time-bounded
	// build download URL. The applicantIdent is the playtesthub-side
	// applicant identity ADT logs for per-tester audit/revoke (the
	// exact shape is TBD-from-ADT-eng — STATUS_M5.md Open Question §1
	// — and pinned by the live adapter when it lands).
	//
	// Per STATUS_M5.md B6, the service layer falls back to the
	// playtest's adtFallbackDownloadUrl when ADT can't issue a
	// per-applicant URL (ClientError) and surfaces Unavailable when
	// retries exhaust.
	IssueDownloadURL(ctx context.Context, params IssueDownloadURLParams) (IssuedDownloadURL, error)
}

// Build is the minimum build row the admin UI / pth CLI needs to drive
// the build picker. ADT's full build payload is larger; we copy only
// what we display.
type Build struct {
	// ID is the ADT-assigned build identifier persisted as
	// playtest.adt_build_id (PRD §5.1).
	ID string
	// Name is the operator-visible label rendered in the picker.
	Name string
	// Version is the optional build version string (semver / short
	// hash / ADT-defined). Empty when the build has no version.
	Version string
	// UploadedAt is the build's upload timestamp; admin picker sorts
	// descending on this. Zero when ADT does not surface it.
	UploadedAt time.Time
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
	// ApplicantIdent is the playtesthub-side identity ADT logs for
	// per-tester audit. Exact shape TBD (STATUS_M5.md Open Question
	// §1) — the service layer passes through whatever the live
	// adapter pins.
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
