// Package iam wraps the AccelByte IAM SDK with the narrow interface the
// rest of the service consumes: token validation (delegated to the SDK)
// and a context-plumbed AGS actorUserId.
//
// The SDK's validator.AuthTokenValidator verifies signatures and role
// permissions but does not surface the subject claim. We decode `sub`
// ourselves after the SDK has validated the token, stash it via
// WithActorUserID, and let handlers read it through ActorUserIDFromContext.
// The audit-log writer and the signup handler both depend on this —
// without it we cannot attribute admin actions or correlate players to
// their AGS identities.
package iam

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

type (
	actorCtxKey     struct{}
	discordIDCtxKey struct{}
)

// WithActorUserID returns a child context tagged with the given AGS user
// id. An empty id is a no-op — we only want the key present when there's
// a real value to read.
func WithActorUserID(ctx context.Context, userID string) context.Context {
	if userID == "" {
		return ctx
	}
	return context.WithValue(ctx, actorCtxKey{}, userID)
}

// ActorUserIDFromContext returns the AGS user id stashed by
// WithActorUserID. The boolean is false when no id is attached — callers
// that require one should reject the request with codes.Unauthenticated.
func ActorUserIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(actorCtxKey{}).(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

// WithDiscordID tags the context with the Discord snowflake the caller
// federated in as. Signup uses it for the bot-token handle lookup
// (PRD §10 M1). Empty input is a no-op so a Discord-less token (service
// account, non-federated login) just leaves the key absent.
func WithDiscordID(ctx context.Context, discordID string) context.Context {
	if discordID == "" {
		return ctx
	}
	return context.WithValue(ctx, discordIDCtxKey{}, discordID)
}

// DiscordIDFromContext returns the caller's Discord snowflake if the
// auth interceptor extracted one from the AGS IAM token.
func DiscordIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(discordIDCtxKey{}).(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

// Claims is the narrow slice of JWT claims we care about. The AGS token
// carries many more fields; we only parse what the service consumes.
//
// Discord federation populates `platform_id` ("discord") and
// `platform_user_id` (the Discord snowflake); non-federated logins leave
// both empty.
type Claims struct {
	Sub            string `json:"sub"`
	PlatformID     string `json:"platform_id"`
	PlatformUserID string `json:"platform_user_id"`
}

// DecodeClaims parses the payload segment of a JWT without verifying its
// signature. Intended to run *after* the AGS SDK's validator has accepted
// the token, so the bytes are trusted; this function exists because the
// SDK does not expose the parsed claims.
func DecodeClaims(token string) (*Claims, error) {
	token = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(token), "Bearer "))
	if token == "" {
		return nil, fmt.Errorf("jwt: empty token")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("jwt: expected 3 segments, got %d", len(parts))
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("jwt: decode payload: %w", err)
	}
	var claims Claims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return nil, fmt.Errorf("jwt: unmarshal payload: %w", err)
	}
	return &claims, nil
}

// DecodeSubject is a shim over DecodeClaims for callers that only care
// about `sub`. Missing sub is a token-shape error surfaced as such.
func DecodeSubject(token string) (string, error) {
	c, err := DecodeClaims(token)
	if err != nil {
		return "", err
	}
	if c.Sub == "" {
		return "", fmt.Errorf("jwt: missing sub claim")
	}
	return c.Sub, nil
}

// DiscordIDFromClaims returns the Discord snowflake when the token came
// from a Discord-federated login. Returns "" if the user authenticated
// through any other IdP.
func DiscordIDFromClaims(c *Claims) string {
	if c == nil {
		return ""
	}
	if !strings.EqualFold(c.PlatformID, "discord") {
		return ""
	}
	return c.PlatformUserID
}
