#!/usr/bin/env bash
# scripts/smoke/boot.sh — end-to-end "does the service boot?" check.
#
# Brings up a throwaway Postgres, launches the backend with auth disabled,
# asserts gRPC reflection + OpenAPI + an unauth RPC call reach the handler
# layer, then tears everything down.
#
# Runs in ~10s on a warm Go module cache. Intended to be the "I touched
# wiring, did I break boot?" gate from CLAUDE.md's verification checklist.
# Superseded by `pth flow golden-m1` (M1 phase 10) once the CLI ships —
# the CLI harness covers everything this does plus real RPC exercise.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

CONTAINER_NAME="${CONTAINER_NAME:-playtesthub-smoke-pg}"
PG_PORT="${PG_PORT:-54399}"
APP_PORT_GRPC="${APP_PORT_GRPC:-6565}"
APP_PORT_HTTP="${APP_PORT_HTTP:-8000}"
BASE_PATH="${BASE_PATH:-/playtesthub}"

log() { printf '[smoke] %s\n' "$*" >&2; }
fail() { log "FAIL: $*"; exit 1; }

require() {
    command -v "$1" >/dev/null 2>&1 || fail "$1 not on PATH"
}

require docker
require go
require grpcurl
require curl
require jq

APP_PID=""
cleanup() {
    local code=$?
    if [[ -n "$APP_PID" ]] && kill -0 "$APP_PID" 2>/dev/null; then
        log "stopping app (pid=$APP_PID)"
        kill "$APP_PID" 2>/dev/null || true
        wait "$APP_PID" 2>/dev/null || true
    fi
    log "removing postgres container $CONTAINER_NAME"
    docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
    exit "$code"
}
trap cleanup EXIT INT TERM

# 1. Postgres --------------------------------------------------------------
log "starting postgres on :$PG_PORT"
docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
docker run -d --rm --name "$CONTAINER_NAME" \
    -e POSTGRES_USER=playtesthub \
    -e POSTGRES_PASSWORD=playtesthub \
    -e POSTGRES_DB=playtesthub \
    -p "$PG_PORT:5432" \
    postgres:16-alpine >/dev/null

log "waiting for postgres"
for _ in {1..30}; do
    if docker exec "$CONTAINER_NAME" pg_isready -U playtesthub -d playtesthub >/dev/null 2>&1; then
        break
    fi
    sleep 1
done
docker exec "$CONTAINER_NAME" pg_isready -U playtesthub -d playtesthub >/dev/null \
    || fail "postgres did not become ready in 30s"

# pg_isready flips on while postgres is still finishing its init script —
# the very first connection sometimes gets a reset-by-peer. Round-trip a
# real psql query so we don't race the migration runner into that window.
for _ in {1..10}; do
    if docker exec "$CONTAINER_NAME" psql -U playtesthub -d playtesthub -c 'SELECT 1' >/dev/null 2>&1; then
        break
    fi
    sleep 1
done
docker exec "$CONTAINER_NAME" psql -U playtesthub -d playtesthub -c 'SELECT 1' >/dev/null 2>&1 \
    || fail "postgres did not accept a psql query in 10s"

# 2. App --------------------------------------------------------------------
# pkg/config (M1 phase 5) treats every PRD §5.9 required var as a hard
# boot prereq — missing one exits before anything else runs. Even with
# auth disabled, the values are still read at startup, so we seed
# placeholders here. Real values are irrelevant because AGS is not
# reached: PLUGIN_GRPC_SERVER_AUTH_ENABLED=false short-circuits
# Validator.Initialize + oauthService.LoginClient.
log "booting app (auth disabled) — logs to /tmp/playtesthub-smoke.log"
BASE_PATH="$BASE_PATH" \
DATABASE_URL="postgres://playtesthub:playtesthub@localhost:$PG_PORT/playtesthub?sslmode=disable" \
DISCORD_BOT_TOKEN="smoke-discord-token" \
AGS_IAM_CLIENT_ID="smoke-client-id" \
AGS_IAM_CLIENT_SECRET="smoke-client-secret" \
AGS_BASE_URL="https://ags.smoke.invalid" \
AGS_NAMESPACE="smoke" \
PLUGIN_GRPC_SERVER_AUTH_ENABLED=false \
    go run . >/tmp/playtesthub-smoke.log 2>&1 &
APP_PID=$!

log "waiting for gRPC on :$APP_PORT_GRPC"
for _ in {1..30}; do
    if grpcurl -plaintext -max-time 1 "localhost:$APP_PORT_GRPC" list >/dev/null 2>&1; then
        break
    fi
    sleep 1
done
grpcurl -plaintext -max-time 1 "localhost:$APP_PORT_GRPC" list >/dev/null \
    || { tail -20 /tmp/playtesthub-smoke.log >&2; fail "gRPC did not come up in 30s"; }

