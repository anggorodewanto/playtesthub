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

// testIAMUserID is the canonical "fake admin-scope userId" used across
// the admin-endpoint cases. Pulled into a const so the assertions stay
// readable without a goconst lint.
const testIAMUserID = "u-1"

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

func TestIAMClientAdminCreateTestUsersHappyPath(t *testing.T) {
	t.Parallel()
	gotMethod, gotPath, gotAuth, gotCT := "", "", "", ""
	gotBody := map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotCT = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"userId": testIAMUserID, "username": "n", "password": "p", "emailAddress": "e", "namespace": "ns"},
			},
		})
	}))
	defer srv.Close()
	c := &iamClient{BaseURL: srv.URL, ClientID: "cli", HTTPClient: srv.Client()}
	resp, err := c.adminCreateTestUsers(context.Background(), "BEARER", "ns", &adminCreateTestUsersRequest{
		Count:    1,
		UserInfo: &adminTestUserInfo{Country: "ID"},
	})
	if err != nil {
		t.Fatalf("adminCreateTestUsers: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/iam/v4/admin/namespaces/ns/test_users" || gotAuth != "Bearer BEARER" || gotCT != "application/json" {
		t.Errorf("method=%s path=%s auth=%s ct=%s", gotMethod, gotPath, gotAuth, gotCT)
	}
	if gotBody["count"] != float64(1) {
		t.Errorf("count=%v", gotBody["count"])
	}
	if len(resp.Data) != 1 || resp.Data[0].UserID != testIAMUserID {
		t.Errorf("resp=%+v", resp)
	}
}

func TestIAMClientAdminCreateTestUsersRejectsBadCount(t *testing.T) {
	t.Parallel()
	c := &iamClient{BaseURL: "https://example.invalid", ClientID: "cli"}
	_, err := c.adminCreateTestUsers(context.Background(), "BEARER", "ns", &adminCreateTestUsersRequest{Count: 0})
	if err == nil || !strings.Contains(err.Error(), "count must be >= 1") {
		t.Errorf("err=%v", err)
	}
}

func TestIAMClientAdminCreateTestUsersRequiresBearer(t *testing.T) {
	t.Parallel()
	c := &iamClient{BaseURL: "https://example.invalid", ClientID: "cli"}
	_, err := c.adminCreateTestUsers(context.Background(), "", "ns", &adminCreateTestUsersRequest{Count: 1})
	if err == nil || !strings.Contains(err.Error(), "admin token required") {
		t.Errorf("err=%v", err)
	}
}

func TestIAMClientAdminGetUserByIDHappyPath(t *testing.T) {
	t.Parallel()
	gotMethod, gotPath, gotAuth := "", "", ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"userId":    testIAMUserID,
			"userName":  "ags-name",
			"namespace": "ns",
		})
	}))
	defer srv.Close()
	c := &iamClient{BaseURL: srv.URL, ClientID: "cli", HTTPClient: srv.Client()}
	out, err := c.adminGetUserByID(context.Background(), "BEARER", "ns", testIAMUserID)
	if err != nil {
		t.Fatalf("adminGetUserByID: %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/iam/v3/admin/namespaces/ns/users/u-1" || gotAuth != "Bearer BEARER" {
		t.Errorf("method=%s path=%s auth=%s", gotMethod, gotPath, gotAuth)
	}
	if out.Username != "ags-name" {
		t.Errorf("username=%q", out.Username)
	}
}

func TestIAMClientAdminGetUserByIDMissingUserName(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"userId": testIAMUserID})
	}))
	defer srv.Close()
	c := &iamClient{BaseURL: srv.URL, ClientID: "cli", HTTPClient: srv.Client()}
	_, err := c.adminGetUserByID(context.Background(), "BEARER", "ns", testIAMUserID)
	if err == nil || !strings.Contains(err.Error(), "missing userName") {
		t.Errorf("err=%v", err)
	}
}

func TestIAMClientAdminDeleteUserHappyPath(t *testing.T) {
	t.Parallel()
	gotMethod, gotPath := "", ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	c := &iamClient{BaseURL: srv.URL, ClientID: "cli", HTTPClient: srv.Client()}
	if err := c.adminDeleteUser(context.Background(), "BEARER", "ns", testIAMUserID); err != nil {
		t.Fatalf("adminDeleteUser: %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/iam/v3/admin/namespaces/ns/users/u-1/information" {
		t.Errorf("method=%s path=%s", gotMethod, gotPath)
	}
}

func TestIAMClientAdminDeleteUserNotFound(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{"errorCode": 10139, "errorMessage": "user not found"})
	}))
	defer srv.Close()
	c := &iamClient{BaseURL: srv.URL, ClientID: "cli", HTTPClient: srv.Client()}
	err := c.adminDeleteUser(context.Background(), "BEARER", "ns", testIAMUserID)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	var ie *iamError
	if !errors.As(err, &ie) {
		t.Fatalf("want *iamError, got %T", err)
	}
	if ie.StatusCode != http.StatusNotFound || !strings.Contains(ie.Description, "user not found") {
		t.Errorf("ie=%+v", ie)
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
