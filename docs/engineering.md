# playtesthub — Engineering

Build-side conventions that don't belong in the PRD: repo layout, test strategy, TDD workflow, CI gates, mocking policy. The PRD is authoritative for product behavior; this document is authoritative for *how we build it*.

Referenced from `CLAUDE.md` and `STATUS.md`.

---

## 1. Base templates

Two templates, one per deployable.

### 1.1 Backend — [`AccelByte/extend-service-extension-go`](https://github.com/AccelByte/extend-service-extension-go)

**Not forked** — cloned as a fresh standalone repo under the maintainer's GitHub account. Sequence:

```
git clone https://github.com/AccelByte/extend-service-extension-go.git playtesthub
cd playtesthub
rm -rf .git
git init
# update go.mod module path to github.com/anggorodewanto/playtesthub
# first commit = template snapshot; subsequent commits = our work
git remote add origin git@github.com:anggorodewanto/playtesthub.git
```

No upstream remote. If the template publishes a notable fix, cherry-pick it manually — we own the code from commit zero.

Updating `go.mod`'s module path (and every internal import that references it) is part of the scaffold step; a stale `github.com/AccelByte/...` import path will compile but marks the repo as a fork in disguise.

It gives us:

- gRPC server on `:6565` + grpc-gateway REST proxy on `:8000/<BASE_PATH>` in the same process.
- IAM auth interceptor (`pkg/common/authServerInterceptor.go`) that validates AGS IAM JWTs via the AccelByte Go SDK and enforces per-method permissions declared through proto options.
- Prometheus `:8080`, OpenTelemetry/Zipkin wiring, structured logging.
- `Dockerfile`, `docker-compose.yaml`, `Makefile`, `proto.sh`, `buf` + `protoc-gen-openapiv2` config.
- **OpenAPI spec served at `/apidocs/api.json`** — this is what `@accelbyte/codegen` in the admin UI consumes. Every RPC needs `google.api.http` annotations so the Swagger output is complete.

What the template does **not** give us (we add):

- Persistence — template uses AGS CloudSave; we replace it with Postgres (`pgx` + `golang-migrate`). PRD §5.9 mandates Extend-managed Postgres.
- Tests — template ships zero `_test.go` files. We add a full unit + integration suite (see §3).
- DM worker, reclaim job, leader election, Discord client.


### 1.2 Admin UI — `AccelByte/extend-app-ui-templates` (canonical post-GA) / `tryajitiono-ab/test-admin-ui` (playtest-stage mirror)

Scaffolded via `extend-helper-cli clone-template --scenario "Extend App UI" --template react -d admin/`. Relevant template: **`templates/react`** (single Extend app). It gives us:

- React 19 + TypeScript + Vite + `@module-federation/vite` Module Federation remote. `vite.config.ts` exposes `./src/mf-entry.ts` as `remoteEntry.js`; the Admin Portal host loads it and calls the exported `mount(container, hostContext)` (`AppUIModule` contract from `@accelbyte/sdk-extend-app-ui`).
- Ant Design v6 + Tailwind v4 (utilities prefixed `appui:` to avoid host CSS collisions).
- AccelByte JS SDK wiring (`@accelbyte/sdk`, `@accelbyte/sdk-iam`, `@accelbyte/validator`) — token lifecycle is handled for us.
- `@accelbyte/codegen` + `abcodegen.config.ts` — reads `swaggers.json` (tuple form `[serviceName, aliasName, swaggerFileOutput, swaggerURL]`), downloads from `<service_url>/apidocs/api.json`, emits typed endpoint classes + `@tanstack/react-query` hooks into `src/playtesthubapi/`.
- `@accelbyte/sdk-extend-app-ui/plugins`' `devProxyPlugin` — proxies `/ext-<namespace>-<app>` to AGS with auth attached so `npm run dev` talks to a real backend without CORS or token wiring.
- `main.tsx` dev bootstrap that fabricates a `HostContext` from `VITE_AB_*` env vars so the bundle runs standalone outside the Admin Portal for local work.

What the admin template does **not** give us (we add):

- Tests — no Vitest / RTL / Playwright / Storybook config in any template's `package.json`, no `__tests__` / `*.spec.ts`. We add **Vitest + React Testing Library** for unit/component tests and reuse the player app's Playwright harness for admin e2e smoke.
- Any playtesthub-specific UI — the template content (tournaments in the reference example) is placeholder; we delete it and build our five pages (PRD §5.7) against the codegen'd `usePlaytesthubServiceApi_*` hooks.

**Availability caveat**: Extend App UI is an **experimental AGS capability** available in **Internal Shared Cloud only** at MVP time (PRD §9 R11). Treat this as a hard constraint for demo/self-host until GA.

---

## 2. Repo layout

Target layout after M1 scaffolding lands. Flat at the top (matches the template; no `cmd/`):

Tooling note: Go stub + OpenAPI codegen runs via `proto.sh` (protoc) — inherited from the template and retained. `buf.yaml` drives `buf lint` only; there is no `buf.gen.yaml`.

