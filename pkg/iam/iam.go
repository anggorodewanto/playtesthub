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

type actorCtxKey struct{}

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

// DecodeSubject extracts the `sub` claim from a JWT without verifying its
// signature. Intended to run *after* the AGS SDK's validator has accepted
// the token, so the bytes are trusted; this function exists because the
// SDK does not expose the parsed claims.
func DecodeSubject(token string) (string, error) {
	token = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(token), "Bearer "))
	if token == "" {
		return "", fmt.Errorf("jwt: empty token")
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("jwt: expected 3 segments, got %d", len(parts))
	}

	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("jwt: decode payload: %w", err)
	}

	var claims struct {
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		return "", fmt.Errorf("jwt: unmarshal payload: %w", err)
	}
	if claims.Sub == "" {
		return "", fmt.Errorf("jwt: missing sub claim")
	}
	return claims.Sub, nil
}
