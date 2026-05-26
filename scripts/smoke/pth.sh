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
PLAYER_BASE_URL="https://player.smoke.invalid" \
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

# --- pth public-config ------------------------------------------------
# Asserts the env→service wiring for PLAYER_BASE_URL: boot sets
# https://player.smoke.invalid, the unauth RPC must surface that value
# verbatim so the admin AppUI can build cross-app share links.
log "pth public-config returns the booted PLAYER_BASE_URL"
set +e
cfg_out=$("$PTH_BIN" --addr "$TARGET_ADDR" --insecure public-config 2>/tmp/pth-stderr)
cfg_exit=$?
set -e
[[ $cfg_exit -eq 0 ]] \
    || { cat /tmp/pth-stderr >&2; fail "public-config exit=$cfg_exit, want 0 (out: $cfg_out)"; }
[[ "$(jq -r '.player_base_url' <<<"$cfg_out")" == "https://player.smoke.invalid" ]] \
    || fail "public-config player_base_url mismatch: $cfg_out"

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

# M5.A phase 5: --auto-approve + --auto-approve-limit reach the request
# body. Default OFF must not populate auto_approve_limit (oneof presence
# leaks model intent into the wire shape).
log "pth playtest create --auto-approve --dry-run populates auto-approve fields"
create_aa_dry=$("$PTH_BIN" --namespace smoke playtest create \
    --slug demo-02 --title "Demo AA" \
    --platforms STEAM \
    --distribution-model STEAM_KEYS \
    --auto-approve --auto-approve-limit 5 \
    --dry-run)
[[ "$(jq -r '.auto_approve' <<<"$create_aa_dry")" == "true" ]] \
    || fail "playtest create --auto-approve dry-run auto_approve != true: $create_aa_dry"
[[ "$(jq -r '.auto_approve_limit' <<<"$create_aa_dry")" == "5" ]] \
    || fail "playtest create --auto-approve dry-run auto_approve_limit != 5: $create_aa_dry"
[[ "$(jq -r '.auto_approve' <<<"$create_dry")" == "null" || "$(jq -r '.auto_approve' <<<"$create_dry")" == "false" ]] \
    || fail "playtest create default dry-run auto_approve should be unset: $create_dry"

log "pth playtest edit --dry-run"
edit_dry=$("$PTH_BIN" --namespace smoke playtest edit --id p-smoke --title "New" --dry-run)
[[ "$(jq -r '.playtest_id' <<<"$edit_dry")" == "p-smoke" ]] \
    || fail "playtest edit dry-run playtest_id mismatch: $edit_dry"
[[ "$(jq -r '.title' <<<"$edit_dry")" == "New" ]] \
    || fail "playtest edit dry-run title mismatch: $edit_dry"

log "pth playtest edit --auto-approve --dry-run populates auto-approve fields"
edit_aa_dry=$("$PTH_BIN" --namespace smoke playtest edit --id p-smoke \
    --auto-approve --auto-approve-limit 25 --dry-run)
[[ "$(jq -r '.auto_approve' <<<"$edit_aa_dry")" == "true" ]] \
    || fail "playtest edit --auto-approve dry-run auto_approve != true: $edit_aa_dry"
[[ "$(jq -r '.auto_approve_limit' <<<"$edit_aa_dry")" == "25" ]] \
    || fail "playtest edit --auto-approve dry-run auto_approve_limit != 25: $edit_aa_dry"
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

log "pth playtest schedule-info --dry-run echoes the underlying AdminGetPlaytestRequest"
sched_dry=$("$PTH_BIN" --namespace smoke playtest schedule-info --id p-smoke --dry-run)
[[ "$(jq -r '.namespace' <<<"$sched_dry")" == "smoke" ]] \
    || fail "playtest schedule-info dry-run namespace mismatch: $sched_dry"
[[ "$(jq -r '.playtest_id' <<<"$sched_dry")" == "p-smoke" ]] \
    || fail "playtest schedule-info dry-run playtest_id mismatch: $sched_dry"

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

log "pth applicant retry-failed-dms --dry-run prints the request body"
retry_failed_dry=$("$PTH_BIN" --namespace smoke --profile smoke-pth \
    applicant retry-failed-dms --playtest p-smoke --dry-run)
[[ "$(jq -r '.namespace' <<<"$retry_failed_dry")" == "smoke" ]] \
    || fail "applicant retry-failed-dms dry-run namespace mismatch: $retry_failed_dry"
