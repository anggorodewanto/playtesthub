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

# --- pth auth login --discord --no-browser --dry-run -----------------
# Phase 10.3 (docs/STATUS.md): asserts CLI URL construction + env-var
# plumbing for the Discord-direct → backend ExchangeDiscordCode flow
# without hitting Discord, AGS, or the backend. Live login is browser-
# bound and can't be exercised headlessly.
log "pth auth login --discord --no-browser --dry-run prints expected JSON"
discord_dry=$(
    PTH_DISCORD_CLIENT_ID="smoke-discord-client" \
    PTH_DISCORD_LOOPBACK_PORT=14565 \
    PTH_BACKEND_REST_URL="https://backend.smoke.invalid" \
    "$PTH_BIN" --namespace smoke --profile smoke-discord-dry \
        auth login --discord --no-browser --dry-run
)
[[ "$(jq -r '.mode' <<<"$discord_dry")" == "loopback" ]] \
    || fail "discord dry-run mode=$(jq -r '.mode' <<<"$discord_dry"); want loopback. out=$discord_dry"
[[ "$(jq -r '.redirectUri' <<<"$discord_dry")" == "http://127.0.0.1:14565/callback" ]] \
    || fail "discord dry-run redirectUri mismatch: $discord_dry"
[[ "$(jq -r '.exchangeUrl' <<<"$discord_dry")" == "https://backend.smoke.invalid/v1/player/discord/exchange" ]] \
    || fail "discord dry-run exchangeUrl mismatch: $discord_dry"
authorize_url=$(jq -r '.authorizeUrl' <<<"$discord_dry")
[[ "$authorize_url" == *"discord.com/oauth2/authorize"* ]] \
    || fail "discord dry-run authorizeUrl missing discord.com/oauth2/authorize: $authorize_url"
[[ "$authorize_url" == *"client_id=smoke-discord-client"* ]] \
    || fail "discord dry-run authorizeUrl missing client_id: $authorize_url"
[[ "$authorize_url" == *"state="* ]] \
    || fail "discord dry-run authorizeUrl missing state: $authorize_url"

# --- pth user (dry-run; unconditional) --------------------------------
# Phase 10.4 (docs/STATUS.md): assert URL + body shape for the AGS IAM
# admin endpoints without dialling. Live round-trip is gated below on
# PTH_E2E_* secrets.
log "pth user create --dry-run prints the test_users path and body"
user_create_dry=$("$PTH_BIN" --namespace smoke --profile smoke-pth user create --count 2 --country ID --dry-run)
[[ "$(jq -r '.method' <<<"$user_create_dry")" == "POST" ]] \
    || fail "user create dry-run method != POST: $user_create_dry"
[[ "$(jq -r '.path' <<<"$user_create_dry")" == "/iam/v4/admin/namespaces/smoke/test_users" ]] \
    || fail "user create dry-run path mismatch: $user_create_dry"
[[ "$(jq -r '.body.count' <<<"$user_create_dry")" == "2" ]] \
    || fail "user create dry-run count mismatch: $user_create_dry"
[[ "$(jq -r '.body.userInfo.country' <<<"$user_create_dry")" == "ID" ]] \
    || fail "user create dry-run country mismatch: $user_create_dry"

log "pth user delete --dry-run prints the v3 information path"
user_delete_dry=$("$PTH_BIN" --namespace smoke --profile smoke-pth user delete --id u-smoke --dry-run)
[[ "$(jq -r '.method' <<<"$user_delete_dry")" == "DELETE" ]] \
    || fail "user delete dry-run method != DELETE: $user_delete_dry"
[[ "$(jq -r '.path' <<<"$user_delete_dry")" == "/iam/v3/admin/namespaces/smoke/users/u-smoke/information" ]] \
    || fail "user delete dry-run path mismatch: $user_delete_dry"

log "pth user login-as --dry-run prints both lookup + token paths"
user_login_dry=$("$PTH_BIN" --namespace smoke --profile smoke-pth user login-as --id u-smoke --dry-run)
[[ "$(jq -r '.lookupPath' <<<"$user_login_dry")" == "/iam/v3/admin/namespaces/smoke/users/u-smoke" ]] \
    || fail "user login-as dry-run lookupPath mismatch: $user_login_dry"
