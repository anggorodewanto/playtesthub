#!/usr/bin/env bash
# scripts/loadtest/run.sh — orchestrate a perf proof-point run.
#
# 1. Calls prepare.sh to ensure playtest + test-user pool + tokens exist.
# 2. Invokes k6 with signup.js against the deployed gateway.
# 3. Renders results/<timestamp>.md from the k6 summary.
#
# See scripts/loadtest/README.md for env-var knobs.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LOADTEST_DIR="${REPO_ROOT}/scripts/loadtest"
CACHE_DIR="${LOADTEST_DIR}/.cache"
RESULTS_DIR="${LOADTEST_DIR}/results"
mkdir -p "${RESULTS_DIR}"

log()  { printf '[loadtest:run] %s\n' "$*" >&2; }
fail() { log "FAIL: $*"; exit 1; }

require() { command -v "$1" >/dev/null 2>&1 || fail "$1 not on PATH"; }
for tool in k6 jq curl; do require "$tool"; done

LOADTEST_USERS="${LOADTEST_USERS:-500}"
LOADTEST_DURATION="${LOADTEST_DURATION:-10m}"
LOADTEST_RATE_PER_MIN="${LOADTEST_RATE_PER_MIN:-50}"

log "preparing pool (users=${LOADTEST_USERS} duration=${LOADTEST_DURATION} rate=${LOADTEST_RATE_PER_MIN}/min)"
prep_out=$(LOADTEST_USERS="${LOADTEST_USERS}" \
           LOADTEST_SLUG="${LOADTEST_SLUG:-}" \
           LOADTEST_BASE_URL="${LOADTEST_BASE_URL:-}" \
           "${LOADTEST_DIR}/prepare.sh")
echo "${prep_out}" >&2

# prepare.sh emits KEY=VALUE lines on stdout — parse them.
LOADTEST_SLUG=$(awk -F= '/^LOADTEST_SLUG=/ {print $2}' <<<"${prep_out}")
LOADTEST_BASE_URL=$(awk -F= '/^LOADTEST_BASE_URL=/ {print $2}' <<<"${prep_out}")
TOKENS_FILE=$(awk -F= '/^TOKENS_FILE=/ {print $2}' <<<"${prep_out}")
TOKEN_POOL_SIZE=$(awk -F= '/^TOKEN_POOL_SIZE=/ {print $2}' <<<"${prep_out}")

[[ -n "${LOADTEST_SLUG}" ]] || fail "prepare.sh did not emit LOADTEST_SLUG"
[[ -n "${LOADTEST_BASE_URL}" ]] || fail "prepare.sh did not emit LOADTEST_BASE_URL"
[[ -s "${TOKENS_FILE}" ]] || fail "tokens file empty: ${TOKENS_FILE}"

log "tokens=${TOKEN_POOL_SIZE} slug=${LOADTEST_SLUG} base=${LOADTEST_BASE_URL}"

ts=$(date -u +%Y%m%dT%H%M%SZ)
SUMMARY_JSON="${CACHE_DIR}/summary-${ts}.json"
K6_LOG="${CACHE_DIR}/k6-${ts}.log"

log "launching k6 → ${SUMMARY_JSON}"
set +e
k6 run \
    --summary-export="${SUMMARY_JSON}" \
    -e BASE_URL="${LOADTEST_BASE_URL}" \
    -e SLUG="${LOADTEST_SLUG}" \
    -e TOKENS_FILE="${TOKENS_FILE}" \
    -e DURATION="${LOADTEST_DURATION}" \
    -e RATE_PER_MIN="${LOADTEST_RATE_PER_MIN}" \
    "${LOADTEST_DIR}/signup.js" 2>&1 | tee "${K6_LOG}"
k6_exit="${PIPESTATUS[0]}"
set -e

log "k6 exit=${k6_exit} summary=${SUMMARY_JSON}"