[[ "$(jq -r '.playtest_id' <<<"$retry_failed_dry")" == "p-smoke" ]] \
    || fail "applicant retry-failed-dms dry-run playtest_id mismatch: $retry_failed_dry"

# --- pth survey (dry-run; unconditional) ------------------------------
# Phase 3 (docs/STATUS.md M3): assert the create/edit/get wrappers wire
# the right namespace + playtest_id + questions body without dialling.
# Live round-trip (admin token + real playtest) is gated below on the
# PTH_E2E_* secrets path, in line with M1/M2 wrappers.
log "pth survey create --dry-run prints the request body"
survey_questions_file="$(mktemp -t pth-survey.XXXXXX.json)"
cat >"$survey_questions_file" <<'EOF'
[
  {
    "type": "SURVEY_QUESTION_TYPE_TEXT",
    "prompt": "How was the matchmaking?",
    "required": true
  },
  {
    "type": "SURVEY_QUESTION_TYPE_MULTI_CHOICE",
    "prompt": "Which platforms did you play on?",
    "allowMultiple": true,
    "options": [
      {"label": "Steam"},
      {"label": "Xbox"}
    ]
  }
]
EOF
trap 'rm -f "$survey_questions_file"; cleanup' EXIT INT TERM
survey_create_dry=$("$PTH_BIN" --namespace smoke --profile smoke-pth \
    survey create --playtest p-smoke --from "$survey_questions_file" --dry-run)
[[ "$(jq -r '.namespace' <<<"$survey_create_dry")" == "smoke" ]] \
    || fail "survey create dry-run namespace mismatch: $survey_create_dry"
[[ "$(jq -r '.playtest_id' <<<"$survey_create_dry")" == "p-smoke" ]] \
    || fail "survey create dry-run playtest_id mismatch: $survey_create_dry"
[[ "$(jq -r '.questions | length' <<<"$survey_create_dry")" == "2" ]] \
    || fail "survey create dry-run questions length mismatch: $survey_create_dry"
[[ "$(jq -r '.questions[0].prompt' <<<"$survey_create_dry")" == "How was the matchmaking?" ]] \
    || fail "survey create dry-run questions[0].prompt mismatch: $survey_create_dry"

log "pth survey edit --dry-run prints the request body"
survey_edit_dry=$("$PTH_BIN" --namespace smoke --profile smoke-pth \
    survey edit --playtest p-smoke --from "$survey_questions_file" --dry-run)
[[ "$(jq -r '.playtest_id' <<<"$survey_edit_dry")" == "p-smoke" ]] \
    || fail "survey edit dry-run playtest_id mismatch: $survey_edit_dry"

log "pth survey get --dry-run"
survey_get_dry=$("$PTH_BIN" survey get --playtest p-smoke --dry-run)
[[ "$(jq -r '.playtest_id' <<<"$survey_get_dry")" == "p-smoke" ]] \
    || fail "survey get dry-run playtest_id mismatch: $survey_get_dry"

log "pth survey submit --dry-run prints the request body"
survey_answers_file="$(mktemp -t pth-survey-answers.XXXXXX.json)"
cat >"$survey_answers_file" <<'EOF'
[
  {"questionId": "q1", "text": "free text answer"},
  {"questionId": "q2", "rating": 4},
  {"questionId": "q3", "multiChoice": {"optionIds": ["opt1","opt2"]}}
]
EOF
trap 'rm -f "$survey_questions_file" "$survey_answers_file"; cleanup' EXIT INT TERM
survey_submit_dry=$("$PTH_BIN" --profile smoke-pth \
    survey submit --playtest p-smoke --survey s-smoke --from "$survey_answers_file" --dry-run)
[[ "$(jq -r '.playtest_id' <<<"$survey_submit_dry")" == "p-smoke" ]] \
    || fail "survey submit dry-run playtest_id mismatch: $survey_submit_dry"
[[ "$(jq -r '.survey_id' <<<"$survey_submit_dry")" == "s-smoke" ]] \
    || fail "survey submit dry-run survey_id mismatch: $survey_submit_dry"
[[ "$(jq -r '.answers | length' <<<"$survey_submit_dry")" == "3" ]] \
    || fail "survey submit dry-run answers length mismatch: $survey_submit_dry"
[[ "$(jq -r '.answers[1].rating' <<<"$survey_submit_dry")" == "4" ]] \
    || fail "survey submit dry-run answers[1].rating mismatch: $survey_submit_dry"