# 3. Assertions -------------------------------------------------------------
log "gRPC reflection lists playtesthub.v1.PlaytesthubService"
grpcurl -plaintext "localhost:$APP_PORT_GRPC" list \
    | grep -q '^playtesthub\.v1\.PlaytesthubService$' \
    || fail "service not exposed via reflection"

log "all 10 M1 methods visible"
EXPECTED_METHODS=(
    GetPublicPlaytest GetPlaytestForPlayer AdminGetPlaytest ListPlaytests
    CreatePlaytest EditPlaytest SoftDeletePlaytest TransitionPlaytestStatus
    Signup GetApplicantStatus
)
methods_output=$(grpcurl -plaintext "localhost:$APP_PORT_GRPC" list playtesthub.v1.PlaytesthubService)
for m in "${EXPECTED_METHODS[@]}"; do
    grep -q "\.${m}$" <<<"$methods_output" \
        || fail "method missing from service descriptor: $m"
done

log "OpenAPI spec served and parses"
spec=$(curl -sf "http://localhost:$APP_PORT_HTTP$BASE_PATH/apidocs/api.json")
title=$(jq -r '.info.title' <<<"$spec")
[[ "$title" == "playtesthub API" ]] \
    || fail "unexpected OpenAPI title: $title"

log "unauth GetPublicPlaytest reaches handler and returns NotFound for missing slug"
# M1 phase 6 wired GetPublicPlaytest to the repo-backed handler. A slug
# that does not exist now yields a 404 (NotFound) — the grpc-gateway
# status mapping. Prior to phase 6 the same call returned 501.
code=$(curl -s -o /dev/null -w '%{http_code}' \
    "http://localhost:$APP_PORT_HTTP$BASE_PATH/v1/public/playtests/smoke-test")
[[ "$code" == "404" ]] \
    || { tail -30 /tmp/playtesthub-smoke.log >&2; fail "expected 404 NotFound from GetPublicPlaytest, got $code"; }

log "admin RPCs still require auth (expect Unauthenticated with auth disabled)"
# With PLUGIN_GRPC_SERVER_AUTH_ENABLED=false the interceptor does not
# attach an actor — admin handlers short-circuit with codes.Unauthenticated
# (HTTP 401). Confirms the handler wiring reaches requireActor before
# any DB work happens.
code=$(curl -s -o /dev/null -w '%{http_code}' \
    "http://localhost:$APP_PORT_HTTP$BASE_PATH/v1/admin/namespaces/smoke/playtests")
[[ "$code" == "401" ]] \
    || { tail -30 /tmp/playtesthub-smoke.log >&2; fail "expected 401 Unauthenticated from ListPlaytests, got $code"; }

log "player Signup requires auth (expect Unauthenticated with auth disabled)"
# M1 phase 7: Signup is wired to the handler layer. Without a bearer
# token the auth interceptor short-circuits before any DB or Discord
# call. Confirms the POST body path + route reach Signup, not the 501
# Unimplemented stub.
code=$(curl -s -o /dev/null -w '%{http_code}' \
    -H 'Content-Type: application/json' \
    -d '{"platforms":["PLATFORM_STEAM"]}' \
    "http://localhost:$APP_PORT_HTTP$BASE_PATH/v1/player/playtests/smoke-test/signup")
[[ "$code" == "401" ]] \
    || { tail -30 /tmp/playtesthub-smoke.log >&2; fail "expected 401 Unauthenticated from Signup, got $code"; }

log "player GetApplicantStatus requires auth (expect Unauthenticated with auth disabled)"
code=$(curl -s -o /dev/null -w '%{http_code}' \
    "http://localhost:$APP_PORT_HTTP$BASE_PATH/v1/player/playtests/smoke-test/applicant")
[[ "$code" == "401" ]] \
    || { tail -30 /tmp/playtesthub-smoke.log >&2; fail "expected 401 Unauthenticated from GetApplicantStatus, got $code"; }

# PRD §6 forbids NDA text in logs. CreatePlaytest is rejected upstream (no
# auth), but the logging interceptor runs before the auth check — so a
# regression that re-enables PayloadReceived/PayloadSent would dump the
# request body verbatim. We seed a distinctive marker in nda_text and
# assert it does not appear in the log file.
log "NDA text never reaches logs (PRD §6 observability constraint)"
NDA_MARKER="SMOKE_NDA_MARKER_DO_NOT_LOG_$(date +%s%N)"
curl -s -o /dev/null \
    -H 'Content-Type: application/json' \
    -d "{\"slug\":\"smoke-nda\",\"title\":\"t\",\"nda_required\":true,\"nda_text\":\"${NDA_MARKER}\",\"distribution_model\":\"DISTRIBUTION_MODEL_STEAM_KEYS\"}" \
    "http://localhost:$APP_PORT_HTTP$BASE_PATH/v1/admin/namespaces/smoke/playtests" || true
if grep -q "$NDA_MARKER" /tmp/playtesthub-smoke.log; then
    fail "NDA text marker leaked into logs — PRD §6 violation"
fi

log "PASS"