```
playtesthub/
├── main.go                          # entrypoint (template convention)
├── Dockerfile
├── docker-compose.yaml              # local dev: postgres + backend
├── Makefile
├── buf.yaml                         # lint config only — codegen is protoc via proto.sh
├── proto.sh                         # protoc codegen driver (Go stubs + OpenAPI)
├── .golangci.yml
├── go.mod / go.sum
├── migrations/                      # golang-migrate SQL files; append-only
│   ├── 0001_init.up.sql
│   └── 0001_init.down.sql
├── proto/
│   └── playtesthub/v1/
│       ├── playtesthub.proto        # RPCs from PRD §4.7
│       └── permission.proto         # AGS permission annotations
├── pkg/
│   ├── pb/                          # generated gRPC + grpc-gateway stubs (checked in)
│   ├── common/                      # interceptors, gateway setup, logging, tracing
│   ├── service/                     # gRPC handler implementations, one file per domain
│   │   ├── playtest.go
│   │   ├── applicant.go
│   │   ├── code.go
│   │   ├── survey.go
│   │   └── *_test.go                # handler-level tests (mocked repos)
│   ├── repo/                        # Postgres repositories (real SQL, no ORM)
│   │   ├── playtest.go
│   │   ├── applicant.go
│   │   ├── code.go
│   │   ├── survey.go
│   │   ├── auditlog.go
│   │   ├── leader.go
│   │   ├── txrunner.go              # InTx(ctx, fn) wrapper — lets services chain repo calls in one tx without leaking pgx
│   │   └── *_test.go                # integration tests against testcontainers-postgres
│   ├── ags/                         # AGS Platform / Campaign API client
│   ├── iam/                         # IAM JWT validation wrapper around the AGS SDK
│   ├── discord/                     # Discord bot client (handle lookup + DM send)
│   ├── dmqueue/                     # in-memory FIFO, circuit breaker, restart sweep
│   └── config/                      # env-var parsing, defaults
├── internal/                        # process-internal subsystems (not importable by other repos)
│   ├── bootapp/                     # gRPC server construction shared by main.go and e2e suite
│   └── reclaim/                     # leader-elected reclaim worker
├── cmd/
│   └── pth/                         # CLI binary; see docs/cli.md
├── e2e/                             # top-level e2e suite — boots bootapp + testcontainers-postgres, drives `pth`
├── scripts/
│   ├── smoke/                       # bash smoke harness — see §5.1
│   └── loadtest/                    # perf proof-point harness (PRD §6 / §7)
├── admin/                           # Extend App UI (React 19 + Vite + Module Federation remote)
│   ├── package.json                 # scripts: dev, build, codegen, cg:download, cg:clean-and-generate, test, lint
│   ├── vite.config.ts               # @module-federation/vite + @tailwindcss/vite + devProxyPlugin
│   ├── abcodegen.config.ts          # @accelbyte/codegen config (basePath '', overrideAsAny, etc.)
│   ├── swaggers.json                # tuple list; points at <service>/apidocs/api.json
│   ├── vitest.config.ts             # we add this — template ships no test runner
│   └── src/
│       ├── mf-entry.ts              # MF entrypoint; imports Tailwind
│       ├── module.tsx               # exports mount(container, hostContext) — AppUIModule contract
│       ├── federated-element.tsx    # AppUIContextProvider wrapper
│       ├── main.tsx                 # standalone dev bootstrap (fabricates HostContext from VITE_AB_* env)
│       ├── playtesthubapi/          # generated — DO NOT EDIT; regen via `npm run codegen`
│       ├── pages/                   # our five admin pages (PRD §5.7)
│       └── components/
├── player/                          # Svelte static app (self-hosted, GitHub Pages / Vercel)
│   ├── public/config.json.example
│   └── src/
├── docs/                            # existing — PRD, schema, etc.
├── CLAUDE.md
└── README.md
```

Rationale for the boundaries:

- **`pkg/service` vs `pkg/repo`** — service layer is the only thing the gRPC server wires up. Handlers depend on repository interfaces, not concrete types. This is what makes handler-level unit tests fast (mocked repos) while repo tests exercise real SQL.
- **`pkg/ags`, `pkg/iam`, `pkg/discord`** — one package per external boundary. Each exposes a narrow interface and hides the SDK behind it. Tests mock at the interface; we never mock the SDK types directly.
- **`pkg/dmqueue`, `internal/reclaim`** — background subsystems with their own tests. They receive collaborators (repo, DM client) via interface injection.
- **`internal/bootapp`** — single source of truth for gRPC server construction (interceptors + service registration + reflection + health + Prometheus). Both `main.go` and the `e2e/` suite call `bootapp.New(...)` so they wire identical servers; e2e gets a free port + an in-process server without duplicating handler-registration logic. main.go owns what e2e doesn't need (grpc-gateway HTTP server, OTEL tracer, swagger UI, signal handling, the reclaim/DM workers).
- **`pkg/repo/txrunner.go`** — `TxRunner` interface + `PgTxRunner` wrap `*pgxpool.Pool`. Services that need to chain repo calls inside one transaction (approve flow: `Reserve → FencedFinalize → ApproveCAS`; AGS_CAMPAIGN initial-create: `playtest.CreateTx → code.BulkInsertGeneratedTx`) call `txRunner.InTx(ctx, fn)`; the runner commits on nil error and rolls back otherwise. Repo methods that need to participate take a `repo.Querier` (satisfied by both `*pgxpool.Pool` and `pgx.Tx`) so a single method works in or out of a tx without leaking pgx into `pkg/service`.
- **`internal/`** is the drop zone for code we don't want any future Go consumer to import. `pkg/` is the public Go-import surface (still an internal-by-convention boundary, but exportable if a consumer ever appears).

---

## 3. Testing strategy

Four test layers, matched to the boundary they exercise. Every PR adds tests at the layer that changed.

