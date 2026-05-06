package discord

import (
	"context"
	"io"
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

func TestSendDM_HappyPath_OpensChannelAndPosts(t *testing.T) {
	var (
		gotOpenBody string
		gotPostBody string
		postPath    string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bot t" {
			t.Errorf("Authorization = %q", got)
		}
		body, _ := io.ReadAll(r.Body)
		switch {
		case r.URL.Path == "/users/@me/channels":
			gotOpenBody = string(body)
			_, _ = w.Write([]byte(`{"id":"chan-42"}`))
		case strings.HasPrefix(r.URL.Path, "/channels/chan-42/messages"):
			postPath = r.URL.Path
			gotPostBody = string(body)
			_, _ = w.Write([]byte(`{"id":"msg-1"}`))
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			http.Error(w, "no", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewBotClient("t").WithBaseURL(srv.URL)
	if err := c.SendDM(context.Background(), "777", "hi there"); err != nil {
		t.Fatalf("SendDM: %v", err)
	}
	if !strings.Contains(gotOpenBody, `"recipient_id":"777"`) {
		t.Errorf("open-dm body = %q", gotOpenBody)
	}
	if !strings.Contains(gotPostBody, `"content":"hi there"`) {
		t.Errorf("post body = %q", gotPostBody)
	}
	if postPath != "/channels/chan-42/messages" {
		t.Errorf("post path = %q", postPath)
	}
}

func TestSendDM_RateLimited_SurfacesRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "3.5")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"slow down"}`))
	}))
	defer srv.Close()

	c := NewBotClient("t").WithBaseURL(srv.URL)
	err := c.SendDM(context.Background(), "1", "hi")
	if err == nil {
		t.Fatal("expected rate-limit error")
	}
	if !strings.Contains(err.Error(), "rate limited") || !strings.Contains(err.Error(), "retry_after=3.5") {
		t.Errorf("error = %v", err)
	}
}

func TestSendDM_5xx_SurfacesUpstream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"message":"bad gateway"}`))
	}))
	defer srv.Close()

	c := NewBotClient("t").WithBaseURL(srv.URL)
	err := c.SendDM(context.Background(), "1", "hi")
	if err == nil {
		t.Fatal("expected 5xx error")
	}
	if !strings.Contains(err.Error(), "upstream 502") {
		t.Errorf("error = %v", err)
	}
}

func TestSendDM_4xx_SurfacesStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"cannot send messages to this user"}`))
	}))
	defer srv.Close()

	c := NewBotClient("t").WithBaseURL(srv.URL)
	err := c.SendDM(context.Background(), "1", "hi")
	if err == nil {
		t.Fatal("expected 403 error")
	}
	if !strings.Contains(err.Error(), "unexpected status 403") {
		t.Errorf("error = %v", err)
	}
}

func TestSendDM_NilClient_Error(t *testing.T) {
	var c *BotClient
	if err := c.SendDM(context.Background(), "1", "hi"); err == nil {
		t.Fatal("expected error for nil client")
	}
}

func TestSendDM_EmptyRecipient_Error(t *testing.T) {
	c := NewBotClient("t")
	if err := c.SendDM(context.Background(), "", "hi"); err == nil {
		t.Fatal("expected error for empty recipient")
	}
}