log "pth survey responses --dry-run prints the request body"
survey_responses_dry=$("$PTH_BIN" --namespace smoke --profile smoke-pth \
    survey responses --playtest p-smoke --survey s-smoke --page-size 7 --dry-run)
[[ "$(jq -r '.namespace' <<<"$survey_responses_dry")" == "smoke" ]] \
    || fail "survey responses dry-run namespace mismatch: $survey_responses_dry"
[[ "$(jq -r '.playtest_id' <<<"$survey_responses_dry")" == "p-smoke" ]] \
    || fail "survey responses dry-run playtest_id mismatch: $survey_responses_dry"
[[ "$(jq -r '.survey_id_filter' <<<"$survey_responses_dry")" == "s-smoke" ]] \
    || fail "survey responses dry-run survey_id_filter mismatch: $survey_responses_dry"
[[ "$(jq -r '.page_size' <<<"$survey_responses_dry")" == "7" ]] \
    || fail "survey responses dry-run page_size mismatch: $survey_responses_dry"

# --- pth audit (dry-run; unconditional) -------------------------------
# Phase 6 (docs/STATUS.md M3): assert the audit list wrapper wires
# namespace + playtest_id + filter fields without dialling.
log "pth audit list --dry-run prints the request body"
audit_list_dry=$("$PTH_BIN" --namespace smoke --profile smoke-pth \
    audit list --playtest p-smoke --actor system --action playtest.create --page-size 11 --dry-run)
[[ "$(jq -r '.namespace' <<<"$audit_list_dry")" == "smoke" ]] \
    || fail "audit list dry-run namespace mismatch: $audit_list_dry"
[[ "$(jq -r '.playtest_id' <<<"$audit_list_dry")" == "p-smoke" ]] \
    || fail "audit list dry-run playtest_id mismatch: $audit_list_dry"
[[ "$(jq -r '.actor_filter' <<<"$audit_list_dry")" == "system" ]] \
    || fail "audit list dry-run actor_filter mismatch: $audit_list_dry"
[[ "$(jq -r '.action_filter' <<<"$audit_list_dry")" == "playtest.create" ]] \
    || fail "audit list dry-run action_filter mismatch: $audit_list_dry"
[[ "$(jq -r '.page_size' <<<"$audit_list_dry")" == "11" ]] \
    || fail "audit list dry-run page_size mismatch: $audit_list_dry"

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

# --- pth flow golden-m2 --dry-run (unconditional) ---------------------
# Phase 12 (docs/STATUS.md M2): seven NDJSON steps with status=DRY_RUN.
# Adds the M2 surface (accept-nda, upload-codes, approve, get-code) to
# the M1 golden flow.
log "pth flow golden-m2 --dry-run emits 7 NDJSON steps without dialling"
flow_dry_m2=$("$PTH_BIN" --namespace smoke flow golden-m2 --slug demo-flow-m2 --dry-run)
flow_dry_m2_lines=$(printf '%s\n' "$flow_dry_m2" | wc -l | tr -d ' ')
[[ "$flow_dry_m2_lines" -eq 7 ]] \
    || { printf '%s\n' "$flow_dry_m2" >&2; fail "flow golden-m2 dry-run lines=$flow_dry_m2_lines, want 7"; }
expected_steps_m2=("create-playtest" "transition-open" "signup" "accept-nda" "upload-codes" "approve" "get-code")
i=0
while IFS= read -r line; do
    step=$(jq -r '.step' <<<"$line")
    status=$(jq -r '.status' <<<"$line")
    [[ "$step" == "${expected_steps_m2[$i]}" ]] \
        || fail "flow golden-m2 dry-run line $((i+1)) step=$step, want ${expected_steps_m2[$i]}"
    [[ "$status" == "DRY_RUN" ]] \
        || fail "flow golden-m2 dry-run line $((i+1)) status=$status, want DRY_RUN"
    i=$((i+1))
done <<<"$flow_dry_m2"

# M5.A phase 5: --auto-approve variant hoists upload-codes before signup
# and substitutes `assert-applicant-auto-approved` for the manual
# `approve` step. Still seven NDJSON steps, different ordering + tail.
log "pth flow golden-m2 --auto-approve --dry-run reorders the prefix and replaces approve"
flow_dry_m2_aa=$("$PTH_BIN" --namespace smoke flow golden-m2 --slug demo-flow-m2-aa \
    --auto-approve --auto-approve-limit 5 --dry-run)
