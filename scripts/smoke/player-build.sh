#!/usr/bin/env bash
# scripts/smoke/player-build.sh — "does the player app build and its tests pass?" check.
#
# The player app is the M1 phase 9 deliverable. It has no backend surface
# of its own; this script is the frontend analogue of boot.sh — it proves
# that a clean checkout can install deps, typecheck, run unit tests, and
# produce a deployable static bundle.
#
# Sibling to boot.sh / cloud.sh. Callers run all three.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT/player"

log() { printf '[smoke/player] %s\n' "$*" >&2; }
fail() { log "FAIL: $*"; exit 1; }

require() {
    command -v "$1" >/dev/null 2>&1 || fail "$1 not on PATH"
}

require node
require npm

log "installing deps"
# `npm install` over `npm ci`: peer-dep resolution in the current tree
# is more forgiving under npm install, and lockfile-strictness is the
# job of the phase-12 CI gate, not this smoke script.
npm install --no-audit --no-fund >/tmp/playtesthub-player-install.log 2>&1 \
    || { tail -40 /tmp/playtesthub-player-install.log >&2; fail "npm install failed"; }

log "running vitest"
npm run test --silent >/tmp/playtesthub-player-test.log 2>&1 \
    || { tail -80 /tmp/playtesthub-player-test.log >&2; fail "vitest failed"; }

log "running build (svelte-check + vite build)"
npm run build --silent >/tmp/playtesthub-player-build.log 2>&1 \
    || { tail -80 /tmp/playtesthub-player-build.log >&2; fail "build failed"; }

# The bundle must contain a mountable index.html and emit at least one JS
# chunk — a silent empty build (no matching entry glob) would otherwise
# slip through.
[[ -f dist/index.html ]] || fail "dist/index.html missing after build"
compgen -G 'dist/assets/*.js' >/dev/null || fail "no JS chunks under dist/assets/"

# config.json must NOT be checked into dist (gitignored + not an artefact
# of the build). Deployers supply their own.
if [[ -f dist/config.json ]]; then
    fail "dist/config.json should not be shipped by the build (operator supplies it)"
fi

log "PASS"
