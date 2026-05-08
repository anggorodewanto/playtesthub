package common

import (
	"net/http"
	"strings"
)

// CORSMiddleware returns an http.Handler that handles CORS preflight
// (OPTIONS) and decorates real responses with `Access-Control-*`
// headers when the request `Origin` is in `allowedOrigins`. Empty
// list → returns `next` unchanged (no CORS handling — vanilla
// grpc-gateway behavior, where the gateway returns 501 on OPTIONS).
//
// Allowed-headers includes `Authorization` (player bearer token),
// `Content-Type` (POST/PATCH JSON bodies), and `Cookie` (admin
// session). Allow-Credentials is set so cookie-authed admin RPCs keep
// working when the admin UI is hosted off-origin.
//
// Wildcard `"*"` is treated as "reflect any origin"; a credentialed
// CORS response with `*` is invalid per the spec, so the middleware
// reflects the request origin in that case.
func CORSMiddleware(allowedOrigins []string, next http.Handler) http.Handler {
	if len(allowedOrigins) == 0 {
		return next
	}
	allow := make(map[string]struct{}, len(allowedOrigins))
	wildcard := false
	for _, o := range allowedOrigins {
		if o == "*" {
			wildcard = true
			continue
		}
		allow[o] = struct{}{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		_, ok := allow[origin]
		if !ok && !wildcard {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Set("Access-Control-Allow-Origin", origin)
		h.Set("Access-Control-Allow-Credentials", "true")
		h.Add("Vary", "Origin")
		if r.Method == http.MethodOptions {
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			reqHeaders := r.Header.Get("Access-Control-Request-Headers")
			if reqHeaders == "" {
				reqHeaders = "Authorization, Content-Type, Cookie"
			}
			h.Set("Access-Control-Allow-Headers", reqHeaders)
			h.Set("Access-Control-Max-Age", "600")
			h.Add("Vary", "Access-Control-Request-Headers")
			h.Add("Vary", "Access-Control-Request-Method")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// JoinAllowedOrigins is a small helper for tests / docs — joins the
// configured origins with a comma so they can be log-rendered without
// pulling fmt into the hot path.
func JoinAllowedOrigins(origins []string) string {
	return strings.Join(origins, ",")
}
