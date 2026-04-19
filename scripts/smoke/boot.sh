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

# 2. App --------------------------------------------------------------------
log "booting app (auth disabled) — logs to /tmp/playtesthub-smoke.log"
BASE_PATH="$BASE_PATH" \
DATABASE_URL="postgres://playtesthub:playtesthub@localhost:$PG_PORT/playtesthub?sslmode=disable" \
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

log "unauth RPC reaches handler (expect Unimplemented until M1 phase 6)"
code=$(curl -s -o /dev/null -w '%{http_code}' \
    "http://localhost:$APP_PORT_HTTP$BASE_PATH/v1/public/playtests/smoke-test")
[[ "$code" == "501" ]] \
    || fail "expected 501 Unimplemented from GetPublicPlaytest, got $code"

log "PASS"
