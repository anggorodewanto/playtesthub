# playtesthub

> Self-hosted, MIT-licensed [AccelByte Gaming Services (AGS) Extend](https://docs.accelbyte.io/gaming-services/modules/foundations/extend/) application for running **closed game playtests** end-to-end.

Players apply for a slot, click-accept the NDA, get a code (Steam key or AGS Campaign), play, and fill out a survey. Admins curate signups, manage the code pool, and review structured feedback from inside the AGS Admin Portal.

Built for indie and mid-size studios that already use AGS and need tenant-isolated playtest tooling they can own, audit, and self-host inside their own namespace — without rolling the same signup → NDA → key → feedback plumbing every release.

![playtesthub end-to-end walkthrough — admin creates a playtest, player signs up, admin approves, code is granted, player submits a survey response, admin reviews the audit log](docs/images/walkthrough.gif)

## What's in the box

- **Two distribution models** per playtest — `STEAM_KEYS` (CSV passthrough, manual Steam redemption) and `AGS_CAMPAIGN` (in-game redemption via the AGS Platform Campaign API). One internal code pool and state machine for both.
- **Discord-federated player identity** via AGS IAM's platform-token grant. Players sign in with Discord; the backend receives a real AGS user.
- **NDA versioning with forced re-acceptance** — edit the NDA mid-playtest and approved players must re-accept before submitting a survey response.
- **Discord DM delivery** of granted codes — FIFO worker queue with circuit breaker, manual retry, and restart-sweep semantics. Approval succeeds even if the DM fails; the code is also visible in the player UI. Requires a Discord server the bot and applicants both join (Discord blocks bot DMs without a mutual guild) — see [`docs/runbooks/setup-ags-discord.md` § 7 "Discord bot + server"](docs/runbooks/setup-ags-discord.md#7-discord-bot--server-required-for-dm-delivery).
- **Versioned typed surveys** (text, 1–5 rating, multi-choice) with per-version response splits.
- **Per-action audit log** for every admin mutation, stable JSONB shapes.
- **TDD-first** — unit, integration (testcontainers Postgres), e2e golden flow, and smoke harness (`pth` CLI). CI enforces every gate on every PR.

```mermaid
flowchart LR
  Player[Discord-authed player]
  Admin[AGS admin]
  subgraph "Extend Service Extension"
    GW["grpc-gateway HTTP/JSON"]
    SV["gRPC server\n(Go)"]
    PG[("Postgres\nExtend-managed")]
    GW --- SV --- PG
  end
  AppUI["Extend App UI\n(React, in AGS Admin Portal)"]
  Svelte["Player static bundle\n(GitHub Pages / Vercel)"]
  AGS["AGS Platform / IAM / Campaign"]
  Player --> Svelte --> GW
  Admin --> AppUI --> GW
  SV <--> AGS
```

> **Admin authorization.** Every admin RPC is gated on the built-in AGS IAM permission `ADMIN:NAMESPACE:{namespace}:EXTEND:APPUI` (the AppUI-admin perm) at a per-RPC action bit (CREATE / READ / UPDATE / DELETE) — held by namespace-admin roles like **Game Admin** and **Studio Admin** that studios already assign to admin staff. Studios authorize playtest admins by assigning one of those roles in the AGS Admin Portal; **no custom role creation required**, which is what makes this work on Shared Cloud (game admins cannot assign `CUSTOM:*` perms there). The `AuditLog` provides per-action attribution. See [PRD §6 AuthZ](docs/PRD.md#security) and [PRD §9 R8](docs/PRD.md).

## Status

**v1.0.0 shipped (MIT).** Track progress in [`docs/STATUS.md`](docs/STATUS.md). Sources of truth, in order:

| Doc | What it owns |
| --- | --- |
| [`docs/PRD.md`](docs/PRD.md) | Behavior. Authoritative if anything else disagrees. |
| [`docs/schema.md`](docs/schema.md) | DB schemas, audit-log enum + JSONB shapes, fenced-finalize SQL. |
| [`docs/errors.md`](docs/errors.md) | Byte-exact gRPC error codes / messages. |
| [`docs/architecture.md`](docs/architecture.md) | Stack + external dependency detail. |
| [`docs/engineering.md`](docs/engineering.md) | Repo layout, test strategy, TDD workflow, CI gates. |
| [`docs/cli.md`](docs/cli.md) | `pth` CLI spec — surface for humans + AI to drive the app end-to-end. |
| [`docs/dm-queue.md`](docs/dm-queue.md) | DM worker FIFO, circuit breaker, restart sweep. |
| [`docs/ags-failure-modes.md`](docs/ags-failure-modes.md) | AGS retry policy, cleanup matrix, M2 sub-cap rules. |
| [`docs/game-integration.md`](docs/game-integration.md) | Bridging playtesthub's Discord-headless AGS user to the game's Steam-headless AGS user. |

## Quick start

### Prerequisites

- Linux / macOS / WSL2; Bash; Docker 23+; Go 1.25; Node 22+; `protoc`, `grpcurl`, `jq`, `curl`.
- An AGS namespace and a confidential IAM client (PRD §5.9). [`docs/runbooks/setup-ags-discord.md`](docs/runbooks/setup-ags-discord.md) walks the AGS-side setup.

### Boot the backend

```bash
git clone https://github.com/anggorodewanto/playtesthub.git
cd playtesthub

cp .env.template .env
# Fill AGS_BASE_URL, AGS_NAMESPACE, AGS_IAM_CLIENT_ID, AGS_IAM_CLIENT_SECRET,
# DISCORD_BOT_TOKEN. DATABASE_URL + BASE_PATH already have local-dev defaults.

docker compose up --build       # backend + Postgres
```

Smoke check:

```bash
./scripts/smoke/boot.sh         # ephemeral PG + backend boot + reflection probe
```

### Reproduce the golden flow

The `pth` CLI is the canonical end-to-end harness — same surface a human or AI uses to drive the system, and the same path the e2e test exercises. The composite command **`pth flow golden-m3`** runs the full M3 golden flow (admin creates an NDA-required STEAM_KEYS playtest → publishes → player signs up → accepts the NDA → admin uploads keys → admin approves → player retrieves the granted code → admin authors a survey → player submits a response → admin lists responses) and emits one NDJSON line per step.

```bash
go build -o pth ./cmd/pth

# Profile A — admin (used to create + publish the playtest, upload keys, approve, author survey, list responses).
export PTH_AGS_BASE_URL=https://your-namespace.gamingservices.accelbyte.io
export PTH_IAM_CLIENT_ID=<confidential-iam-client-id>
export PTH_IAM_CLIENT_SECRET=<confidential-iam-client-secret>
export PTH_BACKEND=localhost:6565

./pth --profile admin auth login --password \
  --namespace your-namespace --username admin@example.com

# Profile B — player (created on the fly via the AGS test-user-group endpoint).
read -r USER_ID USERNAME PASSWORD < <(./pth user create --json | jq -r '[.userId,.username,.password] | @tsv')
echo "$PASSWORD" | ./pth --profile "player-$USER_ID" user login-as \
  --user-id "$USER_ID" --username "$USERNAME" --password-stdin

# Drive the golden flow. --codes-count synthesises a STEAM key for the upload step;
# pass --codes-file <path> to supply a real CSV instead. The flow seeds a TEXT +
# RATING survey question pair inline — no --from path needed.
./pth flow golden-m3 \
  --slug "demo-$(date +%s)" \
  --admin-profile admin \
  --player-profile "player-$USER_ID"
```

Expected output — ten NDJSON lines, all `status=OK`:

```json
{"step":"create-playtest","status":"OK","response":{"playtest":{"id":"…","status":"PLAYTEST_STATUS_DRAFT","nda_required":true}}}
{"step":"transition-open","status":"OK","response":{"playtest":{"status":"PLAYTEST_STATUS_OPEN"}}}
{"step":"signup","status":"OK","response":{"applicant":{"id":"…","status":"APPLICANT_STATUS_PENDING"}}}
{"step":"accept-nda","status":"OK","response":{"acceptance":{"nda_version_hash":"…"}}}
{"step":"upload-codes","status":"OK","response":{"accepted":1,"rejected":0}}
{"step":"approve","status":"OK","response":{"applicant":{"status":"APPLICANT_STATUS_APPROVED"}}}
{"step":"get-code","status":"OK","response":{"value":"GOLDEN-M2-DEMO-…","distribution_model":"DISTRIBUTION_MODEL_STEAM_KEYS"}}
{"step":"create-survey","status":"OK","response":{"survey":{"id":"…","version":1,"questions":[{"id":"…","type":"SURVEY_QUESTION_TYPE_TEXT"},{"id":"…","type":"SURVEY_QUESTION_TYPE_RATING"}]}}}
{"step":"submit-response","status":"OK","response":{"response":{"id":"…","submitted_at":"…"}}}
{"step":"list-responses","status":"OK","response":{"responses":[{"id":"…","answers":[…]}]}}
```

Tear down:

```bash
./pth playtest delete --slug "demo-..." --yes
./pth user delete --user-id "$USER_ID" --yes
```

The tests under [`e2e/golden_m1_test.go`](e2e/golden_m1_test.go), [`e2e/golden_m2_test.go`](e2e/golden_m2_test.go), [`e2e/golden_m3_test.go`](e2e/golden_m3_test.go), and [`e2e/golden_m4_test.go`](e2e/golden_m4_test.go) wrap the same sequences behind `go test ./e2e/...` for CI / operator verification — see [`docs/cli.md` §7.4](docs/cli.md).

**Window enforcement (M4)** — `Playtest.startsAt` / `Playtest.endsAt` are no longer display-only. A background `internal/window/` worker auto-transitions `DRAFT → OPEN` at `startsAt` and `OPEN → CLOSED` at `endsAt` (PRD §5.1, [`docs/STATUS_M4.md`](docs/STATUS_M4.md)). `pth flow golden-m4 --slug e2e-m4 --admin-profile admin` exercises the path: create with a window, await the auto-open + auto-close, assert two system-attributed `playtest.status_transition` audit rows.

**Auto-approve (M5.A)** — set `autoApprove=true` + `autoApproveLimit=N` (1..100,000) on a playtest and the first `N` signups land `APPROVED` straight from the signup handler, skipping the manual triage queue (PRD §5.4, [`docs/STATUS_M5.md`](docs/STATUS_M5.md)). Distribution-model-agnostic — works for STEAM_KEYS and AGS_CAMPAIGN today, will work for ADT under Track B. The cap bounds **auto-approvals only**; manual `ApproveApplicant` against PENDING applicants stays uncapped. Pool-empty during a burst is a silent PENDING fallback — the signup still succeeds and the operator restocks (or manually approves) at leisure; no applicant-visible error. `pth flow golden-m2 --slug "auto-$(date +%s)" --admin-profile admin --player-profile "player-$USER_ID" --auto-approve --auto-approve-limit 5` runs the variant: same seven NDJSON lines, but `upload-codes` is hoisted before `signup` (auto-approve consumes from the pool inside the signup tx) and the manual `approve` step is replaced by `assert-applicant-auto-approved`, which calls `ListApplicants` and pins `status=APPROVED` + `auto_approved=true` on the just-signed-up row.

## Architecture at a glance

- **Backend** — Go, gRPC + grpc-gateway in-process, Postgres (Extend-managed), `pgx` driver, `golang-migrate` migrations, `accelbyte-go-sdk` for IAM + Platform.
- **Player frontend** — Svelte 5 + Vite + Tailwind v4, static bundle, hash router. Discord login via AGS's platform-token grant ([`docs/engineering.md` §"Discord federation via platform-token grant"](docs/engineering.md)).
- **Admin frontend** — React 19 + TypeScript Extend App UI bundled as a Module Federation remote, hosted by AccelByte and rendered inside the AGS Admin Portal. Currently Internal-Shared-Cloud only ([PRD §9 R11](docs/PRD.md)).
- **CLI (`pth`)** — Go binary, talks gRPC directly on `:6565`. Authoritative end-to-end harness. Spec in [`docs/cli.md`](docs/cli.md).

Repo layout is documented in [`docs/engineering.md` §2](docs/engineering.md#2-repo-layout).

## Development workflow

This repo is **TDD-first**. Every production change follows red → green → refactor:

1. Write a failing test that names the behavior.
2. Write the minimum code to pass.
3. Refactor with tests green.

Smoke harness lands with the code that introduces it — see [`CLAUDE.md`](CLAUDE.md) and [`docs/engineering.md` §4](docs/engineering.md#4-redgreen-tdd-loop).

### Verification before committing

```bash
go test ./...                           # unit + integration (testcontainers-postgres)
golangci-lint run
buf lint
./proto.sh && git diff --exit-code      # proto stubs in sync
./scripts/smoke/boot.sh                 # backend boots + RPCs reach handlers
./pth flow golden-m3 ...                # canonical e2e (once env is configured)

# Frontend
( cd player && npm test && npm run build )
( cd admin  && npm test && npm run build )
```

CI runs the same gates on every PR — see [`.github/workflows/ci.yml`](.github/workflows/ci.yml). Browser-based a11y (`@axe-core/playwright` per [`docs/engineering.md` §5](docs/engineering.md#5-ci-gates)) is tracked under STATUS phase 12.1.

## Deploy

Three deployable surfaces. Each has its own host and its own runbook.

1. **Backend (Extend Service Extension)** — Go binary + Postgres on AGS Extend.
   1. Create the Extend Service Extension app in the AGS Admin Portal. Set the env vars and secrets per [PRD §5.9](docs/PRD.md#59-runtime-configuration-go-backend) — including `CORS_ALLOWED_ORIGINS` if the player will be hosted off-origin.
   2. Build + push with [`extend-helper-cli`](https://github.com/AccelByte/extend-helper-cli):
      ```bash
      extend-helper-cli image-upload --login \
        --namespace <namespace> --app <app-name> --image-tag v0.0.1
      ```
   3. Deploy the pushed image from **App Detail → Image Version History → Deploy**, or via `extend-helper-cli deploy-app --wait`.
2. **Player frontend (Svelte → GitHub Pages)** — static bundle, hash-routed, Discord-federated. Auto-deploys on push to `main` via [`.github/workflows/pages.yml`](.github/workflows/pages.yml). Setup is one-time per fork — enable Pages with the workflow build source, set three repo Variables, allowlist the Pages origin in the backend's `CORS_ALLOWED_ORIGINS`, register the Pages callback URL with Discord + AGS. Walk-through in [`docs/runbooks/deploy-player-pages.md`](docs/runbooks/deploy-player-pages.md). Vercel + custom-domain variants are noted in that runbook's § Out of scope.
3. **Admin UI (Extend App UI)** — React Module Federation remote hosted by AccelByte. `extend-helper-cli appui create` + `appui upload` (Internal Shared Cloud only today — see [`docs/engineering.md` §8](docs/engineering.md#8-temporary-ags-platform-workarounds)).

For first-time AGS + Discord setup (IAM client, platform credential, redirect URIs), follow [`docs/runbooks/setup-ags-discord.md`](docs/runbooks/setup-ags-discord.md) before any of the above.

## Integrating with your game

playtesthub identifies players by their **Discord-federated** AGS user; the game probably identifies them by their **Steam-federated** AGS user. AGS IAM treats those as two separate headless accounts, so the same human ends up with two different AGS userIds unless the integration explicitly bridges them. [`docs/game-integration.md`](docs/game-integration.md) covers the four patterns a game team can pick from — recommended path is a one-time Discord-OAuth gate on first launch that links Steam onto the playtesthub-side account.

## Contributing

Issues and PRs welcome. Before opening one:

- Read [`CLAUDE.md`](CLAUDE.md) and [`docs/engineering.md`](docs/engineering.md) — they encode the conventions CI enforces.
- Add a failing test before the fix. PRs that change behavior without a test will be sent back.
- For PRD-shaping proposals, file an issue first; the PRD is authoritative and changes there gate everything else.

## License

MIT — see [`LICENSE`](LICENSE).
