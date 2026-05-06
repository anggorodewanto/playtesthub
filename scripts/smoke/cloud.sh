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

# M2 RPC reachability gate (STATUS.md M2 phase 15). Each probe is an
# unauth call that must reach the auth interceptor — anything other
# than 401 means the route never registered (404) or the interceptor
# regressed (200/500). Body payloads are deliberately minimal because
# the auth check fires before request-body binding.
NS="${AGS_NAMESPACE}"
PT="cloud-smoke-nonexistent"
APP_ID="00000000-0000-0000-0000-000000000000"

declare -a m2_probes=(
    "AcceptNDA            POST ${BASE}/v1/player/playtests/${PT}:acceptNda                                  {}"
    "GetGrantedCode       GET  ${BASE}/v1/player/playtests/${PT}/grantedCode                                -"
    "ListApplicants       GET  ${BASE}/v1/admin/namespaces/${NS}/playtests/${PT}/applicants                 -"
    "ApproveApplicant     POST ${BASE}/v1/admin/namespaces/${NS}/applicants/${APP_ID}:approve               {}"
    "RejectApplicant      POST ${BASE}/v1/admin/namespaces/${NS}/applicants/${APP_ID}:reject                {}"
    "RetryDM              POST ${BASE}/v1/admin/namespaces/${NS}/applicants/${APP_ID}:retryDm               {}"
    "UploadCodes          POST ${BASE}/v1/admin/namespaces/${NS}/playtests/${PT}/codes:upload               {}"
    "TopUpCodes           POST ${BASE}/v1/admin/namespaces/${NS}/playtests/${PT}/codes:topUp                {}"
    "SyncFromAGS          POST ${BASE}/v1/admin/namespaces/${NS}/playtests/${PT}/codes:syncFromAgs          {}"
    "GetCodePool          GET  ${BASE}/v1/admin/namespaces/${NS}/playtests/${PT}/codes                      -"
)

for probe in "${m2_probes[@]}"; do
    read -r name method url body <<<"$probe"
    log "M2 ${name} requires auth (expect 401)"
    if [[ "$body" == "-" ]]; then
        code=$(curl -s -o /dev/null -w '%{http_code}' -X "$method" "$url")
    else
        code=$(curl -s -o /dev/null -w '%{http_code}' -X "$method" \
            -H 'Content-Type: application/json' -d "$body" "$url")
    fi
    [[ "$code" == "401" ]] \
        || fail "expected 401 from ${name}, got ${code} (${method} ${url})"
done

# M3 phase 3 RPC reachability gate (STATUS.md M3 phase 3): each survey
# CRUD route must reach the auth interceptor — anything other than 401
# means the route never registered (404) or auth regressed.
declare -a m3_probes=(
    "CreateSurvey         POST  ${BASE}/v1/admin/namespaces/${NS}/playtests/${PT}/survey                    {}"
    "EditSurvey           PATCH ${BASE}/v1/admin/namespaces/${NS}/playtests/${PT}/survey                    {}"
    "GetSurvey            GET   ${BASE}/v1/player/playtests/${PT}/survey                                    -"
    "SubmitSurveyResponse POST  ${BASE}/v1/player/playtests/${PT}/survey:submit                             {}"
    "ListSurveyResponses  GET   ${BASE}/v1/admin/namespaces/${NS}/playtests/${PT}/survey/responses          -"
    "ListAuditLog         GET   ${BASE}/v1/admin/namespaces/${NS}/playtests/${PT}/auditLog                 -"
)

for probe in "${m3_probes[@]}"; do
    read -r name method url body <<<"$probe"
    log "M3 ${name} requires auth (expect 401)"
    if [[ "$body" == "-" ]]; then
        code=$(curl -s -o /dev/null -w '%{http_code}' -X "$method" "$url")
    else
        code=$(curl -s -o /dev/null -w '%{http_code}' -X "$method" \
            -H 'Content-Type: application/json' -d "$body" "$url")
    fi
    [[ "$code" == "401" ]] \
        || fail "expected 401 from ${name}, got ${code} (${method} ${url})"
done

# Phase 9.3 / verified end-to-end in 9.4: ExchangeDiscordCode is unauth
# and posts a Discord OAuth code to AGS IAM's platform-token grant.
# Bogus probe — sends an obviously-fake code, expects a 400 because the
# handler detects AGS's wrap of Discord's 400 invalid_grant inside an
# AGS 500 server_error and surfaces it as InvalidArgument. Validates:
# (a) RPC routed, (b) backend has working AGS Basic auth, (c) AGS error
# mapped to gRPC InvalidArgument → HTTP 400. The live success path
# requires a real Discord OAuth flow (manual smoke per STATUS.md M1
# phase 9.4 / docs/runbooks/discord-login.md).
log "ExchangeDiscordCode rejects bogus code (expect 400 with discord invalid_grant wrap)"
http_response=$(curl -s -w '\n%{http_code}' -X POST \
    -H 'Content-Type: application/json' \
    -d "{\"code\":\"smoke-bogus-${RANDOM}\",\"redirect_uri\":\"http://localhost:5173/callback\"}" \
    "${BASE}/v1/player/discord/exchange")
exchange_code=$(tail -n1 <<<"$http_response")
exchange_body=$(sed '$d' <<<"$http_response")
[[ "$exchange_code" == "400" ]] \
    || fail "expected 400 from ExchangeDiscordCode, got $exchange_code (body: $exchange_body)"
# grpc-gateway maps codes.InvalidArgument → HTTP 400. Body includes the
# AGS error_description verbatim (per errors.md row), which carries
# Discord's invalid_grant marker through the AGS server_error wrap.
grep -q -i 'invalid_grant' <<<"$exchange_body" \
    || fail "ExchangeDiscordCode 400 body missing invalid_grant marker: $exchange_body"

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