flow_dry_m2_aa_lines=$(printf '%s\n' "$flow_dry_m2_aa" | wc -l | tr -d ' ')
[[ "$flow_dry_m2_aa_lines" -eq 7 ]] \
    || { printf '%s\n' "$flow_dry_m2_aa" >&2; fail "flow golden-m2 --auto-approve dry-run lines=$flow_dry_m2_aa_lines, want 7"; }
expected_steps_m2_aa=("create-playtest" "transition-open" "upload-codes" "signup" "accept-nda" "assert-applicant-auto-approved" "get-code")
i=0
while IFS= read -r line; do
    step=$(jq -r '.step' <<<"$line")
    [[ "$step" == "${expected_steps_m2_aa[$i]}" ]] \
        || fail "flow golden-m2 --auto-approve dry-run line $((i+1)) step=$step, want ${expected_steps_m2_aa[$i]}"
    i=$((i+1))
done <<<"$flow_dry_m2_aa"
# The create-playtest request body must carry auto_approve=true + the cap.
create_line=$(printf '%s\n' "$flow_dry_m2_aa" | head -n1)
[[ "$(jq -r '.request.auto_approve' <<<"$create_line")" == "true" ]] \
    || fail "flow golden-m2 --auto-approve create-playtest auto_approve != true: $create_line"
[[ "$(jq -r '.request.auto_approve_limit' <<<"$create_line")" == "5" ]] \
    || fail "flow golden-m2 --auto-approve create-playtest auto_approve_limit != 5: $create_line"

# --- pth flow golden-m3 --dry-run (unconditional) ---------------------
# Phase 12 (docs/STATUS.md M3): ten NDJSON steps with status=DRY_RUN.
# Adds the M3 survey surface (create-survey, submit-response,
# list-responses) to the golden-m2 flow.
log "pth flow golden-m3 --dry-run emits 10 NDJSON steps without dialling"
flow_dry_m3=$("$PTH_BIN" --namespace smoke flow golden-m3 --slug demo-flow-m3 --dry-run)
flow_dry_m3_lines=$(printf '%s\n' "$flow_dry_m3" | wc -l | tr -d ' ')
[[ "$flow_dry_m3_lines" -eq 10 ]] \
    || { printf '%s\n' "$flow_dry_m3" >&2; fail "flow golden-m3 dry-run lines=$flow_dry_m3_lines, want 10"; }
expected_steps_m3=("create-playtest" "transition-open" "signup" "accept-nda" "upload-codes" "approve" "get-code" "create-survey" "submit-response" "list-responses")
i=0
while IFS= read -r line; do
    step=$(jq -r '.step' <<<"$line")
    status=$(jq -r '.status' <<<"$line")
    [[ "$step" == "${expected_steps_m3[$i]}" ]] \
        || fail "flow golden-m3 dry-run line $((i+1)) step=$step, want ${expected_steps_m3[$i]}"
    [[ "$status" == "DRY_RUN" ]] \
        || fail "flow golden-m3 dry-run line $((i+1)) status=$status, want DRY_RUN"
    i=$((i+1))
done <<<"$flow_dry_m3"

# M5.A phase 5: golden-m3 inherits the golden-m2 auto-approve prefix
# reordering. Ten lines, but the M2-prefix tail uses
# assert-applicant-auto-approved + upload-codes hoisted before signup.
log "pth flow golden-m3 --auto-approve --dry-run inherits the M2 auto-approve prefix"
flow_dry_m3_aa=$("$PTH_BIN" --namespace smoke flow golden-m3 --slug demo-flow-m3-aa \
    --auto-approve --auto-approve-limit 5 --dry-run)
flow_dry_m3_aa_lines=$(printf '%s\n' "$flow_dry_m3_aa" | wc -l | tr -d ' ')
[[ "$flow_dry_m3_aa_lines" -eq 10 ]] \
    || { printf '%s\n' "$flow_dry_m3_aa" >&2; fail "flow golden-m3 --auto-approve dry-run lines=$flow_dry_m3_aa_lines, want 10"; }
expected_steps_m3_aa=("create-playtest" "transition-open" "upload-codes" "signup" "accept-nda" "assert-applicant-auto-approved" "get-code" "create-survey" "submit-response" "list-responses")
i=0
while IFS= read -r line; do
    step=$(jq -r '.step' <<<"$line")
    [[ "$step" == "${expected_steps_m3_aa[$i]}" ]] \
        || fail "flow golden-m3 --auto-approve dry-run line $((i+1)) step=$step, want ${expected_steps_m3_aa[$i]}"
    i=$((i+1))
done <<<"$flow_dry_m3_aa"

