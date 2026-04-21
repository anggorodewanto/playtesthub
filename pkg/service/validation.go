package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/anggorodewanto/playtesthub/pkg/pb/playtesthub/v1"
)

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

// platformsErr wraps a platform conversion error so callers can surface
// the field name cleanly.
func wrapPlatformsErr(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("platforms: %w", err)
}