### 3.1 Unit tests — `pkg/service/*_test.go`

- Target: gRPC handler logic — authz, validation, orchestration, error-to-gRPC-status mapping.
- Dependencies: **mocked** repositories, AGS client, IAM validator, Discord client, DM queue. Mocks generated with `go.uber.org/mock` (already a transitive dep via the template).
- Database: none.
- Speed: sub-second; run on every save.
- What to assert:
  - the exact gRPC code + message for every error row in `docs/errors.md`;
  - every audit-log write (shape + enum value per `docs/schema.md`);
  - happy-path ordering of collaborator calls only when the ordering matters (DB tx before DM enqueue, etc.).

### 3.2 Repository / integration tests — `pkg/repo/*_test.go`

- Target: SQL. Real Postgres. **No mocks at this layer** — the point of the test is the SQL behavior, not the Go code around it.
- Dependencies: `testcontainers-go` postgres module, spun up once per package (`TestMain`) and truncated between tests.
- Migrations: applied via `golang-migrate` against the testcontainer at boot.
- What to assert:
  - the fenced-finalize SQL (schema.md) — concurrent reserve/finalize scenarios including the 0-row path;
  - `UNIQUE(playtestId, value)` on Code; unique `(userId, playtestId, ndaVersionHash)` on NDAAcceptance; unique `(playtestId, userId)` on SurveyResponse;
  - slug uniqueness across soft-deleted rows;
  - `pg_advisory_xact_lock` serialization on CSV upload and AGS top-up;
  - audit-log JSONB payloads round-trip.

### 3.3 End-to-end — small set, `e2e/*_test.go` (top-level)

- Target: the full gRPC server wired in-process, real Postgres, faked external boundaries (IAM/AGS/Discord via the interface mocks from §3.1).
- Speed: a few seconds; not run on every save. CI always runs them.
- Scope: the golden flow (PRD §4.1) per milestone, plus one or two critical concurrency scenarios (two admins approve the same applicant; approve racing reclaim).

### 3.4 Frontend tests