[[ "$(jq -r '.tokenPath' <<<"$user_login_dry")" == "/iam/v3/oauth/token" ]] \
    || fail "user login-as dry-run tokenPath mismatch: $user_login_dry"

log "pth user delete without --yes refuses (destructive guard)"
set +e
"$PTH_BIN" --namespace smoke --profile smoke-pth user delete --id u-smoke 2>/tmp/pth-user-delete-stderr
delete_no_yes_exit=$?
set -e
[[ $delete_no_yes_exit -eq 3 ]] \
    || { cat /tmp/pth-user-delete-stderr >&2; fail "user delete without --yes exit=$delete_no_yes_exit, want 3"; }
grep -q -- '--yes' /tmp/pth-user-delete-stderr \
    || { cat /tmp/pth-user-delete-stderr >&2; fail "user delete error message did not mention --yes"; }

# --- pth playtest + applicant (dry-run; unconditional) ----------------
# Phase 10.5 (docs/STATUS.md): every M1 single-RPC wrapper exposes
# --dry-run so an agent can validate request shape without committing
# the action. The dry-run output is the protojson encoding of the
# request body; the request fields use proto names (snake_case).
log "pth playtest get-player --dry-run"
gpl_dry=$("$PTH_BIN" --addr "$TARGET_ADDR" --insecure playtest get-player --slug demo --dry-run)
[[ "$(jq -r '.slug' <<<"$gpl_dry")" == "demo" ]] \
    || fail "playtest get-player dry-run slug mismatch: $gpl_dry"

log "pth playtest get --dry-run"
adm_dry=$("$PTH_BIN" --namespace smoke playtest get --id p-smoke --dry-run)
[[ "$(jq -r '.namespace' <<<"$adm_dry")" == "smoke" ]] \
    || fail "playtest get dry-run namespace mismatch: $adm_dry"
[[ "$(jq -r '.playtest_id' <<<"$adm_dry")" == "p-smoke" ]] \
    || fail "playtest get dry-run playtest_id mismatch: $adm_dry"

log "pth playtest list --dry-run"
list_dry=$("$PTH_BIN" --namespace smoke playtest list --dry-run)
[[ "$(jq -r '.namespace' <<<"$list_dry")" == "smoke" ]] \
    || fail "playtest list dry-run namespace mismatch: $list_dry"

log "pth playtest create --dry-run renders the full body"
create_dry=$("$PTH_BIN" --namespace smoke playtest create \
    --slug demo-01 --title "Demo" --description "d" \
    --platforms STEAM,XBOX \
    --starts-at 2026-05-01T12:00:00Z --ends-at 2026-06-01T12:00:00Z \
    --nda-required --nda-text "raw nda" \
    --distribution-model STEAM_KEYS \
    --dry-run)
[[ "$(jq -r '.slug' <<<"$create_dry")" == "demo-01" ]] \
    || fail "playtest create dry-run slug mismatch: $create_dry"
[[ "$(jq -r '.distribution_model' <<<"$create_dry")" == "DISTRIBUTION_MODEL_STEAM_KEYS" ]] \
    || fail "playtest create dry-run distribution_model wrong: $create_dry"
[[ "$(jq -r '.platforms | length' <<<"$create_dry")" == "2" ]] \
    || fail "playtest create dry-run platforms length wrong: $create_dry"

log "pth playtest edit --dry-run"
edit_dry=$("$PTH_BIN" --namespace smoke playtest edit --id p-smoke --title "New" --dry-run)
[[ "$(jq -r '.playtest_id' <<<"$edit_dry")" == "p-smoke" ]] \
    || fail "playtest edit dry-run playtest_id mismatch: $edit_dry"
[[ "$(jq -r '.title' <<<"$edit_dry")" == "New" ]] \
    || fail "playtest edit dry-run title mismatch: $edit_dry"
# Edit must reject immutable flags client-side.
log "pth playtest edit rejects --slug client-side"
set +e
"$PTH_BIN" --namespace smoke playtest edit --id p-smoke --slug new-slug --dry-run >/dev/null 2>/tmp/pth-edit-stderr
edit_immut_exit=$?
set -e
[[ $edit_immut_exit -eq 3 ]] \
    || { cat /tmp/pth-edit-stderr >&2; fail "playtest edit --slug exit=$edit_immut_exit, want 3"; }
