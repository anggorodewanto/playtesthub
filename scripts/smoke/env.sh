#!/usr/bin/env bash
# scripts/smoke/env.sh — asserts pkg/config hard-fails boot when a PRD
# §5.9 required env var is unset.
#
# This is the "did we actually wire pkg/config?" gate. Unit tests in
# pkg/config cover Load() directly; this script proves the wired binary
# exits with the expected error before migrations run or a port binds.
#
# Intentionally standalone: does not need Postgres, does not care about
# auth. Runs in <5s.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

log() { printf '[smoke:env] %s\n' "$*" >&2; }
fail() { log "FAIL: $*"; exit 1; }

require() {
    command -v "$1" >/dev/null 2>&1 || fail "$1 not on PATH"
}
require go

LOG_FILE="$(mktemp -t playtesthub-smoke-env.XXXXXX.log)"
trap 'rm -f "$LOG_FILE"' EXIT

# Case 1 — every required var unset. Expect non-zero exit and every
# missing key name in stderr so operators see the full list at once.
log "missing-required boot must hard-fail"
set +e
env -i PATH="$PATH" HOME="$HOME" go run . >"$LOG_FILE" 2>&1
exit_code=$?
set -e

if [[ "$exit_code" == 0 ]]; then
    cat "$LOG_FILE" >&2
    fail "expected non-zero exit for missing required env, got 0"
fi

for key in DATABASE_URL DISCORD_BOT_TOKEN AGS_IAM_CLIENT_ID AGS_IAM_CLIENT_SECRET AGS_BASE_URL AGS_NAMESPACE BASE_PATH; do
    if ! grep -q "$key" "$LOG_FILE"; then
        cat "$LOG_FILE" >&2
        fail "missing-required error did not mention $key"
    fi
done

# Case 2 — BASE_PATH malformed (no leading slash). Expect non-zero exit
# and the key name in stderr so the reason is self-describing.
log "malformed BASE_PATH must hard-fail"
set +e
env -i PATH="$PATH" HOME="$HOME" \
    DATABASE_URL="postgres://p:p@localhost/p?sslmode=disable" \
    DISCORD_BOT_TOKEN="x" \
    AGS_IAM_CLIENT_ID="x" \
    AGS_IAM_CLIENT_SECRET="x" \
    AGS_BASE_URL="https://x" \
    AGS_NAMESPACE="x" \
    BASE_PATH="no-leading-slash" \
    go run . >"$LOG_FILE" 2>&1
exit_code=$?
set -e

if [[ "$exit_code" == 0 ]]; then
    cat "$LOG_FILE" >&2
    fail "expected non-zero exit for bad BASE_PATH, got 0"
fi

if ! grep -q "BASE_PATH" "$LOG_FILE"; then
    cat "$LOG_FILE" >&2
    fail "bad-BASE_PATH error did not mention BASE_PATH"
fi

log "PASS"
