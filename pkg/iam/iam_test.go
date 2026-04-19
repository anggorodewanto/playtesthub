package iam_test

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/anggorodewanto/playtesthub/pkg/iam"
)

func TestActorUserID_RoundTrip(t *testing.T) {
	ctx := iam.WithActorUserID(context.Background(), "user-123")
	got, ok := iam.ActorUserIDFromContext(ctx)
	if !ok {
		t.Fatal("ActorUserIDFromContext returned ok=false")
	}
	if got != "user-123" {
		t.Errorf("got %q, want user-123", got)
	}
}

func TestActorUserID_Absent(t *testing.T) {
	_, ok := iam.ActorUserIDFromContext(context.Background())
	if ok {
		t.Fatal("expected ok=false for ctx without actor")
	}
}

func TestActorUserID_EmptyStringIgnored(t *testing.T) {
	ctx := iam.WithActorUserID(context.Background(), "")
	if _, ok := iam.ActorUserIDFromContext(ctx); ok {
		t.Fatal("empty actor should not be stored")
	}
}

func TestDecodeSubject_Valid(t *testing.T) {
	// header.payload.signature — only the payload's `sub` is used.
	token := makeJWT(t, `{"sub":"abc-def","namespace":"playtesthub-dev"}`)
	sub, err := iam.DecodeSubject(token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sub != "abc-def" {
		t.Errorf("sub = %q, want abc-def", sub)
	}
}

func TestDecodeSubject_BearerPrefixStripped(t *testing.T) {
	token := "Bearer " + makeJWT(t, `{"sub":"user-42"}`)
	sub, err := iam.DecodeSubject(token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sub != "user-42" {
		t.Errorf("sub = %q, want user-42", sub)
	}
}

func TestDecodeSubject_MissingSub(t *testing.T) {
	token := makeJWT(t, `{"namespace":"x"}`)
	if _, err := iam.DecodeSubject(token); err == nil {
		t.Fatal("expected error for missing sub")
	}
}

func TestDecodeSubject_MalformedToken(t *testing.T) {
	cases := []string{
		"",
		"onlytwo.parts",
		"not.base64!.nope",
	}
	for _, tc := range cases {
		if _, err := iam.DecodeSubject(tc); err == nil {
			t.Errorf("expected error for %q", tc)
		}
	}
}

func TestDiscordID_RoundTrip(t *testing.T) {
	ctx := iam.WithDiscordID(context.Background(), "snowflake-1")
	got, ok := iam.DiscordIDFromContext(ctx)
	if !ok {
		t.Fatal("DiscordIDFromContext returned ok=false")
	}
	if got != "snowflake-1" {
		t.Errorf("got %q, want snowflake-1", got)
	}
}

func TestDiscordID_EmptyIgnored(t *testing.T) {
	ctx := iam.WithDiscordID(context.Background(), "")
	if _, ok := iam.DiscordIDFromContext(ctx); ok {
		t.Fatal("empty discord id should not be stored")
	}
}

func TestDecodeClaims_PullsPlatformFields(t *testing.T) {
	token := makeJWT(t, `{"sub":"user-1","platform_id":"discord","platform_user_id":"999"}`)
	c, err := iam.DecodeClaims(token)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got := iam.DiscordIDFromClaims(c); got != "999" {
		t.Errorf("discord id = %q, want 999", got)
	}
}

func TestDiscordIDFromClaims_NonDiscordPlatform_Empty(t *testing.T) {
	token := makeJWT(t, `{"sub":"user-1","platform_id":"steam","platform_user_id":"76561"}`)
	c, err := iam.DecodeClaims(token)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got := iam.DiscordIDFromClaims(c); got != "" {
		t.Errorf("discord id = %q, want empty (platform=steam)", got)
	}
}

// makeJWT builds a signatureless JWT whose payload is the given JSON. The
// signature segment is a constant — DecodeSubject does not verify it; the
// AGS SDK has already done so by the time this helper is used in
// production.
func makeJWT(t *testing.T, payloadJSON string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(payloadJSON))
	return strings.Join([]string{header, payload, "sig"}, ".")
}