grep -q "immutable" /tmp/pth-edit-stderr \
    || { cat /tmp/pth-edit-stderr >&2; fail "playtest edit error did not name immutability"; }

log "pth playtest delete --dry-run"
del_dry=$("$PTH_BIN" --namespace smoke playtest delete --id p-smoke --dry-run)
[[ "$(jq -r '.playtest_id' <<<"$del_dry")" == "p-smoke" ]] \
    || fail "playtest delete dry-run playtest_id mismatch: $del_dry"

log "pth playtest transition --dry-run normalizes short status"
trans_dry=$("$PTH_BIN" --namespace smoke playtest transition --id p-smoke --to OPEN --dry-run)
[[ "$(jq -r '.target_status' <<<"$trans_dry")" == "PLAYTEST_STATUS_OPEN" ]] \
    || fail "playtest transition dry-run target_status mismatch: $trans_dry"

log "pth playtest transition rejects unknown status"
set +e
"$PTH_BIN" --namespace smoke playtest transition --id p-smoke --to ARCHIVED --dry-run >/dev/null 2>/tmp/pth-trans-stderr
trans_bad_exit=$?
set -e
[[ $trans_bad_exit -eq 3 ]] \
    || { cat /tmp/pth-trans-stderr >&2; fail "playtest transition ARCHIVED exit=$trans_bad_exit, want 3"; }

log "pth applicant signup --dry-run"
sign_dry=$("$PTH_BIN" applicant signup --slug demo-01 --platforms STEAM --dry-run)
[[ "$(jq -r '.slug' <<<"$sign_dry")" == "demo-01" ]] \
    || fail "applicant signup dry-run slug mismatch: $sign_dry"
[[ "$(jq -r '.platforms[0]' <<<"$sign_dry")" == "PLATFORM_STEAM" ]] \
    || fail "applicant signup dry-run platforms[0] mismatch: $sign_dry"

log "pth applicant signup requires --platforms"
set +e
"$PTH_BIN" applicant signup --slug demo-01 --dry-run >/dev/null 2>/tmp/pth-sign-stderr
sign_bad_exit=$?
set -e
[[ $sign_bad_exit -eq 3 ]] \
    || { cat /tmp/pth-sign-stderr >&2; fail "applicant signup without --platforms exit=$sign_bad_exit, want 3"; }

log "pth applicant status --dry-run"
status_dry=$("$PTH_BIN" applicant status --slug demo-01 --dry-run)
[[ "$(jq -r '.slug' <<<"$status_dry")" == "demo-01" ]] \
    || fail "applicant status dry-run slug mismatch: $status_dry"

# --- pth describe (unconditional) -------------------------------------
# Phase 10.6 (docs/STATUS.md): the CI diff-check on
# cmd/pth/testdata/describe.golden.json owns the byte-exact assertion.
# This probe just proves the binary emits the catalogue with a non-empty
# commands list and the cli.md §5 schema marker — covers wiring only.
log "pth describe emits cli-schema.v1 with a non-empty commands list"
describe_out=$("$PTH_BIN" describe)
[[ "$(jq -r '.schema' <<<"$describe_out")" == "cli-schema.v1" ]] \
    || fail "describe schema != cli-schema.v1: $describe_out"
describe_count=$(jq '.commands | length' <<<"$describe_out")
[[ "$describe_count" -gt 0 ]] \
    || fail "describe commands list empty (length=$describe_count)"

# --- pth flow golden-m1 --dry-run (unconditional) ---------------------
# Phase 10.6 (docs/STATUS.md): four NDJSON steps with status=DRY_RUN
# and a request body. No profiles needed in dry-run mode.
log "pth flow golden-m1 --dry-run emits 4 NDJSON steps without dialling"
flow_dry=$("$PTH_BIN" --namespace smoke flow golden-m1 --slug demo-flow --dry-run)
flow_dry_lines=$(printf '%s\n' "$flow_dry" | wc -l | tr -d ' ')
[[ "$flow_dry_lines" -eq 4 ]] \
    || { printf '%s\n' "$flow_dry" >&2; fail "flow dry-run lines=$flow_dry_lines, want 4"; }
