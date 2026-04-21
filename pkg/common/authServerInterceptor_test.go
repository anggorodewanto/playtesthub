// Copyright (c) 2026 AccelByte Inc. All Rights Reserved.

package common

import (
	"testing"

	"google.golang.org/grpc/metadata"
)

func TestExtractBearerToken_FromAuthorization(t *testing.T) {
	meta := metadata.New(map[string]string{"authorization": "Bearer abc.def.ghi"})
	got := extractBearerToken(meta)
	if got != "abc.def.ghi" {
		t.Fatalf("want abc.def.ghi, got %q", got)
	}
}

func TestExtractBearerToken_FromCookieFallback(t *testing.T) {
	meta := metadata.New(map[string]string{
		"cookie": "_ga=GA1.2.3; access_token=cookie.jwt.value; _zitok=x",
	})
	got := extractBearerToken(meta)
	if got != "cookie.jwt.value" {
		t.Fatalf("want cookie.jwt.value, got %q", got)
	}
}

func TestExtractBearerToken_AuthorizationWinsOverCookie(t *testing.T) {
	meta := metadata.Pairs(
		"authorization", "Bearer header.jwt",
		"cookie", "access_token=cookie.jwt",
	)
	got := extractBearerToken(meta)
	if got != "header.jwt" {
		t.Fatalf("authorization metadata should take precedence, got %q", got)
	}
}

func TestExtractBearerToken_MissingBoth(t *testing.T) {
	meta := metadata.New(map[string]string{"cookie": "_ga=GA1.2.3"})
	if got := extractBearerToken(meta); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestExtractBearerToken_EmptyBearerFallsThroughToCookie(t *testing.T) {
	meta := metadata.Pairs(
		"authorization", "Bearer ",
		"cookie", "access_token=cookie.jwt",
	)
	if got := extractBearerToken(meta); got != "cookie.jwt" {
		t.Fatalf("want cookie.jwt, got %q", got)
	}
}
