package discord

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLookupHandle_Pomelo_UsesGlobalName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bot test-token" {
			t.Errorf("Authorization header = %q, want \"Bot test-token\"", got)
		}
		if !strings.HasSuffix(r.URL.Path, "/users/123456789") {
			t.Errorf("path = %q, want .../users/123456789", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"username":"alice","discriminator":"0","global_name":"Alice"}`))
	}))
	defer srv.Close()

	c := NewBotClient("test-token").WithBaseURL(srv.URL)
	got, err := c.LookupHandle(context.Background(), "123456789")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got != "Alice" {
		t.Errorf("handle = %q, want Alice", got)
	}
}

func TestLookupHandle_Legacy_AppendsDiscriminator(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"username":"bob","discriminator":"4242"}`))
	}))
	defer srv.Close()

	c := NewBotClient("t").WithBaseURL(srv.URL)
	got, err := c.LookupHandle(context.Background(), "1")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got != "bob#4242" {
		t.Errorf("handle = %q, want bob#4242", got)
	}
}

func TestLookupHandle_NotFound_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Unknown User"}`))
	}))
	defer srv.Close()

	c := NewBotClient("t").WithBaseURL(srv.URL)
	_, err := c.LookupHandle(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error = %v, want to contain 404", err)
	}
}

func TestNewBotClient_EmptyTokenReturnsNil(t *testing.T) {
	if c := NewBotClient(""); c != nil {
		t.Fatalf("expected nil client for empty token, got %+v", c)
	}
}

func TestLookupHandle_NilClient_Error(t *testing.T) {
	var c *BotClient
	_, err := c.LookupHandle(context.Background(), "1")
	if err == nil {
		t.Fatal("expected error for nil client")
	}
}
