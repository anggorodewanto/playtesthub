# playtesthub — Build Status

Tracks playtesthub build status. PRD version history lives in [`CHANGELOG.md`](CHANGELOG.md); full requirements live in [`PRD.md`](PRD.md); schema definitions live in [`schema.md`](schema.md); build conventions (repo layout, test strategy, TDD workflow, CI gates) live in [`engineering.md`](engineering.md).

## Status legend

- `not started` — no code written yet
- `in progress` — some code landed; milestone not yet complete
- `shipped` — milestone deliverables merged and demoed

## Milestones

Milestone scope is defined in PRD §10.

| Milestone | Scope (summary) | Status |
| --------- | --------------- | ------ |
| **M1 — Backend + admin CRUD + signup (STEAM_KEYS only)** | Postgres schema (Playtest incl. `distributionModel`/`agsItemId`/`agsCampaignId`/`initialCodeQuantity`, Applicant, Code, `leader_lease`). Admin Portal extension site with Playtest list + create/edit (+ soft-delete). Player Svelte app: landing + Discord-through-IAM login + signup form with `config.json` loader. `distributionModel` field exists but only `STEAM_KEYS` is functional. Golden flow stops at "applicant is PENDING". | in progress |
| **M2 — NDA + approval + code grant (pool-only) + AGS Campaign API integration** | NDA click-accept + versioned acceptance storage. Admin applicants page with approve/reject. Key pool management: STEAM_KEYS CSV upload; AGS_CAMPAIGN pool status + "Generate more codes" + "Sync from AGS". AGS Campaign API integration (Item + Campaign creation + code generation/fetch). Approve API: reserve → fenced finalize (pool-only grant, §4.1 step 6). Background reclaim job with TTL. `AuditLog` write path wired for NDA edits, playtest edits, `code.grant_orphaned`, `code.upload_rejected`, and `campaign.*` events. Golden flow stops at "player sees code in UI". | not started |
| **M3 — Survey (when configured) + Discord notifications + polish/docs/demo** | Survey builder (text / rating / multi-choice, optional per playtest). Survey response submission (one-shot immutable) + responses viewer (aggregates split by `surveyId`). Discord DM notification on approval. Audit log viewer UI (paginated, filterable, JSON diff). Perf proof point: 500 signups / 10 min at p95 < 3s end-to-end. README walkthrough (including RBAC warning), demo video, public MIT repo. | not started |

## Intra-milestone ordering

Per-milestone execution sequence. Each phase is red–green TDD per [`engineering.md`](engineering.md) §4 — tests land with the code, not after. The ordering minimises rework: proto and schema lock the contracts before any handler depends on them; repository tests exercise real SQL before service handlers mock repos; per-RPC vertical slices give a demoable golden flow as early as possible.

### M1 — Backend + admin CRUD + signup (STEAM_KEYS only)

Target: golden flow stops at "applicant is PENDING".

Phase status legend: `[ ]` not started · `[~]` in progress · `[x]` shipped.

**Smoke harness policy** (cross-cutting): `scripts/smoke/` carries bash smoke scripts that exercise the real binary. `boot.sh` landed alongside phase 3 and is the minimum bar today (service boots, reflection lists all 10 M1 methods, OpenAPI spec served, an unauth RPC reaches the handler). Every phase that adds user-visible behavior extends the harness with a script that hits the RPC paths it introduces — phases 6, 7, 8, 9 all carry this sub-deliverable. The harness is deliberately bash + `grpcurl` + `curl` so it runs before the `pth` CLI exists (phase 10). Once `pth flow golden-m1` lands in phase 10, it supersedes the bash harness and becomes the authoritative e2e gate; `scripts/smoke/` can be deleted then (or kept as a minimal sanity tool for contributors without IAM creds).

