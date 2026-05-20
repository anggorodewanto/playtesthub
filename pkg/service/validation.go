package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
	"github.com/anggorodewanto/playtesthub/pkg/repo"
)

// parseReqUUID parses a request-side UUID field, returning the canonical
// codes.InvalidArgument "<field> is not a uuid: %v" status used across
// pkg/service. Centralises the byte-exact error message.
func parseReqUUID(field, raw string) (uuid.UUID, error) {
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, status.Errorf(codes.InvalidArgument, "%s is not a uuid: %v", field, err)
	}
	return id, nil
}

// playtestSoftDelete returns p.DeletedAt safely when p may be nil — the
// repo Get* helpers return (nil, err) on miss, so callers cannot read
// the field directly when threading mapPlaytestLookupErr.
func playtestSoftDelete(p *repo.Playtest) *time.Time {
	if p == nil {
		return nil
	}
	return p.DeletedAt
}

// mapPlaytestLookupErr collapses the canonical playtest-lookup result
// triad — repo.ErrNotFound, generic repo error, or soft-deleted row —
// into the matching gRPC status. Returns nil when the row is healthy.
//
// Pass soft=nil at call sites that intentionally surface deleted rows
// (e.g. AdminGetPlaytest); pass &p.DeletedAt elsewhere so the soft-delete
// collapse to NotFound stays uniform across the service. op labels the
// codes.Internal wrap for non-ErrNotFound repo failures.
func mapPlaytestLookupErr(err error, soft *time.Time, op string) error {
	if errors.Is(err, repo.ErrNotFound) {
		return status.Error(codes.NotFound, "playtest not found")
	}
	if err != nil {
		return status.Errorf(codes.Internal, "%s: %v", op, err)
	}
	if soft != nil {
		return status.Error(codes.NotFound, "playtest not found")
	}
	return nil
}

const (
	maxTitleLen       = 200
	maxDescriptionLen = 10_000
	maxBannerURLLen   = 2_048
	maxNamespacePlayt = 100
)

