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
    # `go run .` forks a child — the built binary — which will survive a
    # plain `kill $APP_PID`. Kill the whole process group instead so the
    # app binary doesn't linger bound to :8000 and corrupt the next run.
    if [[ -n "$APP_PID" ]] && kill -0 "$APP_PID" 2>/dev/null; then
        log "stopping app (pid=$APP_PID)"
        kill -TERM "-$APP_PID" 2>/dev/null || kill "$APP_PID" 2>/dev/null || true
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
LEADER_HEARTBEAT_SECONDS=1 \
LEADER_LEASE_TTL_SECONDS=5 \
RECLAIM_INTERVAL_SECONDS=1 \
RESERVATION_TTL_SECONDS=5 \
    setsid go run . >/tmp/playtesthub-smoke.log 2>&1 &
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

log "all 11 M1 + 10 M2 methods visible"
# M2 phase 1 declares the full M2 RPC surface on the service so codegen
# + admin/CLI work can land before handlers do; the embedded
# UnimplementedPlaytesthubServiceServer makes calls return gRPC
# Unimplemented at runtime until each milestone phase ships.
EXPECTED_METHODS=(
    # M1 (phase 1)
    GetPublicPlaytest GetPlaytestForPlayer AdminGetPlaytest ListPlaytests
    CreatePlaytest EditPlaytest SoftDeletePlaytest TransitionPlaytestStatus
    Signup GetApplicantStatus ExchangeDiscordCode
    # M2 (phase 1)
    AcceptNDA GetGrantedCode ListApplicants ApproveApplicant
    RejectApplicant RetryDM UploadCodes TopUpCodes SyncFromAGS GetCodePool
)
methods_output=$(grpcurl -plaintext "localhost:$APP_PORT_GRPC" list playtesthub.v1.PlaytesthubService)
for m in "${EXPECTED_METHODS[@]}"; do
    grep -q "\.${m}$" <<<"$methods_output" \
        || fail "method missing from service descriptor: $m"
done

log "remaining M2 RPCs still return Unimplemented"
# Phases 4–6 implement AcceptNDA, UploadCodes, GetCodePool, and the
# four approve-flow RPCs (ApproveApplicant, RejectApplicant,
# ListApplicants, GetGrantedCode). The remaining 4 M2 RPCs (RetryDM,
# TopUpCodes, SyncFromAGS) still ride the embedded
# UnimplementedPlaytesthubServiceServer. Probe one over the native
# gRPC port (no auth interceptor when AUTH_ENABLED=false) to confirm
# the runtime gating still distinguishes shipped from pending
# handlers. RetryDM is the canary — it lands in M2 phase 7.
unimpl_status=$(grpcurl -plaintext \
    -d '{"namespace":"smoke","applicant_id":"00000000-0000-0000-0000-000000000000"}' \
    "localhost:$APP_PORT_GRPC" \
    playtesthub.v1.PlaytesthubService/RetryDM 2>&1 || true)
grep -q "Unimplemented" <<<"$unimpl_status" \
    || { echo "$unimpl_status" >&2; fail "expected Unimplemented from RetryDM (still pending in M2)"; }

log "AcceptNDA reaches the handler (no longer Unimplemented)"
# Phase 4: AcceptNDA must surface its own validation, not the generic
# Unimplemented. With auth disabled the auth interceptor does not
# attach an actor, so the handler short-circuits on requireActor with
# Unauthenticated — that is the proof the handler ran.
accept_status=$(grpcurl -plaintext \
    -d '{"playtest_id":"00000000-0000-0000-0000-000000000000"}' \
    "localhost:$APP_PORT_GRPC" \
    playtesthub.v1.PlaytesthubService/AcceptNDA 2>&1 || true)
if grep -q "Unimplemented" <<<"$accept_status"; then
    echo "$accept_status" >&2
    fail "AcceptNDA still Unimplemented — phase 4 wiring did not land"
fi
grep -q "Unauthenticated" <<<"$accept_status" \
    || { echo "$accept_status" >&2; fail "expected Unauthenticated from AcceptNDA (auth disabled, handler reached)"; }

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

log "admin UploadCodes reaches the handler (no longer Unimplemented)"
# Phase 5: UploadCodes must run its handler. With auth disabled the
# auth interceptor does not attach an actor, so the handler short-
# circuits on requireActor with Unauthenticated — that is the proof
# the handler ran (vs. embedded Unimplemented).
upload_status=$(grpcurl -plaintext \
    -d '{"namespace":"smoke","playtest_id":"00000000-0000-0000-0000-000000000000","csv_content":""}' \
    "localhost:$APP_PORT_GRPC" \
    playtesthub.v1.PlaytesthubService/UploadCodes 2>&1 || true)