# --- pth flow golden-m4 --dry-run (unconditional) ---------------------
# STATUS_M4 phase 8: four NDJSON steps with status=DRY_RUN. Window-
# enforcement flow drives create-playtest (with startsAt/endsAt set)
# → await-auto-open → await-auto-close → assert-system-transitions.
log "pth flow golden-m4 --dry-run emits 4 NDJSON steps without dialling"
flow_dry_m4=$("$PTH_BIN" --namespace smoke flow golden-m4 --slug demo-flow-m4 --dry-run)
flow_dry_m4_lines=$(printf '%s\n' "$flow_dry_m4" | wc -l | tr -d ' ')
[[ "$flow_dry_m4_lines" -eq 4 ]] \
    || { printf '%s\n' "$flow_dry_m4" >&2; fail "flow golden-m4 dry-run lines=$flow_dry_m4_lines, want 4"; }
expected_steps_m4=("create-playtest" "await-auto-open" "await-auto-close" "assert-system-transitions")
i=0
while IFS= read -r line; do
    step=$(jq -r '.step' <<<"$line")
    status=$(jq -r '.status' <<<"$line")
    [[ "$step" == "${expected_steps_m4[$i]}" ]] \
        || fail "flow golden-m4 dry-run line $((i+1)) step=$step, want ${expected_steps_m4[$i]}"
    [[ "$status" == "DRY_RUN" ]] \
        || fail "flow golden-m4 dry-run line $((i+1)) status=$status, want DRY_RUN"
    i=$((i+1))
done <<<"$flow_dry_m4"

# --- pth flow golden-m5 --dry-run (unconditional) ---------------------
# STATUS_M5.md B9: eleven NDJSON steps with status=DRY_RUN. ADT flow
# drives link-adt-start → link-adt-complete → adt-build-list →
# create-playtest (ADT + auto-approve) → transition OPEN → signup →
# assert-applicant-auto-approved → get-adt-download-info ×2 → audit
# list ×2.
log "pth flow golden-m5 --dry-run emits 11 NDJSON steps without dialling"
flow_dry_m5=$("$PTH_BIN" --namespace smoke flow golden-m5 --slug demo-flow-m5 --dry-run)
flow_dry_m5_lines=$(printf '%s\n' "$flow_dry_m5" | wc -l | tr -d ' ')
[[ "$flow_dry_m5_lines" -eq 11 ]] \
    || { printf '%s\n' "$flow_dry_m5" >&2; fail "flow golden-m5 dry-run lines=$flow_dry_m5_lines, want 11"; }
expected_steps_m5=(
    "adt-link-start"
    "adt-link-complete"
    "adt-build-list"
    "create-playtest"
    "transition-open"
    "signup"
    "assert-applicant-auto-approved"
    "get-adt-download-info"
    "assert-adt-download-non-empty"
    "audit-list-auto-approved"
    "audit-list-adt-linkage"
)
i=0
while IFS= read -r line; do
    step=$(jq -r '.step' <<<"$line")
    status=$(jq -r '.status' <<<"$line")
    [[ "$step" == "${expected_steps_m5[$i]}" ]] \
        || fail "flow golden-m5 dry-run line $((i+1)) step=$step, want ${expected_steps_m5[$i]}"
    [[ "$status" == "DRY_RUN" ]] \
        || fail "flow golden-m5 dry-run line $((i+1)) status=$status, want DRY_RUN"
    i=$((i+1))
done <<<"$flow_dry_m5"

# --- pth adt linkage / build dry-run probes ---------------------------
log "pth adt linkage list --dry-run prints the request body"
"$PTH_BIN" --namespace smoke adt linkage list --dry-run >/dev/null \
    || fail "adt linkage list --dry-run exited non-zero"
log "pth adt linkage start --dry-run prints the request body"
"$PTH_BIN" --namespace smoke adt linkage start --dry-run >/dev/null \
    || fail "adt linkage start --dry-run exited non-zero"
log "pth adt linkage complete --dry-run prints the request body"
"$PTH_BIN" --namespace smoke adt linkage complete --state s --adt-namespace adt-ns-1 --dry-run >/dev/null \
    || fail "adt linkage complete --dry-run exited non-zero"
log "pth adt linkage unlink --dry-run prints the request body"
"$PTH_BIN" --namespace smoke adt linkage unlink --id 01234567-89ab-cdef-0123-456789abcdef --dry-run >/dev/null \
    || fail "adt linkage unlink --dry-run exited non-zero"