- **Player Svelte**: Playwright smoke for the golden flow against a spun-up backend; `@axe-core/playwright` a11y gate on the five pages listed in PRD §6 Accessibility.
- **Admin Extend App UI**: **Vitest + React Testing Library** for component + page unit tests (mock the codegen'd react-query hooks at module boundary — never hand-roll fetch mocks). Playwright for one golden-path admin smoke (create playtest → approve an applicant → see status flip) run against the standalone dev bootstrap (`main.tsx`) with a seeded Postgres. The template ships no test runner; we bring our own. No a11y CI gate (PRD §6 — admin UI excluded).
- **Generated `playtesthubapi/` is not tested directly.** Trust the codegen; assert its output is current via the "codegen fresh" CI gate (§5) instead.

### 3.5 What we do not test

- AGS SDK internals — trust the SDK.
- `pgx` internals — trust the driver.
- Generated pb code.
- Logging output — unless a specific redaction rule is a contract (NDA text, survey free-text, `Code.value` absence in logs — these **are** asserted, at the service-unit layer, via a log-capture hook).

---

## 4. Red–green TDD loop

The enforced rhythm for every production change. Violating this is how greenfield codebases accrue untested code that no one dares to refactor.

1. **Name the behavior.** One sentence. "Approving a REJECTED applicant returns `FailedPrecondition` with `applicant is rejected and cannot be re-approved`."
2. **Write the test** at the right layer (§3). Run it. **Confirm it fails for the reason you expect** — not because of a typo, not because a collaborator is nil. A red test that's red for the wrong reason is worse than no test.
3. **Write the minimum code** to turn it green. If the implementation feels like it needs five more helpers, stop and write five more tests first.
4. **Refactor** with the suite green. Extract only what has two call sites now, not what might have three later.
5. **Commit** the red, green, and refactor steps as you go — or squash into a single commit if the sequence is noisy. Either is fine; what matters is the test landed with the code.

Anti-patterns to catch yourself on:

- Tests written after the code to make a green CI — the test may not fail for the right reason.
- Tests that mock the function under test (pseudo-tautology).
- Giant setup blocks — usually signals the unit is too wide; narrow the boundary.
- Skipping the red step "because it's obvious" — the red step is where you catch a misunderstanding before writing 200 lines against it.

---

## 5. CI gates

Every PR must pass, in a single GitHub Actions workflow:

| Gate | Tool | Notes |
| --- | --- | --- |
| Go lint | `golangci-lint run` | config in `.golangci.yml`; includes `errcheck`, `govet`, `staticcheck`, `gofmt`. |
| Go unit + integration | `go test ./...` | testcontainers-postgres spins up in CI; Docker-in-Docker required. |
| Proto lint | `buf lint` | lint-only; `buf.yaml` at repo root, no `buf.gen.yaml`. |
| Proto stubs fresh | `./proto.sh` + `git diff --exit-code` | protoc codegen driver; forces checked-in stubs under `pkg/pb/` and `gateway/apidocs/` to match `.proto`. |
| Svelte build | `npm run build` in `player/` | |
| Svelte a11y | `@axe-core/playwright`, pinned | five player pages; zero critical violations; scoped to `wcag2a, wcag2aa, wcag21a, wcag21aa`. |
| Admin codegen fresh | `npm run codegen` + `git diff --exit-code` in `admin/` | catches a backend proto/HTTP-annotation change that the admin UI hasn't regenerated against. |
| Admin build | `npm run build` in `admin/` (`tsc -b && vite build`) | catches type errors and MF bundle-build failures before deploy. |
| Admin unit | `npm run test` in `admin/` (Vitest) | |
| Migrations apply | `migrate up` against ephemeral Postgres | catches forward-only violations. |

Perf proof point (500 signups / 10 min, p95 < 3s) is **not** a CI gate — reported per-release in `CHANGELOG.md` from `scripts/loadtest/` (PRD §6 / §7).

### 5.1 Smoke harness (`scripts/smoke/`)

Bash + `grpcurl` + `curl` scripts that exercise the real binary end-to-end. They catch the class of failure unit + integration tests miss by construction: the types compile, the handlers register, but the *binary* doesn't actually boot or route. Every phase that adds user-visible behavior extends the relevant script in the same PR (CLAUDE.md "smoke harness lands with the code that introduces it").

| Script | Target | When to extend |
| --- | --- | --- |
| `pth.sh` (**`make smoke` — authoritative**) | `cmd/pth/` binary against an ephemeral local backend (boots its own postgres + auth-disabled service on `:6565`). Asserts CLI-side flag parsing, dry-run JSON shapes, exit-code mapping, the `describe` golden diff, and (gated on `PTH_E2E_*` env) live login → flow round-trips. Supersedes `boot.sh` for the standard dev loop because every backend-reachability check has a CLI-shaped equivalent here. | Any new `pth` subcommand (add a dry-run probe + a registry/describe entry). Any new RPC reachable through a `pth` wrapper. |
| `boot.sh` (`make smoke-boot` — fallback) | Local binary against ephemeral testcontainers-postgres on `:6565`. Asserts: migrations apply, gRPC reflection lists every RPC, `/apidocs/api.json` served, every handler reachable (auth-required RPCs return `Unauthenticated`, never `Unimplemented`), background workers (reclaim, DM queue) emit their startup log lines. | Backend-only changes with no `pth` surface (a new background goroutine, a new internal startup log line). New RPCs may pick up a `boot.sh` reflection probe in addition to their `pth.sh` probe when worth the redundancy. |
| `cloud.sh` | Deployed backend over the public grpc-gateway HTTPS surface. Asserts each RPC the gateway routes responds 401 / its representative success/error code. | Any new RPC, any new HTTP path. The deployed gateway is the only place to catch gateway-route registration bugs and cookie-auth interceptor regressions. |
| `player-build.sh` | `player/` Vite build. Runs `npm install && npm run test && npm run build`; asserts bundle shape. | Any change to the Svelte build config or routing surface. |
| `admin-build.sh` | `admin/` Vite build. Reseeds `swaggers/playtesthub.json` from `gateway/apidocs/api.swagger.json`, runs `cg:clean-and-generate` → eslint → vitest → `vite build`, asserts `dist/remoteEntry.js` + chunks. The codegen-from-local-swagger seed makes this script independent of any deployed AGS namespace, which is why it can run on a clean checkout. | Any change to admin pages, codegen config, or react-query hooks consumed by the UI. |
| `env.sh` | Sources the env-var contract (`PTH_*` for the CLI smoke, `LEADER_*`/`RESERVATION_*` for fast reclaim ticks) so individual probes don't redeclare them. | Any new env var the smoke flow needs. |

The CLI's `pth flow golden-m1` / `pth flow golden-m2` (composite NDJSON flows) are the canonical e2e checks; smoke probes catch wire-routing failures that flows would surface only via cryptic mid-run errors. Both layers stay.

---

## 6. Mocking policy

One rule: **mock at package boundaries we own, not at types we don't.**

- Own: `repo.PlaytestStore`, `ags.CampaignClient`, `iam.Validator`, `discord.Client`, `dmqueue.Enqueuer`. Define the interface in the owning package; generate mocks with `go.uber.org/mock` into a sibling `mocks/` sub-package.
- Don't mock: `*pgx.Conn`, `*sdk.Justice...Service`, `*http.Client`. Wrap them behind one of our interfaces first.

If a test needs a time source, inject a `clock.Clock` (use `benbjohnson/clock` or a tiny internal one). Do not call `time.Now()` inside production code that a test then tries to pin.

---

## 7. Local dev

### Backend
- `docker-compose up` → Postgres on `:5432`. Backend is run locally with `go run .` so breakpoints work.
- `make proto` → regenerate gRPC + grpc-gateway stubs and the OpenAPI spec under `gateway/apidocs/` (runs `proto.sh` — protoc).
- `make lint-proto` → `buf lint` against the `proto/` tree.
- `make test` → unit + integration; assumes Docker is running.
- `make lint` → `golangci-lint run`.
- **SDK adapter activation** (M2 phase 8.1): when `PLUGIN_GRPC_SERVER_AUTH_ENABLED=true`, bootapp wires the SDK-backed AGS adapter (`pkg/ags.SDKClient`) and `CreatePlaytest` provisions a real Item + Campaign + codes; with auth disabled, bootapp falls back to `pkg/ags.MemClient` so dev/e2e boots stay offline. The boot-time log line `ags client: SDK-backed` vs `ags client: in-memory` confirms which branch is live. `AGS_STORE_ID` is **optional** — phase 16 lazily auto-discovers / auto-creates a store on the first AGS_CAMPAIGN create.

#### AGS namespace prerequisites (auto-bootstrapped, M2 phase 16)

`pkg/ags.SDKClient.Bootstrap` runs once per process before the first `AGS_CAMPAIGN` `CreatePlaytest` and lazily provisions every prereq listed below. Each step treats HTTP 409 conflict as success, so a racing operator manually pre-creating the same resource is a no-op. STEAM_KEYS playtests skip Bootstrap entirely (no AGS dependencies).

| Resource | Auto-bootstrap behavior | Operator override |
| --- | --- | --- |
| **Store** | `ListStores` first; reuses any existing store. Empty namespace → `CreateStore` with `title="playtesthub"`, `defaultRegion=US`, `defaultLanguage=en`. Resolved id is cached on the SDKClient. | Set `AGS_STORE_ID` to pin a specific store (skips discovery). |
| **Category `/playtesthub`** | `GetCategory` first; 404 → `CreateCategory` with `localizationDisplayNames["en"]="Playtesthub"`. | None — the path is hardcoded in `SDKClient.CreateItem` so item rows stay isolated. |
| **Currency** (for `RegionData`) | `ListCurrencies` first; reuses any existing `VIRTUAL` entry. No VIRTUAL → `CreateCurrency` with `currencyCode="PTHCOIN"`, `currencyType=VIRTUAL`, `decimals=0`. | Set `AGS_REGION_CURRENCY_CODE` (and optionally `AGS_REGION_CURRENCY_TYPE`, default `VIRTUAL`) to pin a specific currency. |
| **Region** | Hardcoded to `US` unless overridden. | `AGS_REGION_CODE` overrides the RegionData key. |

Bootstrap failure surfaces as a mapped gRPC status on the in-flight `CreatePlaytest`; the next `CreatePlaytest` retries Bootstrap (a transient AGS hiccup at boot does not wedge subsequent creates until restart). Successful Bootstrap emits `event=ags_bootstrap_ok`; failures emit `event=ags_bootstrap_failed` at WARN with the underlying error.

The legacy four-step pre-deploy walkthrough is no longer required, but operators who want explicit control can still pre-create resources via `POST /platform/admin/namespaces/{namespace}/{stores|categories|currencies}` and pin them via the env-var overrides above.

#### AGS_CAMPAIGN provisioning sequence + SDK gotchas

The order in `pkg/service/ags_campaign.go::createAGSCampaignPlaytest` is load-bearing — flipping any two steps will make AGS reject the create with a different error code. The order is:

1. **CreateCampaign** (empty REDEMPTION campaign). AGS auto-derives `boothName` (observed: `"C_<campaign-name>"`). The Go SDK call returns `CampaignInfo.BoothName`; `pkg/ags.SDKClient` surfaces it via `CreatedCampaign.BoothName`. Reusing the raw campaign name as the Item's BoothName fails with HTTP 404 / errorCode `37041` "Ticket booth [...] does not exist" because the lookup is byte-exact.
2. **CreateItem** with `BoothName = createdCampaign.BoothName` (NOT `spec.Name`). AGS validates the booth at create time, so the Campaign must exist first; reversing the order is impossible because `BoothName` is a required-at-create field on CODE items. Schema marks `boothName` `omitempty` but the runtime rejects null with HTTP 422 / errorCode `20002` for the field — treat it as required.
3. **LinkItemToCampaign** — `UpdateCampaign` with `Items=[{itemID, qty:1}]`. Without this, codes redeem nothing. AGS has no DELETE on campaigns; the cleanup matrix uses `UpdateCampaign Status=INACTIVE` instead.
4. **CreateCodes** in batches. The SDK's `CreateCodesShort` returns only `NumCreated`; `SDKClient.CreateCodes` then `QueryCodesShort` paginates by a unique batch name (`pth-<8-hex>`) to recover the values.

**Token plumbing gotcha**: `auth.RefreshTokenScheduler` in `accelbyte-go-sdk` is gated by a process-global `sync.Once`. Whichever `OAuth20Service.LoginClient` runs first claims the goroutine; subsequent `LoginClient` calls store a token but never schedule a refresher. The inbound auth surface in `main.go` runs first at boot, so the platform-side `TokenRepository` (the one Item/Campaign services consume) never auto-refreshes — calls 401 after ~1h. `pkg/ags.SDKClient` compensates with a one-shot **login-on-401 retry**: any outbound call that returns HTTP 401 triggers `Login()` (closure passed via `SDKClientOptions.Login`, wired to the same `LoginClient` call) and retries once. Never skip the `Login` field; without it, the platform side wedges as soon as its first token expires.

### Admin (Extend App UI, in `admin/`)
- `cp .env.local.example .env.local` once; fill in `VITE_AB_*` values pointing at your AGS namespace + deployed service extension. `extend-helper-cli appui setup-env` can populate these.
- `npm install`.
- `npm run codegen` → downloads `apidocs/api.json` from the running backend and regenerates `src/playtesthubapi/`. **Rerun every time proto HTTP annotations change.**
- `npm run dev` → Vite on `http://localhost:5173`; `devProxyPlugin` auto-proxies `/ext-<namespace>-<app>` to AGS with auth.
- `npm run build` → `tsc -b && vite build`. Output: `dist/`. **`BASE_URL` must be set at build time** — `vite.config.ts` bakes it into `mf-manifest.json` as `publicPath`, and the Admin Portal host loads `remoteEntry.js` from that absolute URL. Empty/wrong → `Failed to fetch dynamically imported module: …/remoteEntry.js`.
- **AppUI asset host = parent namespace, not game namespace**: CSM serves the bundle from `<parent>.internal.gamingservices.accelbyte.io/csm/v1/admin/namespaces/<game-ns>/files/app-ui/<name>/<version>/`. The game-namespace host (`<parent>-<game>.internal…`) returns `404 data not found: subdomain mismatch` for the same path. The dev `extend-helper-cli appui upload` log line "Asset Base URL: …" prints the *wrong* host — ignore it, use the parent host.
- Two-step deploy (pin a `$VERSION` so the `BASE_URL` path matches the upload path; bump `$VERSION` on every retry — CSM rejects re-upload with `GeneralError(20024): version already exists for this app UI`):
  ```bash
  VERSION=<short-tag>
  BASE_URL="https://${AB_PARENT}.internal.gamingservices.accelbyte.io/csm/v1/admin/namespaces/${AB_NAMESPACE}/files/app-ui/${AB_APPUI_NAME}/${VERSION}/" \
    npm run build
  extend-helper-cli appui upload --namespace $AB_NAMESPACE --name $AB_APPUI_NAME \
    --build-version $VERSION --no-build
  ```
- First-time registration only: `extend-helper-cli appui create --namespace $AB_NAMESPACE --name $AB_APPUI_NAME`.
- **Verify in the browser, not with `curl`.** A client_credentials / IAM-admin Bearer token has different CSM visibility than the Admin Portal session cookie, so a 404 from `curl -H "Authorization: Bearer ..."` against the parent-host URL doesn't mean the bundle is broken, and a 200 against the game-ns host doesn't mean it works. The only authoritative check is browser DevTools → Network in the live Admin Portal: both `mf-manifest.json` and `remoteEntry.js` should return 200 from the parent host. The cross-origin cookie that makes those requests succeed is only present in the actual Admin Portal session.

### Player (Svelte, in `player/`)
- `npm install && npm run dev` — Vite on `http://localhost:5173`.
- **Runtime config**: Vite serves `player/public/config.json` verbatim at `/config.json`. The loader (`src/lib/config.ts`) fetches it before anything else mounts and hard-fails per PRD §5.8 on any malformed branch. `public/config.json` is gitignored — copy `public/config.json.example` and fill in values for your target deploy.
- **Hitting a local backend (CORS-free)**: set `VITE_BACKEND_URL` in `player/.env`, point `config.json.grpcGatewayUrl` at the dev server's own origin + base path (default `http://localhost:5173/playtesthub`), and `vite.config.ts` will proxy that prefix to the backend. Default base path is `/playtesthub` (matches `BASE_PATH` on the backend); override with `VITE_BACKEND_BASE_PATH` if needed. Same-origin from the browser's perspective, no backend CORS required.

#### Player end-to-end demo against a local backend

The player app needs a running backend AND a seeded playtest row to render anything interesting. The Landing view is driven by the unauth `GetPublicPlaytest` RPC; an empty DB means the friendly "not available" message and nothing else. Full flow for a visual demo:

```bash
# 1. Fresh Postgres on a dedicated port (doesn't collide with smoke/boot.sh's :54399).
docker run -d --rm --name playtesthub-demo-pg \
  -e POSTGRES_USER=playtesthub -e POSTGRES_PASSWORD=playtesthub -e POSTGRES_DB=playtesthub \
  -p 54400:5432 postgres:16-alpine

# 2. Backend with auth disabled — skips Validator.Initialize + LoginClient,
#    so AGS_* env vars can be placeholders. BASE_PATH must still be set
#    because pkg/config hard-fails without it.
BASE_PATH=/playtesthub \
DATABASE_URL="postgres://playtesthub:playtesthub@localhost:54400/playtesthub?sslmode=disable" \
DISCORD_BOT_TOKEN=x AGS_IAM_CLIENT_ID=x AGS_IAM_CLIENT_SECRET=x \
AGS_BASE_URL="https://x.invalid" AGS_NAMESPACE=demo \
PLUGIN_GRPC_SERVER_AUTH_ENABLED=false \
  setsid go run . >/tmp/playtesthub-demo.log 2>&1 &

# Wait until migrations applied + the gateway is live:
until curl -sf http://localhost:8000/playtesthub/apidocs/api.json >/dev/null; do sleep 0.5; done

# 3. Seed a playtest directly via psql (admin RPCs still require auth even
#    with auth disabled — the handler rejects a nil actor). Use the same
#    column names as schema.md; distribution_model = 'STEAM_KEYS', status
#    = 'OPEN' so GetPublicPlaytest surfaces it.
docker exec -i playtesthub-demo-pg psql -U playtesthub -d playtesthub <<'SQL'
INSERT INTO playtest (namespace, slug, title, description, platforms, starts_at, ends_at, status, distribution_model)
VALUES ('demo', 'space-rogue-beta', 'Space Rogue — Closed Beta',
        'Welcome to the closed beta! Short description here.',
        ARRAY['STEAM','XBOX'], now(), now() + interval '14 days', 'OPEN', 'STEAM_KEYS');
SQL

# 4. Player app — dev server with proxy.
cat > player/public/config.json <<'JSON'
{
  "grpcGatewayUrl": "http://localhost:5173/playtesthub",
  "iamBaseUrl": "https://iam.demo.local.invalid",
  "discordClientId": "demo-client-id-not-for-real-login"
}
JSON
cat > player/.env <<'ENV'
VITE_BACKEND_URL=http://localhost:8000
VITE_BACKEND_BASE_PATH=/playtesthub
ENV

cd player && npm run dev
# Open http://localhost:5173/#/playtest/space-rogue-beta
```

With placeholder `iamBaseUrl` / `discordClientId` the Sign-up button redirects nowhere real. For a full Discord round-trip, use the ISC public IAM client from the next subsection. The Landing, the 404 route (any unknown slug), and the BootError screen (mangle `config.json` to see it) are all fully exercisable in this setup without IAM.

**Teardown**: `docker rm -f playtesthub-demo-pg`, `pkill -f 'go run \.'` (or kill the `setsid` process group), and Ctrl-C the Vite dev server.

`.env.example` files live alongside each deployable (`./.env.example`, `admin/.env.local.example`, `player/.env.example`). Actual `.env*` files are gitignored.

#### Discord federation via platform-token grant

The player needs an AGS IAM access token. AGS supports two paths to that token from a federated Discord identity:

1. **PKCE auth-code flow** (`/iam/v3/oauth/authorize` → `/iam/v3/oauth/platforms/discord/authorize` → `/iam/v3/oauth/token`). **Does not work on shared cloud for game namespaces** — AGS's `LoadAuthorize` step requires a Justice platform account for the federated user, but that record is only created by the platform-token-grant handler, never by the auth-code handler. STATUS.md M1 phase 9.2 documents the failed end-to-end attempt; debug trace confirms the failure is structural, not a config bug.
2. **Platform-token grant** (`POST /iam/v3/oauth/platforms/discord/token`). The player runs Discord OAuth directly (Discord developer portal owns the redirect-URI allowlist), then hands the resulting Discord auth code to our backend, which authenticates with confidential AGS credentials and exchanges it for AGS tokens in one round trip. AGS auto-creates the Justice platform account on first call. **This is the path we use.**

The flow:

| Step | Origin | URL | Purpose |
| --- | --- | --- | --- |
| 1 | Player → Discord | `https://discord.com/oauth2/authorize?response_type=code&client_id={DISCORD_CLIENT_ID}&redirect_uri={origin}/callback&scope=identify+email&state={state}` | Player navigates the browser. AGS IAM is **not** involved at this stage. The Discord developer portal's redirect-URI allowlist is the only allowlist that matters. |
| 2 | Discord → Player | `GET {origin}/callback?code=…&state=…` | Discord redirects with its own auth code. `bridgePathCallback` rewrites the path to `/#/callback?…` so the hash-router's existing route picks it up. |
| 3 | Player → Backend | `POST {grpcGatewayUrl}/v1/player/discord/exchange` body `{ "code": "...", "redirect_uri": "{origin}/callback" }` | `Player.ExchangeDiscordCode` RPC. Pre-auth (no JWT). The `redirect_uri` MUST byte-exactly match step 1; AGS forwards it to Discord, which re-validates. |
| 4 | Backend → AGS | `POST {AGS_BASE_URL}/iam/v3/oauth/platforms/discord/token` body `platform_token={code}&redirect_uri=…`, `Authorization: Basic base64({AGS_IAM_CLIENT_ID}:{AGS_IAM_CLIENT_SECRET})` | Server-side. Confidential auth. Single round trip. AGS auto-creates the Justice platform account on first call. |
| 5 | Backend → Player | RPC response `{ "accessToken": "…", "refreshToken": "…", "expiresIn": 3600, "tokenType": "Bearer" }` | Forwarded verbatim. Player stores `accessToken` in `sessionStorage` and Bearer-attaches it to subsequent player RPCs. |

Wired in:
- `pkg/service/discord_exchange.go` — RPC handler (step 4).
- `player/src/lib/auth.ts::buildDiscordAuthorizeUrl` — step 1 URL composition.
- `player/src/lib/auth.ts::exchangeDiscordCode` — step 3.
- `player/src/lib/bootstrap.ts::bridgePathCallback` — step 2 path-to-hash bridge.

##### Setup checklist

The prescriptive setup walkthrough — the screens to click, the values to paste, the verification ladder — lives in [`docs/runbooks/setup-ags-discord.md`](runbooks/setup-ags-discord.md). Follow that for a fresh tenant. The summary:

1. **Discord developer portal** — create app, add `${PLAYER_ORIGIN}/callback` to OAuth2 → Redirects.
2. **AGS Admin Portal → Login Methods → Platforms → Discord** — paste Discord client ID/secret, set `RedirectUri` byte-exact to `${PLAYER_ORIGIN}/callback`, `IsActive: true`. **The AGS-docs default `https://<ags-host>/iam/v3/platforms/discord/authenticate` is wrong for this flow** — see the runbook for why.
3. **AGS confidential IAM client** — same `AGS_IAM_CLIENT_ID` used for IAM JWT validation; needs `NAMESPACE:{namespace}:USER:LOGIN [CREATE]` (or the AGS-equivalent for Discord platform-token grant). If this is missing, AGS returns `unauthorized_client`.
4. **Player config** (`player/public/config.json`) — `discordClientId` is the **Discord** OAuth client ID, not an AGS IAM client. The phase-9.1-era public AGS IAM client is no longer used.

For ISC namespace `abtestdewa-pong`, the Discord client ID + secret are pre-configured. Local dev: `npm run dev` in `player/` plus the Vite proxy entry for `/ext-abtestdewa-pong-playtesthub`.

##### Path-vs-hash callback bridge

The router is hash-based (`#/callback`, `#/signup`, `#/pending`) for static-host compatibility, but Discord's OAuth redirect lands on the path `/callback` (Discord's allowlist matches byte-exactly and forbids fragments). `src/lib/bootstrap.ts::bridgePathCallback()` runs before the app mounts and rewrites `/callback?code=…` → `/#/callback?code=…` via `history.replaceState`. For deploys that serve the bundle under a subpath, adjust `bridgePathCallback` alongside the registered redirect URI.