// slugRegex enforces PRD §5.1 L144: ^[a-z0-9][a-z0-9-]{2,63}$
var slugRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,63}$`)

// validateSlug returns nil when s matches PRD §5.1 slug rules, else an
// InvalidArgument status naming the offending value.
func validateSlug(s string) error {
	if !slugRegex.MatchString(s) {
		return status.Errorf(codes.InvalidArgument, "slug %q does not match %s", s, slugRegex.String())
	}
	return nil
}

// validateTitle enforces the 200-char cap from PRD §5.1.
func validateTitle(s string) error {
	if s == "" {
		return status.Error(codes.InvalidArgument, "title must not be empty")
	}
	if len(s) > maxTitleLen {
		return status.Errorf(codes.InvalidArgument, "title exceeds %d chars (got %d)", maxTitleLen, len(s))
	}
	return nil
}

func validateDescription(s string) error {
	if len(s) > maxDescriptionLen {
		return status.Errorf(codes.InvalidArgument, "description exceeds %d chars (got %d)", maxDescriptionLen, len(s))
	}
	return nil
}

// validateBannerURL enforces PRD §5.1: https-only, max 2048 chars.
// Empty is accepted — `bannerImageUrl` is optional.
func validateBannerURL(s string) error {
	if s == "" {
		return nil
	}
	if len(s) > maxBannerURLLen {
		return status.Errorf(codes.InvalidArgument, "banner_image_url exceeds %d chars (got %d)", maxBannerURLLen, len(s))
	}
	u, err := url.Parse(s)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "banner_image_url is not a valid URL: %v", err)
	}
	if u.Scheme != "https" {
		return status.Errorf(codes.InvalidArgument, "banner_image_url must use https (got %q)", u.Scheme)
	}
	return nil
}

// validateNDA rejects the nonsensical "required with no text" shape.
// hashNDA("") returns "" which the player app renders as an empty modal
// + infinite re-accept loop — the request must carry text when required.
func validateNDA(required bool, text string) error {
	if required && text == "" {
		return status.Error(codes.InvalidArgument, "nda_text is required when nda_required is true")
	}
	return nil
}

// validateWindow enforces PRD §5.1 "Window-driven auto-transition" — when
// both startsAt and endsAt are set, endsAt must be strictly after startsAt.
// Either side individually nil is valid (asymmetric / manual modes per the
// nullable-date matrix). Both nil is also valid (fully manual).
func validateWindow(startsAt, endsAt *time.Time) error {
	if startsAt == nil || endsAt == nil {
		return nil
	}
	if !endsAt.After(*startsAt) {
		return status.Error(codes.InvalidArgument, "ends_at must be after starts_at")
	}
	return nil
}

// platformsToStrings renders the proto enum as the TEXT[] values the DB
// stores (see migration 0001). Unspecified is rejected — it means the
// client omitted a platform.
func platformsToStrings(ps []pb.Platform) ([]string, error) {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		switch p {
		case pb.Platform_PLATFORM_STEAM:
			out = append(out, "STEAM")
		case pb.Platform_PLATFORM_XBOX:
			out = append(out, "XBOX")
		case pb.Platform_PLATFORM_PLAYSTATION:
			out = append(out, "PLAYSTATION")
		case pb.Platform_PLATFORM_EPIC:
			out = append(out, "EPIC")
		case pb.Platform_PLATFORM_OTHER:
			out = append(out, "OTHER")
		default:
			return nil, status.Errorf(codes.InvalidArgument, "platform %q is unspecified or unknown", p.String())
		}
	}
	return out, nil
}

// stringsToPlatforms is the inverse used when rendering Playtest rows
// back to protobuf.
func stringsToPlatforms(ss []string) []pb.Platform {
	out := make([]pb.Platform, 0, len(ss))
	for _, s := range ss {
		switch s {
		case "STEAM":
			out = append(out, pb.Platform_PLATFORM_STEAM)
		case "XBOX":
			out = append(out, pb.Platform_PLATFORM_XBOX)
		case "PLAYSTATION":
			out = append(out, pb.Platform_PLATFORM_PLAYSTATION)
		case "EPIC":
			out = append(out, pb.Platform_PLATFORM_EPIC)
		case "OTHER":
			out = append(out, pb.Platform_PLATFORM_OTHER)
		}
	}
	return out
}

// normalizeNDA applies PRD §5.3 whitespace normalization before hashing:
// trim trailing whitespace per line, CRLF → LF, collapse trailing newlines
// to a single terminal LF (empty input stays empty).
func normalizeNDA(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	out := strings.Join(lines, "\n")
	trimmed := strings.TrimRight(out, "\n")
	if trimmed == "" {
		return ""
	}
	return trimmed + "\n"
}

// hashNDA computes sha256(normalize(ndaText)) per PRD §5.3. Empty input
// returns "" — no hash is stored for playtests without NDA text.
func hashNDA(text string) string {
	if text == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(normalizeNDA(text)))
	return hex.EncodeToString(sum[:])
}

// errMsgAutoApproveLimit is the byte-exact errors.md row for an
// auto-approve config with the limit missing or out of bounds. PRD
// §5.4 / errors.md §"Auto-approve".
const errMsgAutoApproveLimit = "auto_approve_limit must be between 1 and 100000 when auto_approve is true"

// autoApproveLimitMin / autoApproveLimitMax bound auto_approve_limit
// (PRD §5.1 / migration 0005 CHECK). 100,000 mirrors the per-AGS-campaign
// cap and the namespace soft cap on outstanding pool size.
const (
	autoApproveLimitMin = 1
	autoApproveLimitMax = 100_000
)

// validateAutoApprove enforces the PRD §5.4 cross-field invariant on
// auto_approve / auto_approve_limit. The DB has the same CHECK
// constraint (migration 0005) so the byte-exact error keeps server +
// client + DB messages aligned.
func validateAutoApprove(autoApprove bool, limit *int32) error {
	if !autoApprove {
		return nil
	}
	if limit == nil {
		return status.Error(codes.InvalidArgument, errMsgAutoApproveLimit)
	}
	if *limit < autoApproveLimitMin || *limit > autoApproveLimitMax {
		return status.Error(codes.InvalidArgument, errMsgAutoApproveLimit)
	}
	return nil
}

// errMsgADTMissingFields / errMsgADTUnsupportedFields / errMsgADTPoolField
// pin the byte-exact errors.md strings for the CreatePlaytest ADT branch
// (PRD §5.1 / docs/errors.md "ADT cross-field").
const (
	errMsgADTMissingFields     = "adt_namespace, adt_game_id, and adt_build_id are required when distribution_model is ADT"
	errMsgADTUnsupportedFields = "adt_namespace, adt_game_id, adt_build_id, and adt_fallback_download_url must not be set when distribution_model is not ADT"
	errMsgADTPoolFieldOnADT    = "initial_code_quantity must not be set when distribution_model is ADT (no code pool; PRD §5.5)"
	maxADTFallbackURLLen       = 2_048
)

// validateADTFields enforces the model↔fields invariant from PRD §5.1.
// Rejects: ADT branch with any of the three identifiers missing; non-ADT
// branch with any ADT field set; ADT branch with adtFallbackDownloadUrl
// that isn't https.
func validateADTFields(isADT bool, adtNamespace, adtGameID, adtBuildID, adtFallback *string) error {
	if !isADT {
		if !nilOrEmpty(adtNamespace) || !nilOrEmpty(adtGameID) || !nilOrEmpty(adtBuildID) || !nilOrEmpty(adtFallback) {
			return status.Error(codes.InvalidArgument, errMsgADTUnsupportedFields)
		}
		return nil
	}
	if nilOrEmpty(adtNamespace) || nilOrEmpty(adtGameID) || nilOrEmpty(adtBuildID) {
		return status.Error(codes.InvalidArgument, errMsgADTMissingFields)
	}
	if err := validateADTFallbackURL(adtFallback); err != nil {
		return err
	}
	return nil
}

// validateADTFallbackURL enforces https-only + 2048-char cap on the
// per-playtest static fallback URL. Empty/nil is accepted — the field is
// optional. Shape mirrors validateBannerURL.
func validateADTFallbackURL(p *string) error {
	if nilOrEmpty(p) {
		return nil
	}
	s := *p
	if len(s) > maxADTFallbackURLLen {
		return status.Errorf(codes.InvalidArgument, "adt_fallback_download_url exceeds %d chars (got %d)", maxADTFallbackURLLen, len(s))
	}
	u, err := url.Parse(s)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "adt_fallback_download_url is not a valid URL: %v", err)
	}
	if u.Scheme != "https" {
		return status.Errorf(codes.InvalidArgument, "adt_fallback_download_url must use https (got %q)", u.Scheme)
	}
	return nil
}

func nilOrEmpty(p *string) bool {
	return p == nil || *p == ""
}

// platformsErr wraps a platform conversion error so callers can surface
// the field name cleanly.
func wrapPlatformsErr(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("platforms: %w", err)
}