log "pth adt linkage recover --dry-run prints the request body"
"$PTH_BIN" --namespace smoke adt linkage recover --adt-namespace adt-ns-orphan --dry-run >/dev/null \
    || fail "adt linkage recover --dry-run exited non-zero"
log "pth adt build list --dry-run prints the request body"
"$PTH_BIN" --namespace smoke adt build list --linkage-id 01234567-89ab-cdef-0123-456789abcdef --game-id game-x --dry-run >/dev/null \
    || fail "adt build list --dry-run exited non-zero"
log "pth adt build change --dry-run prints the request body"
"$PTH_BIN" --namespace smoke adt build change --playtest-id 01234567-89ab-cdef-0123-456789abcdef --game-id game-y --build-id build-002 --dry-run >/dev/null \
    || fail "adt build change --dry-run exited non-zero"
log "pth adt build check --dry-run prints the request body"
"$PTH_BIN" --namespace smoke adt build check --playtest-id 01234567-89ab-cdef-0123-456789abcdef --dry-run >/dev/null \
    || fail "adt build check --dry-run exited non-zero"
log "pth adt games list --dry-run prints the request body"
"$PTH_BIN" --namespace smoke adt games list --linkage-id 01234567-89ab-cdef-0123-456789abcdef --dry-run >/dev/null \
    || fail "adt games list --dry-run exited non-zero"
log "pth adt diagnostics --dry-run prints the request body"
"$PTH_BIN" --namespace smoke adt diagnostics --dry-run >/dev/null \
    || fail "adt diagnostics --dry-run exited non-zero"

# --- pth announcement dry-run probes (M5.C) --------------------------
log "pth announcement create --dry-run prints the request body"
"$PTH_BIN" --namespace smoke announcement create \
    --playtest-id 01234567-89ab-cdef-0123-456789abcdef \
    --send-to APPROVED_ONLY \
    --subject "smoke subject" --message "smoke message" \
    --dry-run >/dev/null \
    || fail "announcement create --dry-run exited non-zero"
log "pth announcement list --dry-run prints the request body"
"$PTH_BIN" --namespace smoke announcement list \
    --playtest-id 01234567-89ab-cdef-0123-456789abcdef --dry-run >/dev/null \
    || fail "announcement list --dry-run exited non-zero"
log "pth announcement create rejects empty subject"
if "$PTH_BIN" --namespace smoke announcement create \
    --playtest-id 01234567-89ab-cdef-0123-456789abcdef \
    --subject "" --message "msg" --dry-run >/dev/null 2>&1; then
    fail "announcement create --subject '' should have exited non-zero"
fi
log "pth announcement create rejects bogus --send-to"
if "$PTH_BIN" --namespace smoke announcement create \
    --playtest-id 01234567-89ab-cdef-0123-456789abcdef \
    --send-to EVERYBODY \
    --subject "s" --message "m" --dry-run >/dev/null 2>&1; then
    fail "announcement create --send-to=EVERYBODY should have exited non-zero"
fi

# --- pth M2 subcommand catalogue presence -----------------------------
# Phase 12 commits the §6.2 surface to the catalogue. The byte-exact
# diff lives in cmd/pth/testdata/describe.golden.json — this probe is a
# defence-in-depth check that the binary emits every M2 entry name so
# AI consumers don't see a surprise drop on a dirty checkout.
log "pth describe contains every M2 §6.2 subcommand"
m2_required=(
    "applicant accept-nda"
    "applicant approve"
    "applicant get-code"
    "applicant list"
    "applicant reject"
    "applicant retry-dm"
    "code pool"
    "code sync-from-ags"
    "code top-up"
    "code upload"
    "flow golden-m2"
)
for name in "${m2_required[@]}"; do
    jq -e --arg n "$name" '.commands[] | select(.name == $n)' <<<"$describe_out" >/dev/null \
        || fail "describe missing M2 command: $name"
done

# M3 phase 3: survey CRUD wrappers must surface in the catalogue. Same
# defence-in-depth check as the M2 set above; the byte-exact assertion
# lives in cmd/pth/testdata/describe.golden.json.
log "pth describe contains every M3 phase 3 survey subcommand"
m3_required=(
    "survey create"
    "survey edit"
    "survey get"
    "survey submit"
    "survey responses"
    "audit list"
    "applicant retry-failed-dms"
)
for name in "${m3_required[@]}"; do
    jq -e --arg n "$name" '.commands[] | select(.name == $n)' <<<"$describe_out" >/dev/null \
        || fail "describe missing M3 command: $name"
done

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