expected_steps=("create-playtest" "transition-open" "signup" "assert-pending")
i=0
while IFS= read -r line; do
    step=$(jq -r '.step' <<<"$line")
    status=$(jq -r '.status' <<<"$line")
    [[ "$step" == "${expected_steps[$i]}" ]] \
        || fail "flow dry-run line $((i+1)) step=$step, want ${expected_steps[$i]}"
    [[ "$status" == "DRY_RUN" ]] \
        || fail "flow dry-run line $((i+1)) status=$status, want DRY_RUN"
    i=$((i+1))
done <<<"$flow_dry"

# --- pth auth login --password (gated on PTH_E2E_* secrets) -----------
# Phase 10.2 spec (docs/STATUS.md): probe ROPC + whoami + token + logout
# round-trip when admin creds + IAM env are present. Skipped when any
# variable is unset so CI without secrets still runs the smoke clean.
#
# Required env to enable: PTH_AGS_BASE_URL, PTH_IAM_CLIENT_ID,
# PTH_E2E_NAMESPACE, PTH_E2E_USERNAME, PTH_E2E_PASSWORD. Optional:
# PTH_IAM_CLIENT_SECRET (for confidential clients).
auth_skip_reason=""
for var in PTH_AGS_BASE_URL PTH_IAM_CLIENT_ID PTH_E2E_NAMESPACE PTH_E2E_USERNAME PTH_E2E_PASSWORD; do
    if [[ -z "${!var:-}" ]]; then
        auth_skip_reason="${var} unset"
        break
    fi
done

if [[ -n "$auth_skip_reason" ]]; then
    log "skipping auth login probe: $auth_skip_reason"
