#!/usr/bin/env bash
# scripts/smoke/cloud.sh — does the deployed backend respond?
#
# Unlike boot.sh (local binary + throwaway Postgres), this targets the
# Extend deploy at <AGS_BASE_URL>/ext-<namespace>-<app>. It's the "did
# image-upload + deploy-app + start-app actually yield a live service?"
# gate. Bounded: a 404 from the AGS gateway means the app isn't started;
# a 401 on an admin route means the handler is reachable and auth is on.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

log() { printf '[smoke:cloud] %s\n' "$*" >&2; }
fail() { log "FAIL: $*"; exit 1; }

require() {
    command -v "$1" >/dev/null 2>&1 || fail "$1 not on PATH"
}
require curl
require jq

: "${AGS_BASE_URL:?AGS_BASE_URL must be set (e.g. source .env)}"
: "${AGS_NAMESPACE:?AGS_NAMESPACE must be set}"

APP_NAME="${APP_NAME:-playtesthub}"
EXT_PATH="/ext-${AGS_NAMESPACE}-${APP_NAME}"
BASE="${AGS_BASE_URL%/}${EXT_PATH}"

log "targeting $BASE"

log "OpenAPI spec reachable and parses"
spec=$(curl -sf "${BASE}/apidocs/api.json") \
    || fail "apidocs/api.json unreachable — is the app started?"
title=$(jq -r '.info.title' <<<"$spec")
[[ "$title" == "playtesthub API" ]] \
    || fail "unexpected OpenAPI title: $title"

log "unauth GetPublicPlaytest reaches handler (expect 404 for missing slug)"
code=$(curl -s -o /dev/null -w '%{http_code}' \
    "${BASE}/v1/public/playtests/smoke-nonexistent")
[[ "$code" == "404" ]] \
    || fail "expected 404 from GetPublicPlaytest, got $code"

log "admin RPC requires auth (expect 401)"
code=$(curl -s -o /dev/null -w '%{http_code}' \
    "${BASE}/v1/admin/namespaces/${AGS_NAMESPACE}/playtests")
[[ "$code" == "401" ]] \
    || fail "expected 401 from ListPlaytests, got $code"

# Phase 7 signup path: grpc-gateway + auth interceptor reach the Signup
# handler. An unauth call gets 401 from the interceptor before any DB
# work. Validates the deployed route + body binding didn't regress.
log "player Signup requires auth (expect 401)"
code=$(curl -s -o /dev/null -w '%{http_code}' \
    -H 'Content-Type: application/json' \
    -d '{"platforms":["PLATFORM_STEAM"]}' \
    "${BASE}/v1/player/playtests/cloud-smoke-nonexistent/signup")
[[ "$code" == "401" ]] \
    || fail "expected 401 from Signup, got $code"

log "player GetApplicantStatus requires auth (expect 401)"
code=$(curl -s -o /dev/null -w '%{http_code}' \
    "${BASE}/v1/player/playtests/cloud-smoke-nonexistent/applicant")
[[ "$code" == "401" ]] \
    || fail "expected 401 from GetApplicantStatus, got $code"

# Optional: exercise the cookie-forwarded Admin Portal auth path. Set
# ADMIN_PORTAL_COOKIE to the full Cookie header value copied from a
# logged-in Admin Portal browser session. This is the path that
# boot.sh cannot reach — it only exists on the deployed surface where
# grpc-gateway's incoming-header matcher forwards `Cookie:` as gRPC
# metadata (pkg/common/gateway.go) and the auth interceptor pulls the
# `access_token` cookie out of it.
if [[ -n "${ADMIN_PORTAL_COOKIE:-}" ]]; then
    log "cookie-authed ListPlaytests (expect 200)"
    code=$(curl -s -o /dev/null -w '%{http_code}' \
        -H "Cookie: ${ADMIN_PORTAL_COOKIE}" \
        "${BASE}/v1/admin/namespaces/${AGS_NAMESPACE}/playtests")
    [[ "$code" == "200" ]] \
        || fail "expected 200 from cookie-authed ListPlaytests, got $code"
else
    log "skipping cookie-authed check (set ADMIN_PORTAL_COOKIE to enable)"
fi

log "PASS"