1. `[x]` **Scaffold from template** — clone `AccelByte/extend-service-extension-go`, `rm -rf .git`, `git init`, push to `github.com/anggorodewanto/playtesthub`. Rewrite the `go.mod` module path + all internal imports to `github.com/anggorodewanto/playtesthub`. Strip the CloudSave `pkg/storage`; add `migrations/`, `pkg/repo/`, `pkg/config/`; confirm the gRPC + grpc-gateway skeleton builds and boots.
2. `[x]` **Proto `playtesthub.v1`** — define M1 RPCs from PRD §4.7: `GetPublicPlaytest` (unauth), `GetPlaytestForPlayer`, `AdminGetPlaytest`, `ListPlaytests`, `CreatePlaytest`, `EditPlaytest`, `SoftDeletePlaytest`, `TransitionPlaytestStatus`, `Signup`, `GetApplicantStatus`. Wire protoc codegen (via `proto.sh`) + `buf lint`. No handlers yet.
3. `[x]` **Postgres schema — migration 0001** — `Playtest`, `Applicant`, `Code`, `leader_lease`, `AuditLog` tables per [`schema.md`](schema.md). Indexes included. `pkg/migrate` runner + testcontainers-postgres integration test land with the migration; `scripts/smoke/boot.sh` verifies the binary boots end-to-end, applies migrations, exposes all 10 RPCs via gRPC reflection + the OpenAPI gateway, and reaches the handler layer. GitHub Actions `migrate up` gate is deferred to phase 12 with the rest of the CI workflow file.
4. `[x]` **Repository layer (`pkg/repo`)** — one package member per table with an interface + Postgres implementation. **Integration tests against testcontainers-postgres** ([`engineering.md`](engineering.md) §3.2) cover: slug uniqueness across soft-deleted rows, `UNIQUE (playtestId, value)` on Code, audit-log JSONB round-trip, status-transition CAS.
5. `[x]` **Config + IAM wiring** — `pkg/config` env-var parser (PRD §5.9 required vars); `pkg/iam` JWT validator wrapping the AccelByte Go SDK; AGS admin `sub` → `actorUserId` plumbed through request context.
6. `[x]` **Playtest RPCs — service layer** — per-RPC red-green: `CreatePlaytest` (STEAM_KEYS only; AGS_CAMPAIGN returns `Unimplemented`), `EditPlaytest` (whitelist enforcement), `SoftDeletePlaytest`, `TransitionPlaytestStatus`, the three `Get` variants, `ListPlaytests`. Every error row in [`errors.md`](errors.md) for these RPCs asserted in a handler test.
7. `[x]` **Signup RPC** — `Signup` + `GetApplicantStatus`. Discord handle lookup (`pkg/discord`) via bot token at signup, fallback to raw Discord ID on failure (PRD §10 M1). Signup idempotency (`(playtestId, userId)` natural key).
8. `[x]` **Extend App UI skeleton** *(except appui upload — see note)* — scaffolded `admin/` off `AccelByte/extend-app-ui-templates@templates/react` (the bundled CLI scenario list has no "Extend App UI" entry — direct git clone required). Tournament placeholder stripped. Vitest + React Testing Library wired (template ships no runner). `swaggers.json` points at the deployed backend URL (`<AGS_BASE_URL>/ext-<ns>-<app>/apidocs/api.json`); `npm run codegen` emits `src/playtesthubapi/`. Playtest list + create/edit (+ soft-delete) pages live in `admin/src/federated-element.tsx` against the generated react-query hooks; distribution-model selector shows AGS_CAMPAIGN visibly disabled with an M2 tooltip. Cloud deploy path validated end-to-end: `extend-helper-cli create-app` → `update-var`/`update-secret` → `image-upload --login` → `deploy-app --wait` — backend live at the deployed URL and `scripts/smoke/cloud.sh` (new) passes against it. **Deferred**: `extend-helper-cli appui create` + `appui upload` — the public `extend-helper-cli` (latest v0.0.10) ships no `appui` subcommand and `custom-service-manager@1.31.0` in the target namespace exposes no AppUI REST endpoint. PRD §9 R11 already flags AppUI as experimental Internal-Shared-Cloud-only; re-attempt when AGS surfaces the API. Everything else on this phase is deliverable-complete.
9. `[ ]` **Player Svelte app skeleton** — `player/` with `config.json` loader (hard-fail on malformed per PRD §5.8), landing page (unauth field set), Discord-through-IAM login, signup form, pending-state screen.
10. `[ ]` **CLI harness (`pth`)** — `cmd/pth/` binary per [`cli.md`](cli.md). Real-IAM only; no fake-JWT path. Ships meta commands (`doctor`, `describe`, `version`, `--dry-run`); the auth group (`login --discord` loopback + `--manual` fallback, `login --password`, `logout`, `whoami`, `token`) with credential store at `~/.config/playtesthub/credentials.json` and refresh-token rotation; the user group (`create`, `delete`, `login-as`) wrapping AGS IAM admin endpoints; every M1 playtest/applicant subcommand (§6.1); and the `pth flow golden-m1` composite. `describe` catalogue diff-checked in CI. Consumed by the M1 e2e test (phase 11). **Setup prerequisite**: register `http://127.0.0.1/callback` (no port) as an allowed redirect URI on the playtesthub IAM client. AGS IAM ignores port + allows http/https interchange for loopback hosts, so one registration covers every ephemeral port the CLI picks.
11. `[ ]` **E2E golden flow (M1 stop)** — a top-level `e2e/` test that boots the backend in-process against testcontainers-postgres, points `pth` at the real configured AGS IAM (user's own namespace per `cli.md` §7.4), and drives `pth flow golden-m1` against a per-run throwaway test user provisioned via `pth user create`. Asserts the applicant row lands with `status=PENDING`; teardown deletes the test user and soft-deletes the test playtest. Slugs + usernames namespaced `e2e-<timestamp>-<random>` for collision safety.
12. `[ ]` **CI + README walkthrough** — every gate in [`engineering.md`](engineering.md) §5 live on the GitHub Actions workflow; README reproduces the golden flow on a clean checkout using `pth flow golden-m1`; MIT `LICENSE` at repo root.

### M2 — NDA + approval + code grant + AGS Campaign API + RetryDM

Detailed ordering TBD at M1 exit; rough shape: `AcceptNDA` + hash-normalisation → code pool upload (STEAM_KEYS) → reserve-finalize-reclaim approve flow with fenced SQL + leader-elected reclaim job → DM queue (`pkg/dmqueue`) + `RetryDM` → AGS Platform client (`pkg/ags`) + AGS_CAMPAIGN auto-provisioning + `TopUpCodes` + `SyncFromAGS` → admin applicants & key-pool pages.

### M3 — Survey + Discord notifications + polish/docs/demo

Detailed ordering TBD at M2 exit. Includes the DM circuit breaker (if not already in M2), `RetryFailedDms`, survey builder + response one-shot, audit-log viewer UI, perf proof point, demo video.

## How to update

When a milestone deliverable merges, flip its row to `in progress` or `shipped` as appropriate. Add notes inline in a third column if the status needs qualification (e.g. "shipped except responses CSV export — cut per §10 M3"). Do not use this file for PRD version history — that belongs in `CHANGELOG.md`.

### Cut-if-behind tracking

Features that may be cut from their target milestone if the team is behind schedule. Listed here (rather than in PRD §10) so the PRD stays aspirational while the build plan tracks scope risk. On cut, record the decision in the milestone row's notes column above.

#### M1
- **Markdown rendering** — superseded: PRD §5.1 now specifies plain-text rendering for `Playtest.description` in MVP. Markdown rendering is on the post-MVP backlog (PRD §10 Deferred). Not an active cut candidate for M1.

#### M2
- **AGS_CAMPAIGN top-up UI** — ship initial-generate only if behind. Top-up + sync deferred post-v1.0; admin UI hides those actions. Aligns with `ags-failure-modes.md` sub-cap 6/7 partial-ship rules.
- **Rejection reasons** — ship reject without the optional `rejectionReason` text field if behind. `Applicant.rejectionReason` remains admin-visible in schema; UI affordance for entering it is the cut candidate.

#### M3
- **Responses CSV export** — ship responses viewer without export if behind. On PRD §10 Deferred backlog.
- **Drag-reorder for survey questions** — ship up/down buttons only if behind.
- **Webhook-channel fallback** — DM-only delivery if behind. On PRD §10 Deferred backlog as R2 mitigation.
- **Audit log viewer UI** (PRD §5.7 page 5) — ship without the viewer if behind. **Audit writes in M2 remain mandatory** regardless — only the read-side UI is cut.
