package common

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

// allowedOrigins is the test fixture used across the table. Mirrors a
// realistic deployment: GitHub Pages player + a localhost dev origin.
var allowedOrigins = []string{
	"https://anggorodewanto.github.io",
	"http://localhost:5173",
}

func unwrappedHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = io.WriteString(w, "downstream")
	})
}

func TestCORSMiddleware_PassthroughWhenNoAllowedOrigins(t *testing.T) {
	got := CORSMiddleware(nil, unwrappedHandler())
	req := httptest.NewRequest(http.MethodOptions, "/v1/foo", nil)
	req.Header.Set("Origin", "https://anggorodewanto.github.io")
	w := httptest.NewRecorder()
	got.ServeHTTP(w, req)
	if w.Code != http.StatusTeapot {
		t.Fatalf("empty allowlist must be a no-op (downstream wrote 418); got %d", w.Code)
	}
	if h := w.Header().Get("Access-Control-Allow-Origin"); h != "" {
		t.Fatalf("expected no CORS headers in passthrough mode; got %q", h)
	}
}

func TestCORSMiddleware_PreflightAnsweredForAllowedOrigin(t *testing.T) {
	h := CORSMiddleware(allowedOrigins, unwrappedHandler())
	req := httptest.NewRequest(http.MethodOptions, "/v1/foo", nil)
	req.Header.Set("Origin", "https://anggorodewanto.github.io")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "authorization,content-type")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight must short-circuit with 204; got %d", w.Code)
	}
	if h := w.Header().Get("Access-Control-Allow-Origin"); h != "https://anggorodewanto.github.io" {
		t.Fatalf("expected reflected Allow-Origin; got %q", h)
	}
	if h := w.Header().Get("Access-Control-Allow-Credentials"); h != "true" {
		t.Fatalf("expected Allow-Credentials true; got %q", h)
	}
	if h := w.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(h, "POST") {
		t.Fatalf("expected POST in Allow-Methods; got %q", h)
	}
	if h := w.Header().Get("Access-Control-Allow-Headers"); h != "authorization,content-type" {
		t.Fatalf("expected request headers reflected verbatim; got %q", h)
	}
	vary := w.Header().Values("Vary")
	if !slices.Contains(vary, "Origin") {
		t.Fatalf("expected Vary: Origin; got %v", vary)
	}
}

func TestCORSMiddleware_PreflightDefaultsAllowedHeadersWhenRequestOmitsThem(t *testing.T) {
	h := CORSMiddleware(allowedOrigins, unwrappedHandler())
	req := httptest.NewRequest(http.MethodOptions, "/v1/foo", nil)
	req.Header.Set("Origin", "https://anggorodewanto.github.io")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	got := w.Header().Get("Access-Control-Allow-Headers")
	if got != "Authorization, Content-Type, Cookie" {
		t.Fatalf("default Allow-Headers must include auth + content-type + cookie; got %q", got)
	}
}

func TestCORSMiddleware_DisallowedOriginFallsThroughToDownstream(t *testing.T) {
	h := CORSMiddleware(allowedOrigins, unwrappedHandler())
	req := httptest.NewRequest(http.MethodGet, "/v1/foo", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusTeapot {
		t.Fatalf("disallowed origin must reach downstream (418); got %d", w.Code)
	}
	if h := w.Header().Get("Access-Control-Allow-Origin"); h != "" {
		t.Fatalf("disallowed origin must not get an Allow-Origin header; got %q", h)
	}
}

func TestCORSMiddleware_NonOptionsRequestGetsHeadersAndPassesThrough(t *testing.T) {
	h := CORSMiddleware(allowedOrigins, unwrappedHandler())
	req := httptest.NewRequest(http.MethodGet, "/v1/foo", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusTeapot {
		t.Fatalf("non-preflight request must reach downstream; got %d", w.Code)
	}
	if h := w.Header().Get("Access-Control-Allow-Origin"); h != "http://localhost:5173" {
		t.Fatalf("expected reflected Allow-Origin; got %q", h)
	}
	if h := w.Header().Get("Access-Control-Allow-Credentials"); h != "true" {
		t.Fatalf("expected credentials header; got %q", h)
	}
}

func TestCORSMiddleware_RequestWithoutOriginIsUntouched(t *testing.T) {
	h := CORSMiddleware(allowedOrigins, unwrappedHandler())
	req := httptest.NewRequest(http.MethodGet, "/v1/foo", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusTeapot {
		t.Fatalf("origin-less request must pass straight through; got %d", w.Code)
	}
	if h := w.Header().Get("Access-Control-Allow-Origin"); h != "" {
		t.Fatalf("origin-less request must not gain CORS headers; got %q", h)
	}
}

func TestCORSMiddleware_WildcardReflectsArbitraryOriginCredentialed(t *testing.T) {
	// '*' is invalid in credentialed responses, so the middleware
	// reflects the request origin instead — preserves Allow-Credentials
	// semantics without forcing operators to enumerate every dev origin.
	h := CORSMiddleware([]string{"*"}, unwrappedHandler())
	req := httptest.NewRequest(http.MethodOptions, "/v1/foo", nil)
	req.Header.Set("Origin", "https://anywhere.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://anywhere.example" {
		t.Fatalf("wildcard must reflect the request origin; got %q", got)
	}
}
