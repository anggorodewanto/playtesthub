#!/usr/bin/env bash
# scripts/smoke/admin-build.sh — "does the admin Extend App UI build and its tests pass?" check.
#
# The admin app is the M2 phase 10 deliverable (M1 phase 8 shipped the
# baseline shell). It has no backend surface of its own; this script is
# the frontend analogue of boot.sh — it proves that a clean checkout can
# install deps, typecheck, run unit tests, and produce a deployable
# Module Federation bundle.
#
# Sibling to boot.sh / cloud.sh / player-build.sh. Callers run all four.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT/admin"

log() { printf '[smoke/admin] %s\n' "$*" >&2; }
fail() { log "FAIL: $*"; exit 1; }

require() {
    command -v "$1" >/dev/null 2>&1 || fail "$1 not on PATH"
}

require node
require npm

log "installing deps"
npm install --no-audit --no-fund >/tmp/playtesthub-admin-install.log 2>&1 \
    || { tail -40 /tmp/playtesthub-admin-install.log >&2; fail "npm install failed"; }

# OpenAPI spec + generated clients are gitignored; reseed from the local
# proto-derived swagger so a clean checkout can build without reaching
# the private deployed-namespace URL that `npm run cg:download` targets.
log "seeding OpenAPI spec from gateway/apidocs/api.swagger.json"
mkdir -p swaggers
cp "$REPO_ROOT/gateway/apidocs/api.swagger.json" swaggers/playtesthub.json
log "regenerating typed clients (cg:clean-and-generate)"
npm run cg:clean-and-generate --silent >/tmp/playtesthub-admin-codegen.log 2>&1 \
    || { tail -40 /tmp/playtesthub-admin-codegen.log >&2; fail "cg:clean-and-generate failed"; }

log "running eslint"
npm run lint --silent >/tmp/playtesthub-admin-lint.log 2>&1 \
    || { tail -80 /tmp/playtesthub-admin-lint.log >&2; fail "eslint failed"; }

log "running vitest"
npm run test --silent >/tmp/playtesthub-admin-test.log 2>&1 \
    || { tail -80 /tmp/playtesthub-admin-test.log >&2; fail "vitest failed"; }

log "running build (tsc -b + vite build)"
npm run build --silent >/tmp/playtesthub-admin-build.log 2>&1 \
    || { tail -80 /tmp/playtesthub-admin-build.log >&2; fail "build failed"; }

# Module Federation bundle must include a remoteEntry.js so the AGS
# Admin Portal can hydrate this remote.
[[ -f dist/remoteEntry.js ]] || fail "dist/remoteEntry.js missing after build (Module Federation entry)"
[[ -f dist/index.html ]] || fail "dist/index.html missing after build"
compgen -G 'dist/assets/*.js' >/dev/null || fail "no JS chunks under dist/assets/"

log "PASS"
