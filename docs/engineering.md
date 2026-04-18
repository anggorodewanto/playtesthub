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

```
playtesthub/
├── main.go                          # entrypoint (template convention)
├── Dockerfile
├── docker-compose.yaml              # local dev: postgres + backend
├── Makefile
├── buf.yaml / buf.gen.yaml
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
│   │   └── *_test.go                # integration tests against testcontainers-postgres
│   ├── ags/                         # AGS Platform / Campaign API client
│   ├── iam/                         # IAM JWT validation wrapper around the AGS SDK
│   ├── discord/                     # Discord bot client (handle lookup + DM send)
│   ├── dmqueue/                     # in-memory FIFO, circuit breaker, restart sweep
│   ├── reclaim/                     # leader-elected reclaim job
│   └── config/                      # env-var parsing, defaults
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
├── scripts/
│   └── loadtest/                    # perf proof-point harness (PRD §6 / §7)
├── docs/                            # existing — PRD, schema, etc.
├── CLAUDE.md
└── README.md
```

Rationale for the boundaries:

- **`pkg/service` vs `pkg/repo`** — service layer is the only thing the gRPC server wires up. Handlers depend on repository interfaces, not concrete types. This is what makes handler-level unit tests fast (mocked repos) while repo tests exercise real SQL.
- **`pkg/ags`, `pkg/iam`, `pkg/discord`** — one package per external boundary. Each exposes a narrow interface and hides the SDK behind it. Tests mock at the interface; we never mock the SDK types directly.
- **`pkg/dmqueue`, `pkg/reclaim`** — internal subsystems with their own tests. They receive collaborators (repo, DM client) via interface injection.
- **No `internal/`** — everything under `pkg/` is private by convention; there is no downstream consumer importing us as a library.

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
| Proto lint | `buf lint` | |
| Proto stubs fresh | `buf generate` + `git diff --exit-code` | forces checked-in stubs to match `.proto`. |
| Svelte build | `npm run build` in `player/` | |
| Svelte a11y | `@axe-core/playwright`, pinned | five player pages; zero critical violations; scoped to `wcag2a, wcag2aa, wcag21a, wcag21aa`. |
| Admin codegen fresh | `npm run codegen` + `git diff --exit-code` in `admin/` | catches a backend proto/HTTP-annotation change that the admin UI hasn't regenerated against. |
| Admin build | `npm run build` in `admin/` (`tsc -b && vite build`) | catches type errors and MF bundle-build failures before deploy. |
| Admin unit | `npm run test` in `admin/` (Vitest) | |
| Migrations apply | `migrate up` against ephemeral Postgres | catches forward-only violations. |

Perf proof point (500 signups / 10 min, p95 < 3s) is **not** a CI gate — reported per-release in `CHANGELOG.md` from `scripts/loadtest/` (PRD §6 / §7).

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
- `make proto` → regenerate gRPC + grpc-gateway stubs and the OpenAPI spec under `gateway/apidocs/`.
- `make test` → unit + integration; assumes Docker is running.
- `make lint` → `golangci-lint run`.

### Admin (Extend App UI, in `admin/`)
- `cp .env.local.example .env.local` once; fill in `VITE_AB_*` values pointing at your AGS namespace + deployed service extension. `extend-helper-cli appui setup-env` can populate these.
- `npm install`.
- `npm run codegen` → downloads `apidocs/api.json` from the running backend and regenerates `src/playtesthubapi/`. **Rerun every time proto HTTP annotations change.**
- `npm run dev` → Vite on `http://localhost:5173`; `devProxyPlugin` auto-proxies `/ext-<namespace>-<app>` to AGS with auth.
- `npm run build` → `tsc -b && vite build`. Output: `dist/`.
- `extend-helper-cli appui upload --namespace $AB_NAMESPACE --name $AB_APPUI_NAME` → ships `dist/` to AccelByte.
- First-time registration only: `extend-helper-cli appui create --namespace $AB_NAMESPACE --name $AB_APPUI_NAME`.

### Player (Svelte, in `player/`)
- `npm install && npm run dev`.

`.env.example` files live alongside each deployable (`./.env.example`, `admin/.env.local.example`, `player/.env.example`). Actual `.env*` files are gitignored.

---

## 8. When this document is wrong

Update it. Engineering decisions drift; a stale `engineering.md` is worse than no `engineering.md`. If a new layer/pattern emerges (e.g. we add a background worker subsystem that needs its own test strategy), add a subsection here before the second instance lands. Three similar ad-hoc patterns is the trigger.
