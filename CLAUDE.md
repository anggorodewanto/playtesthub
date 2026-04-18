# CLAUDE.md

Instructions for Claude Code when working in this repository.

## Project

**playtesthub** — open-source, self-hosted AccelByte Gaming Services (AGS) Extend application for running closed game playtests. Go + gRPC backend, Svelte player frontend, React admin extension. MIT-licensed. See `docs/PRD.md` for full product requirements.

**Current state**: docs-only. No code has been written yet. All milestones in `docs/STATUS.md` are `not started`.

## Canonical docs

Before touching code, orient against these — they are the sources of truth:

| Doc | Purpose |
| --- | --- |
| `docs/PRD.md` | Product requirements. **Authoritative** for behavior — if `errors.md` or any other doc disagrees with the PRD prose, the PRD wins. |
| `docs/schema.md` | DB schemas, audit-log enum + JSONB shapes, fenced-finalize SQL. Authoritative for shapes. |
| `docs/errors.md` | Byte-exact gRPC error codes / messages. |
| `docs/architecture.md` | Stack + external dependency detail. |
| `docs/dm-queue.md` | DM worker FIFO, circuit breaker, restart sweep. |
| `docs/ags-failure-modes.md` | AGS Platform retry policy, cleanup matrix, M2 sub-cap rules. |
| `docs/STATUS.md` | Build status + intra-milestone ordering + cut-if-behind tracking. |
| `docs/engineering.md` | Repo layout, test strategy, TDD workflow, CI gates. |
| `docs/cli.md` | `pth` CLI spec — surface for humans + AI to exercise the app e2e; also the e2e test harness. |
| `docs/CHANGELOG.md` | PRD version history. Do not duplicate it. |

## Stack

- **Backend**: Go, gRPC + grpc-gateway (REST proxy in-process), Postgres (Extend-managed). Schema migrations via `golang-migrate`. DB driver: `pgx`.
- **Player frontend**: Svelte, static bundle, hosted on GitHub Pages / Vercel. Consumes the backend via the grpc-gateway REST surface (**not** grpc-web).
- **Admin frontend**: **Extend App UI** — React 19 + TypeScript + Vite bundled as a Module Federation remote, hosted by AccelByte and rendered inside the AGS Admin Portal (**Extend → My Extend Apps → App UI**). Uses Ant Design v6 + Tailwind v4. Typed backend clients + react-query hooks are generated from the grpc-gateway OpenAPI spec (`apidocs/api.json`) via `@accelbyte/codegen`. Auth inherited from the Admin Portal `HostContext`; `@accelbyte/sdk-iam` owns token lifecycle. The legacy `justice-adminportal-extension-website` / `justice-ui-library` pattern is **not used**. **Caveat**: Extend App UI is Internal Shared Cloud only (PRD §9 R11).
- **Base template**: [`AccelByte/extend-service-extension-go`](https://github.com/AccelByte/extend-service-extension-go). Cloned (not forked) with `.git` removed and re-initialised as a fresh standalone repo under **`github.com/anggorodewanto/playtesthub`**. No upstream tracking — cherry-pick template fixes manually if any appear. Replace the template's CloudSave `pkg/storage` with Postgres; add `migrations/` and a real test suite (template ships with neither).
- **AGS SDK**: `github.com/AccelByte/accelbyte-go-sdk` for IAM token validation and Platform / Campaign API calls. Auth interceptor validates the AGS IAM JWT on every admin/player RPC.

## Workflow: red–green TDD

This repo is TDD-first. Every production change follows the loop:

1. **Red**: write a failing test that describes the behavior (unit or integration, whichever matches the layer — see `docs/engineering.md`).
2. **Green**: write the minimum code to pass. No speculative abstractions.
3. **Refactor**: clean up with tests green. Keep diffs small.

Before marking any task done:
- `go test ./...` passes locally (unit + integration; integration uses testcontainers-postgres — see `docs/engineering.md`).
- `golangci-lint run` clean.
- Proto changes: `buf lint` + regenerated stubs committed.
- New RPC: the `docs/errors.md` row for every new error condition exists and matches the code byte-for-byte.

Do not commit code whose tests were skipped, or tests that pass without actually asserting the behavior in their name. If a test is hard to write, the design is probably wrong — pause and discuss.

## Conventions

- **Early return** over nested conditionals (matches user-global preference; worth reinforcing here because the codebase is greenfield and sets the tone).
- **Error wrapping**: `fmt.Errorf("doing X: %w", err)`. Never `return err` from a leaf call without context.
- **Structured logs only**: JSON, include `requestId`, `userId` (when authed), `playtestId` (when in scope), `action`. **Never log** NDA text, survey free-text answers, or `Code.value`. See PRD §6 Observability.
- **gRPC errors**: use `status.Error(codes.X, msg)`. Byte-exact strings for rows flagged non–"implementation-defined" in `docs/errors.md`.
- **Migrations are append-only**: never edit a committed migration file. Add a new numbered migration that fixes forward.
- **Environment variables only** for backend config — no config files (PRD §5.9).
- **No new top-level docs** unless the user asks. Product behavior belongs in the PRD; build/engineering context belongs in `docs/engineering.md`.
- **Comments**: default to none. Only when the *why* is non-obvious (a hidden invariant, a subtle concurrency rule, an AGS-side quirk). Do not restate the code.

## Destructive operations

Always ask before:
- dropping tables / truncating data in any Postgres instance (local, testcontainers counts too if shared state matters);
- deleting AGS Items / Campaigns against a real AGS namespace;
- force-pushing or rewriting shared branches.

Dev DBs reset on test boot are fine without asking.

## When you make changes

- Update `docs/STATUS.md` when a deliverable lands — flip `not started` → `in progress` → `shipped`.
- Update `docs/CHANGELOG.md` only for PRD changes. Code changes land in commit messages, not the CHANGELOG.
- If a PRD requirement looks wrong or underspecified, surface it explicitly; do not silently deviate. The PRD prose is authoritative.