if grep -q "Unimplemented" <<<"$upload_status"; then
    echo "$upload_status" >&2
    fail "UploadCodes still Unimplemented — phase 5 wiring did not land"
fi
grep -q "Unauthenticated" <<<"$upload_status" \
    || { echo "$upload_status" >&2; fail "expected Unauthenticated from UploadCodes (auth disabled, handler reached)"; }

log "admin GetCodePool reaches the handler (no longer Unimplemented)"
pool_status=$(grpcurl -plaintext \
    -d '{"namespace":"smoke","playtest_id":"00000000-0000-0000-0000-000000000000"}' \
    "localhost:$APP_PORT_GRPC" \
    playtesthub.v1.PlaytesthubService/GetCodePool 2>&1 || true)
if grep -q "Unimplemented" <<<"$pool_status"; then
    echo "$pool_status" >&2
    fail "GetCodePool still Unimplemented — phase 5 wiring did not land"
fi
grep -q "Unauthenticated" <<<"$pool_status" \
    || { echo "$pool_status" >&2; fail "expected Unauthenticated from GetCodePool (auth disabled, handler reached)"; }

log "admin UploadCodes reaches the handler over the gateway (expect 401 Unauthenticated)"
# Phase 5 wires the gateway path POST /v1/admin/.../codes:upload.
# The auth interceptor → handler short-circuit gives 401, distinct
# from a 501 Unimplemented response that would mean phase 5 wiring
# did not land.
upload_code=$(curl -s -o /dev/null -w '%{http_code}' \
    -H 'Content-Type: application/json' \
    -X POST \
    -d '{"csv_content":""}' \
    "http://localhost:$APP_PORT_HTTP$BASE_PATH/v1/admin/namespaces/smoke/playtests/00000000-0000-0000-0000-000000000000/codes:upload")
[[ "$upload_code" == "401" ]] \
    || { tail -30 /tmp/playtesthub-smoke.log >&2; fail "expected 401 Unauthenticated from UploadCodes gateway path, got $upload_code"; }

log "admin GetCodePool reaches the handler over the gateway (expect 401 Unauthenticated)"
pool_code=$(curl -s -o /dev/null -w '%{http_code}' \
    "http://localhost:$APP_PORT_HTTP$BASE_PATH/v1/admin/namespaces/smoke/playtests/00000000-0000-0000-0000-000000000000/codes")
[[ "$pool_code" == "401" ]] \
    || { tail -30 /tmp/playtesthub-smoke.log >&2; fail "expected 401 Unauthenticated from GetCodePool gateway path, got $pool_code"; }

log "admin ApproveApplicant reaches the handler (no longer Unimplemented)"
approve_status=$(grpcurl -plaintext \
    -d '{"namespace":"smoke","applicant_id":"00000000-0000-0000-0000-000000000000"}' \
    "localhost:$APP_PORT_GRPC" \
    playtesthub.v1.PlaytesthubService/ApproveApplicant 2>&1 || true)
if grep -q "Unimplemented" <<<"$approve_status"; then
    echo "$approve_status" >&2
    fail "ApproveApplicant still Unimplemented — phase 6 wiring did not land"
fi
grep -q "Unauthenticated" <<<"$approve_status" \
    || { echo "$approve_status" >&2; fail "expected Unauthenticated from ApproveApplicant (auth disabled, handler reached)"; }

log "admin RejectApplicant reaches the handler (no longer Unimplemented)"
reject_status=$(grpcurl -plaintext \
    -d '{"namespace":"smoke","applicant_id":"00000000-0000-0000-0000-000000000000"}' \
    "localhost:$APP_PORT_GRPC" \
    playtesthub.v1.PlaytesthubService/RejectApplicant 2>&1 || true)
if grep -q "Unimplemented" <<<"$reject_status"; then
    echo "$reject_status" >&2
    fail "RejectApplicant still Unimplemented — phase 6 wiring did not land"
fi
grep -q "Unauthenticated" <<<"$reject_status" \
    || { echo "$reject_status" >&2; fail "expected Unauthenticated from RejectApplicant (auth disabled, handler reached)"; }

log "admin ListApplicants reaches the handler (no longer Unimplemented)"
list_status=$(grpcurl -plaintext \
    -d '{"namespace":"smoke","playtest_id":"00000000-0000-0000-0000-000000000000"}' \
    "localhost:$APP_PORT_GRPC" \
    playtesthub.v1.PlaytesthubService/ListApplicants 2>&1 || true)
if grep -q "Unimplemented" <<<"$list_status"; then
    echo "$list_status" >&2
    fail "ListApplicants still Unimplemented — phase 6 wiring did not land"
fi
grep -q "Unauthenticated" <<<"$list_status" \
    || { echo "$list_status" >&2; fail "expected Unauthenticated from ListApplicants (auth disabled, handler reached)"; }