else
    # Use an isolated credentials store so a smoke run never touches the
    # operator's real ~/.config/playtesthub.
    PTH_CREDS_DIR="$(mktemp -d -t pth-creds.XXXXXX)"
    export PTH_CREDENTIALS_FILE="$PTH_CREDS_DIR/credentials.json"
    cleanup_creds() { rm -rf "$PTH_CREDS_DIR"; }
    trap 'cleanup_creds; cleanup' EXIT INT TERM

    log "pth auth login --password (profile=smoke-pth)"
    login_out=$(printf '%s' "$PTH_E2E_PASSWORD" \
        | "$PTH_BIN" --addr "$TARGET_ADDR" --insecure \
            --namespace "$PTH_E2E_NAMESPACE" --profile smoke-pth \
            auth login --password --username "$PTH_E2E_USERNAME" --password-stdin)
    login_user=$(jq -r '.userId' <<<"$login_out")
    [[ -n "$login_user" && "$login_user" != "null" ]] \
        || fail "auth login: missing userId in response: $login_out"

    log "pth auth whoami returns the same userId"
    whoami_out=$("$PTH_BIN" --addr "$TARGET_ADDR" --insecure \
        --namespace "$PTH_E2E_NAMESPACE" --profile smoke-pth auth whoami)
    whoami_user=$(jq -r '.userId' <<<"$whoami_out")
    [[ "$whoami_user" == "$login_user" ]] \
        || fail "whoami userId=$whoami_user, want $login_user (out: $whoami_out)"

    log "pth auth token prints a non-empty bearer"
    token_out=$("$PTH_BIN" --addr "$TARGET_ADDR" --insecure \
        --namespace "$PTH_E2E_NAMESPACE" --profile smoke-pth auth token)
    [[ -n "$token_out" ]] || fail "auth token returned empty"

    # --- pth user (live round-trip) -----------------------------------
    # Spec line for phase 10.4: create→login-as→delete using the active
    # admin profile (smoke-pth, just logged in above). The created user
    # is a fresh AGS test user — its credentials are AGS-generated and
    # round-tripped via stdin into login-as so neither end of the
    # round-trip relies on a static password.
    log "pth user create produces an AGS-generated test user"
    user_create_out=$("$PTH_BIN" --addr "$TARGET_ADDR" --insecure \
        --namespace "$PTH_E2E_NAMESPACE" --profile smoke-pth \
        user create --count 1)
    test_user_id=$(jq -r '.userId' <<<"$user_create_out")
    test_username=$(jq -r '.username' <<<"$user_create_out")
    test_password=$(jq -r '.password' <<<"$user_create_out")
    [[ -n "$test_user_id" && "$test_user_id" != "null" ]] \
        || fail "user create: missing userId in response: $user_create_out"
    [[ -n "$test_password" && "$test_password" != "null" ]] \
        || fail "user create: missing password in response: $user_create_out"

    log "pth user login-as binds the test user to a fresh profile"
    login_as_out=$(printf '%s' "$test_password" \
        | "$PTH_BIN" --addr "$TARGET_ADDR" --insecure \
            --namespace "$PTH_E2E_NAMESPACE" --profile "smoke-test-$test_user_id" \
            user login-as --id "$test_user_id" --password-stdin)
    [[ "$(jq -r '.userId' <<<"$login_as_out")" == "$test_user_id" ]] \
        || fail "user login-as userId mismatch: $login_as_out"
    [[ "$(jq -r '.username' <<<"$login_as_out")" == "$test_username" ]] \
        || fail "user login-as username mismatch: $login_as_out"

    # --- playtest + applicant round-trip (phase 10.5) -----------------
    # admin (smoke-pth) creates a playtest, lists, transitions to OPEN
    # so it's player-visible; then the player profile (smoke-test-$id)
    # signs up and observes PENDING; admin soft-deletes. Slug is
    # uniquified per the cli.md §11 strategy so a failed teardown does
    # not poison the next run against the same namespace.
    smoke_slug="pt-$(date +%s)-$$"
    log "pth playtest create as admin (slug=$smoke_slug)"
    pt_create_out=$("$PTH_BIN" --addr "$TARGET_ADDR" --insecure \
        --namespace "$PTH_E2E_NAMESPACE" --profile smoke-pth \
        playtest create --slug "$smoke_slug" --title "Smoke $smoke_slug" \
        --platforms STEAM --distribution-model STEAM_KEYS)
    smoke_playtest_id=$(jq -r '.playtest.id' <<<"$pt_create_out")
    [[ -n "$smoke_playtest_id" && "$smoke_playtest_id" != "null" ]] \
        || fail "playtest create: missing id: $pt_create_out"

    log "pth playtest list includes the new playtest"
    list_out=$("$PTH_BIN" --addr "$TARGET_ADDR" --insecure \
        --namespace "$PTH_E2E_NAMESPACE" --profile smoke-pth playtest list)
    jq -e --arg id "$smoke_playtest_id" '.playtests[] | select(.id == $id)' <<<"$list_out" >/dev/null \
        || fail "playtest list missing $smoke_playtest_id: $list_out"

    log "pth playtest transition DRAFT → OPEN"
    "$PTH_BIN" --addr "$TARGET_ADDR" --insecure \
        --namespace "$PTH_E2E_NAMESPACE" --profile smoke-pth \
        playtest transition --id "$smoke_playtest_id" --to OPEN >/dev/null

    log "pth applicant signup as test player"
    signup_out=$("$PTH_BIN" --addr "$TARGET_ADDR" --insecure \
        --namespace "$PTH_E2E_NAMESPACE" --profile "smoke-test-$test_user_id" \
        applicant signup --slug "$smoke_slug" --platforms STEAM)
    [[ "$(jq -r '.applicant.status' <<<"$signup_out")" == "APPLICANT_STATUS_PENDING" ]] \
        || fail "applicant signup status != PENDING: $signup_out"

    log "pth applicant status returns PENDING for the same player"
    appstatus_out=$("$PTH_BIN" --addr "$TARGET_ADDR" --insecure \
        --namespace "$PTH_E2E_NAMESPACE" --profile "smoke-test-$test_user_id" \
        applicant status --slug "$smoke_slug")
    [[ "$(jq -r '.applicant.status' <<<"$appstatus_out")" == "APPLICANT_STATUS_PENDING" ]] \
        || fail "applicant status != PENDING: $appstatus_out"

    log "pth playtest delete cleans up the smoke playtest"
    "$PTH_BIN" --addr "$TARGET_ADDR" --insecure \
        --namespace "$PTH_E2E_NAMESPACE" --profile smoke-pth \
        playtest delete --id "$smoke_playtest_id" >/dev/null

    # --- pth flow golden-m1 (live, phase 10.6) ------------------------
    # Same admin (smoke-pth) + player (smoke-test-$id) profiles, fresh
    # slug, exercised through the composite command. Asserts each NDJSON
    # step is OK and the assert-pending step actually fires.
    flow_slug="ptf-$(date +%s)-$$"
    log "pth flow golden-m1 (slug=$flow_slug)"
    flow_out=$("$PTH_BIN" --addr "$TARGET_ADDR" --insecure \
        --namespace "$PTH_E2E_NAMESPACE" \
        flow golden-m1 --slug "$flow_slug" \
        --admin-profile smoke-pth \
        --player-profile "smoke-test-$test_user_id")
    flow_lines=$(printf '%s\n' "$flow_out" | wc -l | tr -d ' ')
    [[ "$flow_lines" -eq 4 ]] \
        || { printf '%s\n' "$flow_out" >&2; fail "flow lines=$flow_lines, want 4"; }
    flow_steps=("create-playtest" "transition-open" "signup" "assert-pending")
    j=0
    while IFS= read -r line; do
        step=$(jq -r '.step' <<<"$line")
        status=$(jq -r '.status' <<<"$line")
        [[ "$step" == "${flow_steps[$j]}" ]] \
            || fail "flow line $((j+1)) step=$step, want ${flow_steps[$j]}"
        [[ "$status" == "OK" ]] \
            || { printf '%s\n' "$flow_out" >&2; fail "flow line $((j+1)) status=$status, want OK"; }
        j=$((j+1))
    done <<<"$flow_out"

    # Soft-delete the playtest the flow created so the namespace stays
    # tidy. flow golden-m1 doesn't return the playtestId in stdout (the
    # NDJSON is for the operator) so we resolve it via list.
    flow_pt_id=$("$PTH_BIN" --addr "$TARGET_ADDR" --insecure \
        --namespace "$PTH_E2E_NAMESPACE" --profile smoke-pth playtest list \
        | jq -r --arg slug "$flow_slug" '.playtests[] | select(.slug == $slug) | .id')
    if [[ -n "$flow_pt_id" ]]; then
        "$PTH_BIN" --addr "$TARGET_ADDR" --insecure \
            --namespace "$PTH_E2E_NAMESPACE" --profile smoke-pth \
            playtest delete --id "$flow_pt_id" >/dev/null
    fi

    log "pth user delete cleans up the test user (--yes)"
    delete_out=$("$PTH_BIN" --addr "$TARGET_ADDR" --insecure \
        --namespace "$PTH_E2E_NAMESPACE" --profile smoke-pth \
        user delete --id "$test_user_id" --yes)
    [[ "$(jq -r '.deleted' <<<"$delete_out")" == "true" ]] \
        || fail "user delete did not return deleted=true: $delete_out"

    # Drop the throwaway login-as profile from the isolated store so the
    # next smoke run starts clean.
    "$PTH_BIN" --addr "$TARGET_ADDR" --insecure \
        --namespace "$PTH_E2E_NAMESPACE" --profile "smoke-test-$test_user_id" \
        auth logout >/dev/null

    log "pth auth logout removes the credential"
    logout_out=$("$PTH_BIN" --addr "$TARGET_ADDR" --insecure \
        --namespace "$PTH_E2E_NAMESPACE" --profile smoke-pth auth logout)
    [[ "$(jq -r '.removed' <<<"$logout_out")" == "true" ]] \
        || fail "logout did not remove profile: $logout_out"

    log "post-logout: pth auth whoami exits non-zero"
    set +e
    "$PTH_BIN" --addr "$TARGET_ADDR" --insecure \
        --namespace "$PTH_E2E_NAMESPACE" --profile smoke-pth auth whoami >/dev/null 2>&1
    post_logout_exit=$?
    set -e
    [[ $post_logout_exit -ne 0 ]] || fail "auth whoami should exit non-zero after logout"
fi

log "PASS"
