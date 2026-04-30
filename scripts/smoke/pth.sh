#!/usr/bin/env bash
# scripts/smoke/pth.sh — exercises the `pth` CLI against a freshly
# booted local backend. The build step + unit tests cover the command
# wiring; this script proves the binary actually talks to a real
# gRPC server through the wire contract documented in docs/cli.md.
#
# Mirrors boot.sh's setup (throwaway Postgres + auth-disabled backend)
# so the harness has no external prerequisites. Phase 10 spec
# (docs/STATUS.md M1 phase 10.1): asserts `pth version`, `pth doctor`
# (OK on NotFound sentinel) and `pth playtest get-public --slug
# nonexistent` (NotFound + exit 1).
#
# Once the deployed-backend gRPC endpoint is reachable from CI, this
# script can be retargeted with PTH_TARGET_ADDR=<host:port> instead of
# spinning up its own Postgres + app — same assertions, same exit shape.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

CONTAINER_NAME="${CONTAINER_NAME:-playtesthub-smoke-pth-pg}"
PG_PORT="${PG_PORT:-54398}"
# main.go binds gRPC to a hardcoded :6565 today (no env override). Phase
# 10.1 keeps the smoke aligned to that constant; if a future change
# parameterises the port we revisit. Two smoke runs in parallel would
# collide on this port — we expect them to run sequentially.
APP_PORT_GRPC="${APP_PORT_GRPC:-6565}"
APP_PORT_HTTP="${APP_PORT_HTTP:-8000}"
BASE_PATH="${BASE_PATH:-/playtesthub}"
TARGET_ADDR="${PTH_TARGET_ADDR:-localhost:$APP_PORT_GRPC}"

log() { printf '[smoke:pth] %s\n' "$*" >&2; }
fail() { log "FAIL: $*"; exit 1; }

require() {
    command -v "$1" >/dev/null 2>&1 || fail "$1 not on PATH"
}

require docker
require go
require jq

PTH_BIN="$(mktemp -t pth.XXXXXX)"
APP_PID=""

cleanup() {
    local exit_code=$?
    if [[ -n "$APP_PID" ]] && kill -0 "$APP_PID" 2>/dev/null; then
        log "stopping app (pid=$APP_PID)"
        kill -TERM "-$APP_PID" 2>/dev/null || kill "$APP_PID" 2>/dev/null || true
        wait "$APP_PID" 2>/dev/null || true
    fi
    docker rm -f "$CONTAINER_NAME" >/dev/null 2>&1 || true
    rm -f "$PTH_BIN"
    exit "$exit_code"
}
trap cleanup EXIT INT TERM

log "building pth binary"
go build -o "$PTH_BIN" ./cmd/pth

# --- pth version --------------------------------------------------------
# Pure local — no backend needed. Surfaces a JSON document with the
# required fields per docs/cli.md §5.
log "pth version emits required fields"
version_out=$("$PTH_BIN" version)
for key in buildSHA goVersion protoSchema protoFileCount; do
    jq -e --arg k "$key" '.[$k]' >/dev/null <<<"$version_out" \
        || { echo "$version_out" >&2; fail "version output missing key: $key"; }
done

# --- bring up an ephemeral Postgres + backend ---------------------------
# Mirrors boot.sh. We need both because pth talks to :6565 directly
# (the native gRPC port) — the deployed Extend gateway exposes only
# HTTPS REST.
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
for _ in {1..10}; do
    if docker exec "$CONTAINER_NAME" psql -U playtesthub -d playtesthub -c 'SELECT 1' >/dev/null 2>&1; then
        break
    fi
    sleep 1
done

log "booting app (auth disabled) — logs to /tmp/playtesthub-pth-smoke.log"
BASE_PATH="$BASE_PATH" \
DATABASE_URL="postgres://playtesthub:playtesthub@localhost:$PG_PORT/playtesthub?sslmode=disable" \
DISCORD_BOT_TOKEN="smoke-discord-token" \
AGS_IAM_CLIENT_ID="smoke-client-id" \
AGS_IAM_CLIENT_SECRET="smoke-client-secret" \
AGS_BASE_URL="https://ags.smoke.invalid" \
AGS_NAMESPACE="smoke" \
PLUGIN_GRPC_SERVER_AUTH_ENABLED=false \
    setsid go run . >/tmp/playtesthub-pth-smoke.log 2>&1 &
APP_PID=$!

log "waiting for gRPC on :$APP_PORT_GRPC"
for _ in {1..30}; do
    if "$PTH_BIN" --addr "$TARGET_ADDR" --insecure --timeout 1s doctor >/dev/null 2>&1; then
        break
    fi
    sleep 1
done

# --- pth doctor --------------------------------------------------------
log "pth doctor reports OK against the live handler"
set +e
doctor_out=$("$PTH_BIN" --addr "$TARGET_ADDR" --insecure doctor)
doctor_exit=$?
set -e
if [[ $doctor_exit -ne 0 ]]; then
    tail -20 /tmp/playtesthub-pth-smoke.log >&2
    fail "doctor exit=$doctor_exit, want 0 (output: $doctor_out)"
fi
status_field=$(jq -r '.status' <<<"$doctor_out")
[[ "$status_field" == "OK" ]] \
    || fail "doctor.status=$status_field, want OK (output: $doctor_out)"

# --- pth playtest get-public -------------------------------------------
log "pth playtest get-public returns NotFound (exit 1) for missing slug"
set +e
out=$("$PTH_BIN" --addr "$TARGET_ADDR" --insecure playtest get-public --slug "smoke-nonexistent" 2>/tmp/pth-stderr)
get_exit=$?
set -e
[[ $get_exit -eq 1 ]] \
    || { cat /tmp/pth-stderr >&2; fail "playtest get-public exit=$get_exit, want 1"; }
grep -q 'gRPC NotFound' /tmp/pth-stderr \
    || { cat /tmp/pth-stderr >&2; fail "stderr missing 'gRPC NotFound' line"; }

# --- pth playtest get-public --dry-run ---------------------------------
log "pth playtest get-public --dry-run prints the request without dialling"
dry_out=$("$PTH_BIN" --addr "doesnotresolve.invalid:9" playtest get-public --slug demo --dry-run)
[[ "$(jq -r '.slug' <<<"$dry_out")" == "demo" ]] \
    || fail "dry-run output missing slug: $dry_out"

log "PASS"