REPORT="${RESULTS_DIR}/${ts}.md"
LOADTEST_TOKEN_POOL_SIZE="${TOKEN_POOL_SIZE}" \
LOADTEST_USERS="${LOADTEST_USERS}" \
LOADTEST_DURATION="${LOADTEST_DURATION}" \
LOADTEST_RATE_PER_MIN="${LOADTEST_RATE_PER_MIN}" \
LOADTEST_SLUG="${LOADTEST_SLUG}" \
LOADTEST_BASE_URL="${LOADTEST_BASE_URL}" \
LOADTEST_K6_EXIT="${k6_exit}" \
    "${LOADTEST_DIR}/report.sh" "${SUMMARY_JSON}" > "${REPORT}"

log "report written: ${REPORT}"

# Insert-path verification: regression guard for the rotation bug where
# k6 round-robined VUs onto the same token idx and ~98% of "signups"
# took the idempotent replay path. Compares Neon's distinct user_id
# count for the playtest against k6's iteration count. Skipped (with
# warning) if DATABASE_URL is unset or no Postgres client is available.
verify_inserts() {
    if [[ -z "${DATABASE_URL:-}" ]]; then
        log "WARN: DATABASE_URL unset — skipping insert-path verification"
        return 0
    fi
    local pt_id
    pt_id=$(jq -r '.id // empty' "${CACHE_DIR}/playtest.json" 2>/dev/null)
    if [[ -z "${pt_id}" ]]; then
        log "WARN: no playtest id in cache — skipping insert-path verification"
        return 0
    fi
    local psql_cmd=()
    if command -v psql >/dev/null 2>&1; then
        psql_cmd=(psql)
    elif command -v docker >/dev/null 2>&1; then
        psql_cmd=(docker run --rm -i postgres:16 psql)
    else
        log "WARN: neither psql nor docker on PATH — skipping insert-path verification"
        return 0
    fi
    local iterations
    iterations=$(jq '.metrics.iterations.count // 0' "${SUMMARY_JSON}")
    [[ "${iterations}" =~ ^[0-9]+$ ]] || iterations=0
    local distinct
    distinct=$("${psql_cmd[@]}" "${DATABASE_URL}" -At -c \
        "SELECT COUNT(DISTINCT user_id) FROM applicant WHERE playtest_id = '${pt_id}';" 2>/dev/null) \
        || { log "WARN: DB query failed — skipping insert-path verification"; return 0; }
    [[ "${distinct}" =~ ^[0-9]+$ ]] || { log "WARN: unexpected DB result '${distinct}' — skipping verification"; return 0; }
    local pool="${TOKEN_POOL_SIZE:-${LOADTEST_USERS}}"
    local expected=$(( iterations < pool ? iterations : pool ))
    # Tolerate one wrap replay (iter N+1 reuses token 0) plus a 1% margin
    # for transient request failures already counted by http_req_failed.
    local floor=$(( expected - 1 - expected / 100 ))
    [[ "${floor}" -lt 0 ]] && floor=0
    log "insert-path: distinct=${distinct} expected=${expected} floor=${floor}"

    {
        echo
        echo "## Insert-path verification (Neon)"
        echo
        echo "\`\`\`sql"
        echo "SELECT COUNT(DISTINCT user_id) FROM applicant"
        echo "WHERE playtest_id = '${pt_id}';"
        echo "\`\`\`"
        echo
        echo "| Field | Value |"
        echo "| --- | --- |"
        echo "| k6 iterations | ${iterations} |"
        echo "| Distinct \`user_id\` in DB | ${distinct} |"
        echo "| Expected (≥) | ${floor} |"
        if [[ "${distinct}" -ge "${floor}" ]]; then
            echo "| Verdict | ✅ PASS — insert path actually exercised |"
        else
            echo "| Verdict | ❌ FAIL — too few inserts; check token rotation |"
        fi
    } >> "${REPORT}"

    if [[ "${distinct}" -lt "${floor}" ]]; then
        log "FAIL: insert-path verification (distinct=${distinct} < floor=${floor})"
        return 1
    fi
    return 0
}

verify_exit=0
verify_inserts || verify_exit=$?

final_exit=$(( k6_exit > verify_exit ? k6_exit : verify_exit ))
exit "${final_exit}"