log "player GetGrantedCode reaches the handler (no longer Unimplemented)"
granted_status=$(grpcurl -plaintext \
    -d '{"playtest_id":"00000000-0000-0000-0000-000000000000"}' \
    "localhost:$APP_PORT_GRPC" \
    playtesthub.v1.PlaytesthubService/GetGrantedCode 2>&1 || true)
if grep -q "Unimplemented" <<<"$granted_status"; then
    echo "$granted_status" >&2
    fail "GetGrantedCode still Unimplemented — phase 6 wiring did not land"
fi
grep -q "Unauthenticated" <<<"$granted_status" \
    || { echo "$granted_status" >&2; fail "expected Unauthenticated from GetGrantedCode (auth disabled, handler reached)"; }

log "admin ApproveApplicant reaches the handler over the gateway (expect 401 Unauthenticated)"
approve_code=$(curl -s -o /dev/null -w '%{http_code}' \
    -H 'Content-Type: application/json' \
    -X POST -d '{}' \
    "http://localhost:$APP_PORT_HTTP$BASE_PATH/v1/admin/namespaces/smoke/applicants/00000000-0000-0000-0000-000000000000:approve")
[[ "$approve_code" == "401" ]] \
    || { tail -30 /tmp/playtesthub-smoke.log >&2; fail "expected 401 Unauthenticated from ApproveApplicant gateway path, got $approve_code"; }

log "admin RejectApplicant reaches the handler over the gateway (expect 401 Unauthenticated)"
reject_code=$(curl -s -o /dev/null -w '%{http_code}' \
    -H 'Content-Type: application/json' \
    -X POST -d '{}' \
    "http://localhost:$APP_PORT_HTTP$BASE_PATH/v1/admin/namespaces/smoke/applicants/00000000-0000-0000-0000-000000000000:reject")
[[ "$reject_code" == "401" ]] \
    || { tail -30 /tmp/playtesthub-smoke.log >&2; fail "expected 401 Unauthenticated from RejectApplicant gateway path, got $reject_code"; }

log "admin ListApplicants reaches the handler over the gateway (expect 401 Unauthenticated)"
list_code=$(curl -s -o /dev/null -w '%{http_code}' \
    "http://localhost:$APP_PORT_HTTP$BASE_PATH/v1/admin/namespaces/smoke/playtests/00000000-0000-0000-0000-000000000000/applicants")
[[ "$list_code" == "401" ]] \
    || { tail -30 /tmp/playtesthub-smoke.log >&2; fail "expected 401 Unauthenticated from ListApplicants gateway path, got $list_code"; }

log "player GetGrantedCode reaches the handler over the gateway (expect 401 Unauthenticated)"
granted_code=$(curl -s -o /dev/null -w '%{http_code}' \
    "http://localhost:$APP_PORT_HTTP$BASE_PATH/v1/player/playtests/00000000-0000-0000-0000-000000000000/grantedCode")
[[ "$granted_code" == "401" ]] \
    || { tail -30 /tmp/playtesthub-smoke.log >&2; fail "expected 401 Unauthenticated from GetGrantedCode gateway path, got $granted_code"; }

log "reclaim worker emitted at least one reclaim_tick log line"
# M2 phase 6 starts the reclaim worker as a background goroutine. It
# tries to acquire the leader lease on boot and ticks at the
# heartbeat interval (default 10s). Wait up to 12s for the first tick
# to land in the log file. A missing tick means the worker did not
# start, did not acquire the lease, or its log line shape regressed.
reclaim_ok=""
for _ in {1..24}; do
    if grep -q '"event":"reclaim_tick"' /tmp/playtesthub-smoke.log; then
        reclaim_ok=1
        break
    fi
    sleep 0.5
done
[[ -n "$reclaim_ok" ]] \
    || { tail -50 /tmp/playtesthub-smoke.log >&2; fail "reclaim worker never emitted a reclaim_tick log line"; }

log "player AcceptNDA reaches the handler over the gateway (expect 401 Unauthenticated)"
# M2 phase 4 wires the gateway path POST /v1/player/playtests/{id}:acceptNda.
# auth interceptor → handler short-circuit gives 401, distinct from a
# 501 Unimplemented response that would mean phase 4 wiring did not land.
acceptnda_code=$(curl -s -o /dev/null -w '%{http_code}' \
    -H 'Content-Type: application/json' \
    -X POST \
    -d '{}' \
    "http://localhost:$APP_PORT_HTTP$BASE_PATH/v1/player/playtests/00000000-0000-0000-0000-000000000000:acceptNda")
[[ "$acceptnda_code" == "401" ]] \
    || { tail -30 /tmp/playtesthub-smoke.log >&2; fail "expected 401 Unauthenticated from AcceptNDA gateway path, got $acceptnda_code"; }

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