##### Why the auth-code flow doesn't work

In game namespaces on shared cloud, AGS's `LoadAuthorize` (`pkg/oauth/model/jwtstore.go` in `justice-iam-service`) detects `isGameNamespace(publisher=foundations, ns={game-ns}) == true` and tries to look up the user's Justice platform account. That record is created by `handleUserPlatformTokenGrantV3` (the platform-token grant) but **never** by `platformAuthenticateV3Handler` (the auth-code grant). So PKCE auth-code completes Discord federation, AGS issues a code, and the next-step `/oauth/token` call always fails with `invalid_grant: failed to load authorize data: internal error user justice platform account not found`. The trace observed during 9.2 debugging: `7ca156dae6e6402f94599163a209273d` against `foundations-justice-internal` cluster, namespace `justice`, log_type `application`. There is no client-side workaround — moving the IAM client to the publisher namespace would skip the gate, but customers don't own `foundations` on shared cloud.

#### JWKS caching is owned by the AccelByte SDK

Every admin / player RPC calls `common.Validator.Validate(token, …)` from `pkg/common/authServerInterceptor.go`. The validator is the AccelByte Go SDK's `iam.TokenValidator` (`services-api/pkg/service/iam/auth_validator.go`); we construct it once at boot in `internal/bootapp/bootapp.go`, call `Initialize`, and reuse it for the lifetime of the process. The SDK already caches everything PRD §6 flagged:

- `Initialize` calls `fetchAll`, which populates `JwkSet` + the `PublicKeys map[string]*rsa.PublicKey` keyed by `kid`.
- A background goroutine refreshes the JWK set every `RefreshInterval` (we wire `REFRESH_INTERVAL=600` → 10 min by default; bump via env if AGS rotates faster).
- `Validate` resolves the public key via `RWMutex.RLock() → v.PublicKeys[kid]` — no network call on the hot path.

So the per-request auth path is entirely in-memory after boot. Do **not** wrap the validator in another TTL cache; that would only add a second clock to the same key set. If the SDK upgrade ever drops the background refresh goroutine, audit `auth_validator.go::Initialize` and re-evaluate. (Audited against `accelbyte-go-sdk@v0.87.1` on 2026-05-08 in response to the simplify report's E6 finding — the report's premise was already satisfied upstream.)

---

## 8. Temporary AGS platform workarounds

Each entry here compensates for a missing or pre-release AGS Platform feature. Every entry is **expected to be reverted** once the upstream feature lands — the doc exists so we don't forget to do the reversion, and so an outside reader can tell intentional design from load-bearing duct tape.

- **Database**: the service is configured against Neon Postgres in M1 deployments while we still depend on schema migrations via `golang-migrate`. The PRD targets Extend-managed Postgres (Architecture §5.2). Revert when Extend-managed Postgres exposes the migration-runner surface we need (or when we re-architect around whatever it does expose). Touches: `pkg/config`, `pkg/migrate`, cloud deploy env vars.

