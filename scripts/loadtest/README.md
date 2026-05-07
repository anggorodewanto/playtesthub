# scripts/loadtest — perf proof point

Drives the PRD §6 Performance scenario: **500 signups over 10 minutes on
the demo namespace with p95 signup latency < 3s end-to-end**. Reported
per release in [`docs/CHANGELOG.md`](../../docs/CHANGELOG.md). **Not a CI
gate** ([`docs/engineering.md`](../../docs/engineering.md) §5).

## What it measures

The `Signup` RPC against a deployed playtesthub backend, driven at
`50/min` (`= 500/10min = 0.83 RPS`) by [k6](https://k6.io). Each
iteration burns one pre-minted AGS test user — `Signup` is idempotent on
`(playtestId, userId)` per PRD §5.2 / §10 M1, so a token replayed
against the same playtest takes the cheap "return existing" path. The
harness mints `LOADTEST_USERS` unique users (default 500) so every
iteration up to that count exercises the **insert** path.

### What it does NOT measure

- **Discord OAuth redirect time.** PRD §6 includes "AGS IAM + Discord
  OAuth redirect" in the end-to-end p95 budget. The harness uses AGS
  test users (`pth user create`), which carry no Discord ID — the
  Signup handler falls back to the raw IAM `sub` per PRD §10 M1 and
  never round-trips the Discord API. Subtract from the PRD budget the
  Discord API + browser-redirect time you measure separately. Backend
  signup latency is what you read here.
- **DM delivery.** `Signup` schedules no DM. DM enqueue happens at
  approve time. PRD §5.5 and `docs/dm-queue.md` cover the queue
  contract.
- **Multi-replica behaviour.** PRD §6 explicitly excludes multi-replica
  perf from the v1.0 contract.

## Layout

```
scripts/loadtest/
├── README.md         # this file
├── prepare.sh        # idempotent setup: playtest + 500 test users + tokens
├── signup.js         # k6 script — constant-arrival-rate against /signup
├── run.sh            # orchestrator: prepare → k6 → render report
├── report.sh         # k6 summary.json → results/<timestamp>.md
└── results/          # committed Markdown reports per run
```

## Prerequisites

- `.env` filled in (admin creds + namespace + IAM client). See
  [`.env.template`](../../.env.template).
- `pth` built: `make build` (or `go build ./cmd/pth -o bin/pth`).
- Tools on PATH: `k6`, `curl`, `jq`, `xargs`, `bash`.
- Admin AGS user (the same one your `.env` points at) with permission
  to create test users (`POST /iam/v4/admin/namespaces/{ns}/test_users`).

## Running

```bash
source .env
./scripts/loadtest/run.sh
```

Knobs (env vars, all optional):

| Var | Default | Purpose |
| --- | --- | --- |
| `LOADTEST_USERS` | `500` | Unique test users to mint. ≥ k6 iteration count. |
| `LOADTEST_DURATION` | `10m` | k6 scenario duration. |
| `LOADTEST_RATE_PER_MIN` | `50` | Constant arrival rate. |
| `LOADTEST_SLUG` | `loadtest-<rand>` | Playtest slug. Reused across runs if set. |
| `LOADTEST_BASE_URL` | derived from `AGS_BASE_URL` + `AGS_NAMESPACE` | Override the gateway base. |
| `LOADTEST_KEEP_USERS` | `1` | `0` to teardown users after the run. Default keeps them so re-runs reuse the pool. |

## Smoke / sanity run

For a fast pre-check (~30s) that the harness is wired correctly:

```bash
LOADTEST_USERS=10 LOADTEST_DURATION=12s LOADTEST_RATE_PER_MIN=50 ./scripts/loadtest/run.sh
```

This still drives at the PRD target rate but only collects 10 samples —
useful for verifying tokens work, the playtest exists, and the report
renderer fires.

## Reading the report

`results/<timestamp>.md` includes:

- Run config (users, duration, rate, target).
- k6 summary: `http_reqs`, p50/p95/p99, error rate, status-code histogram.
- Pass/fail vs PRD §6: `p95 < 3s`.
- Caveats reapplied (no Discord OAuth, no DM, single-replica).
- **Insert-path verification (Neon)** — see below.

## Insert-path verification

After k6 exits, `run.sh` queries Neon for
`COUNT(DISTINCT user_id) FROM applicant WHERE playtest_id = <run id>`
and asserts the result is at least `min(iterations, token_pool) - 1 - 1%`.

This guards against rotation bugs in `signup.js` where every "signup"
quietly takes the PRD §5.2 idempotent replay path because two VUs alias
onto the same token idx. (Such a regression once shipped: `results/20260507T023635Z.md`
reported p95 = 466 ms with only 11 distinct inserts behind 501
requests; the fix landed in the same PR as this verification step.)

The check requires `DATABASE_URL` set (already in `.env`) and either
`psql` or `docker` on PATH. If neither is available it emits a warning
and skips — k6 still drives the run, you just lose the regression guard.

## Re-runs and cleanup

By default the test-user pool persists between runs (`LOADTEST_KEEP_USERS=1`).
That makes re-runs cheap — `prepare.sh` reads the cached `tokens.json` and
short-circuits user creation. To reset, delete `scripts/loadtest/.cache/`
or run with `LOADTEST_KEEP_USERS=0`. AGS test-user limits per namespace
apply — see your AGS environment plan.

## Why k6, not vegeta

The team already has k6 installed in CI runners and it has built-in
`thresholds` syntax that maps cleanly onto PRD §6's `p95 < 3s`. Either
would work; k6 wins on built-in scenario shapes (constant-arrival-rate)
and JSON summary export.
