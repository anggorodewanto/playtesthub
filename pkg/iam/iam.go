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
	actorCtxKey            struct{}
	discordIDCtxKey        struct{}
	discordFederatedCtxKey struct{}
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

// WithDiscordID tags the context with the caller's Discord snowflake.
// AGS IAM does not include the snowflake in the JWT — the auth path
// only knows the caller is Discord-federated (see WithDiscordFederation).
// Signup looks up the snowflake via PlatformLookup and stashes it here
// so resolveDiscordHandle and the DM enqueue can read a single source.
// Empty input is a no-op.
func WithDiscordID(ctx context.Context, discordID string) context.Context {
	if discordID == "" {
		return ctx
	}
	return context.WithValue(ctx, discordIDCtxKey{}, discordID)
}

// DiscordIDFromContext returns the caller's Discord snowflake if one has
// been stashed via WithDiscordID.
func DiscordIDFromContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(discordIDCtxKey{}).(string)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}

// WithDiscordFederation tags the context as belonging to a caller whose
// AGS token's `ipf` claim is "discord" — i.e. they federated in via
// Discord OAuth. The snowflake itself is not in the token and must be
// fetched separately (see PlatformLookup).
func WithDiscordFederation(ctx context.Context) context.Context {
	return context.WithValue(ctx, discordFederatedCtxKey{}, true)
}

// IsDiscordFederatedFromContext reports whether the auth interceptor
// flagged the caller as Discord-federated for this request.
func IsDiscordFederatedFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(discordFederatedCtxKey{}).(bool)
	return v
}

// Claims is the narrow slice of JWT claims we care about. The AGS token
// carries many more fields; we only parse what the service consumes.
//
// `ipf` (identity provider flag) is set to "discord" when the caller
// federated in via Discord OAuth. The Discord snowflake itself is NOT
// in the JWT — the AGS IAM admin endpoint
// `/users/{userId}/distinctPlatforms` is the source of truth.
//
// `namespace` / `union_namespace` are the AGS IAM namespace claims used
// by the ADT linkage flow (PRD §4.8) to derive the studio identity from
// the *backend's* service-IAM JWT (NOT the calling admin's token —
// linkage scope is the studio the backend itself runs under, which is
// what ADT sees on every downstream API call). StudioNamespace()
// returns `union_namespace ?? namespace`.
type Claims struct {
	Sub            string `json:"sub"`
	IPF            string `json:"ipf"`
	Namespace      string `json:"namespace"`
	UnionNamespace string `json:"union_namespace"`
}

// StudioNamespace returns the studio identity for ADT linkage purposes:
// `union_namespace ?? namespace`. Empty when neither claim is set.
func (c *Claims) StudioNamespace() string {
	if c == nil {
		return ""
	}
	if c.UnionNamespace != "" {
		return c.UnionNamespace
	}
	return c.Namespace
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

// IsDiscordFederated reports whether the AGS token's `ipf` claim marks
// the caller as Discord-federated. The snowflake is not in the JWT;
// callers that need it must hit AGS IAM (PlatformLookup).
func IsDiscordFederated(c *Claims) bool {
	if c == nil {
		return false
	}
	return strings.EqualFold(c.IPF, "discord")
}