- **Admin RPC permissions**: every admin method in `proto/playtesthub/v1/playtesthub.proto` declares `ADMIN:NAMESPACE:{namespace}:EXTEND:APPUI` as its required resource, with a per-RPC action bit (CREATE / READ / UPDATE / DELETE) per the PRD §6 AuthZ mapping; the auth interceptor enforces the pair against the AGS IAM permission claim via the `accelbyte-go-sdk` permission validator. This is the locked-in **design choice** under PRD §6 AuthZ + §9 R8, not a placeholder. Two reasons APPUI is the right gate today: (1) **entry-surface match** — APPUI is the AGS-built-in perm tied to "render Extend App UIs in this namespace", which is exactly how studio admins reach playtesthub; (2) **zero AGS-side role setup** — every namespace-admin role studios already assign (Game Admin / Studio Admin / equivalent) holds APPUI, so adopters need no playtesthub-specific role creation, which is what makes this work on Shared Cloud where game admins cannot assign app-defined (`CUSTOM:*`) perms to their own user roles (AccelByte-only feature; on the AGS roadmap). The `AuditLog` (PRD §5.7) is the per-actor accountability layer that compensates for the coarse one-bit RBAC. **Forward compat**: when AGS Shared Cloud opens app-defined permission assignment to game admins, a future PRD bump can migrate to a dedicated playtesthub-specific resource string — at that point regenerate stubs (`./proto.sh`) and update the README deployment walkthrough. Tracked landing: M3 phase 18 ([`STATUS.md`](STATUS.md)).

- **Dev `extend-helper-cli` binary**: `appui create` + `appui upload` flows use the dev CLI distributed via Google Drive because public `extend-helper-cli` v0.0.10 lacks the `appui` subcommand. Swap back to the public release when the `appui` commands ship there — update `.devcontainer/post-create.sh` (`EXTEND_HELPER_CLI_VERSION`) and README dev-onboarding. Tracked inline in `docs/STATUS.md` M1 phase 8 note.

---

## 9. When this document is wrong

Update it. Engineering decisions drift; a stale `engineering.md` is worse than no `engineering.md`. If a new layer/pattern emerges (e.g. we add a background worker subsystem that needs its own test strategy), add a subsection here before the second instance lands. Three similar ad-hoc patterns is the trigger.
