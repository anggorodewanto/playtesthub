package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func fixedNow(t time.Time) func() time.Time { return func() time.Time { return t } }

func TestIAMClientPasswordLoginHappyPath(t *testing.T) {
	t.Parallel()
	gotForm := url.Values{}
	gotAuth := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != iamTokenPath {
			t.Errorf("path=%s want %s", r.URL.Path, iamTokenPath)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method=%s want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("content-type=%s", ct)
		}
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(body))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "ACCESS",
			"refresh_token": "REFRESH",
			"expires_in":    3600,
			"token_type":    "Bearer",
			"user_id":       "user-1",
			"namespace":     "ns",
		})
	}))
	defer srv.Close()

	c := &iamClient{BaseURL: srv.URL, ClientID: "cli-id", ClientSecret: "cli-secret", HTTPClient: srv.Client()}
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	tok, err := c.passwordLogin(context.Background(), "ns", "alice", "s3cret", fixedNow(now))
	if err != nil {
		t.Fatalf("passwordLogin: %v", err)
	}
	if tok.AccessToken != "ACCESS" || tok.RefreshToken != "REFRESH" || tok.UserID != "user-1" || tok.Namespace != "ns" {
		t.Errorf("tok=%+v", tok)
	}
	if want := now.Add(time.Hour); !tok.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt=%s want %s", tok.ExpiresAt, want)
	}
	if gotForm.Get("grant_type") != "password" || gotForm.Get("username") != "alice" || gotForm.Get("password") != "s3cret" || gotForm.Get("namespace") != "ns" {
		t.Errorf("form=%v", gotForm)
	}
	// Basic auth should carry both client id + secret.
	if !strings.HasPrefix(gotAuth, "Basic ") {
		t.Errorf("authorization header = %q, want Basic", gotAuth)
	}
}

func TestIAMClientPasswordLoginInvalidGrant(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":             "invalid_grant",
			"error_description": "Invalid username or password",
		})
	}))
	defer srv.Close()
	c := &iamClient{BaseURL: srv.URL, ClientID: "cli", HTTPClient: srv.Client()}
	_, err := c.passwordLogin(context.Background(), "ns", "u", "p", time.Now)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	var ie *iamError
	if !errors.As(err, &ie) {
		t.Fatalf("want *iamError, got %T: %v", err, err)
	}
	if !ie.IsInvalidGrant() {
		t.Errorf("IsInvalidGrant=false; err=%v", err)
	}
	if ie.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode=%d", ie.StatusCode)
	}
	if !strings.Contains(ie.Description, "Invalid username") {
		t.Errorf("Description=%q", ie.Description)
	}
}

func TestIAMClientRefreshHappyPath(t *testing.T) {
	t.Parallel()
	gotForm := url.Values{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(body))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "NEW_ACCESS",
			"refresh_token": "NEW_REFRESH",
			"expires_in":    300,
			"token_type":    "Bearer",
			"user_id":       "user-1",
			"namespace":     "ns",
		})
	}))
	defer srv.Close()
	c := &iamClient{BaseURL: srv.URL, ClientID: "cli", HTTPClient: srv.Client()}
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	tok, err := c.refresh(context.Background(), "OLD_REFRESH", fixedNow(now))
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if tok.AccessToken != "NEW_ACCESS" || tok.RefreshToken != "NEW_REFRESH" {
		t.Errorf("tok=%+v", tok)
	}
	if !tok.ExpiresAt.Equal(now.Add(5 * time.Minute)) {
		t.Errorf("ExpiresAt=%s", tok.ExpiresAt)
	}
	if gotForm.Get("grant_type") != "refresh_token" || gotForm.Get("refresh_token") != "OLD_REFRESH" {
		t.Errorf("form=%v", gotForm)
	}
}

func TestIAMClientRefreshEmptyTokenIsLocalError(t *testing.T) {
	t.Parallel()
	c := &iamClient{BaseURL: "https://example.invalid", ClientID: "cli"}
	_, err := c.refresh(context.Background(), "", time.Now)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "refresh token is empty") {
		t.Errorf("err=%v", err)
	}
}

func TestIAMClientPasswordLoginMissingBaseURL(t *testing.T) {
	t.Parallel()
	c := &iamClient{ClientID: "cli"}
	_, err := c.passwordLogin(context.Background(), "ns", "u", "p", time.Now)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "AGS base URL not configured") {
		t.Errorf("err=%v", err)
	}
}

func TestIAMClientPasswordLoginMissingClientID(t *testing.T) {
	t.Parallel()
	c := &iamClient{BaseURL: "https://example.invalid"}
	_, err := c.passwordLogin(context.Background(), "ns", "u", "p", time.Now)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "client id not configured") {
		t.Errorf("err=%v", err)
	}
}

func TestIAMClientPasswordLoginMissingAccessTokenIsError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"refresh_token": "r",
			"expires_in":    300,
		})
	}))
	defer srv.Close()
	c := &iamClient{BaseURL: srv.URL, ClientID: "cli", HTTPClient: srv.Client()}
	_, err := c.passwordLogin(context.Background(), "ns", "u", "p", time.Now)
	if err == nil || !strings.Contains(err.Error(), "missing access_token") {
		t.Fatalf("err=%v", err)
	}
}

func TestIAMClientPasswordLoginUserIDFallsBackToJWTSub(t *testing.T) {
	t.Parallel()
	// Hand-rolled JWT with payload {"sub":"user-from-sub"}; signature byte
	// is irrelevant — DecodeSubject does not verify.
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1c2VyLWZyb20tc3ViIn0.sig"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  jwt,
			"refresh_token": "r",
			"expires_in":    300,
		})
	}))
	defer srv.Close()
	c := &iamClient{BaseURL: srv.URL, ClientID: "cli", HTTPClient: srv.Client()}
	tok, err := c.passwordLogin(context.Background(), "ns", "u", "p", time.Now)
	if err != nil {
		t.Fatalf("passwordLogin: %v", err)
	}
	if tok.UserID != "user-from-sub" {
		t.Errorf("UserID=%q want user-from-sub", tok.UserID)
	}
}
