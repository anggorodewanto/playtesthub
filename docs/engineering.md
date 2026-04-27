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
| Proto lint | `buf lint` | lint-only; `buf.yaml` at repo root, no `buf.gen.yaml`. |
| Proto stubs fresh | `./proto.sh` + `git diff --exit-code` | protoc codegen driver; forces checked-in stubs under `pkg/pb/` and `gateway/apidocs/` to match `.proto`. |
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
- `make proto` → regenerate gRPC + grpc-gateway stubs and the OpenAPI spec under `gateway/apidocs/` (runs `proto.sh` — protoc).
- `make lint-proto` → `buf lint` against the `proto/` tree.
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

#### Player AGS IAM client (Discord federation)

The player bundle is a static, public SPA — it cannot hold a client secret. The backend-side IAM client in `.env` (`AGS_IAM_CLIENT_ID`) is **confidential** (AGS IAM's `/iam/v3/oauth/token` demands HTTP Basic auth on it: `WWW-Authenticate: Basic`). A separate **public, PKCE-only** IAM client is required for the player; its ID is wired through the backend env var `PLAYER_IAM_CLIENT_ID` (consumed by `GetDiscordLoginUrl` — see below).

For the ISC demo namespace `abtestdewa-pong` this client is already registered: `d6bb5dd2cf6b4d23bd6d6400d7886b94`. Drop it into `player/public/config.json` as `discordClientId` alongside `iamBaseUrl: https://abtestdewa-pong.internal.gamingservices.accelbyte.io`, and into `.env` as `PLAYER_IAM_CLIENT_ID=d6bb5dd2cf6b4d23bd6d6400d7886b94`.

Register equivalent on any other namespace via AGS Admin Portal → **IAM → Clients → Create client**:
- **Client type**: public (no secret issued).
- **Grant types**: `authorization_code` (+ `refresh_token` if token rotation becomes needed).
- **Code challenge method**: `S256` (PKCE required).
- **Redirect URIs** — allowlist these exact strings. **No URL fragments** (`#…`): AGS IAM normalizes fragments during match, so `http://localhost:5173/#/callback` is silently invalid even when registered. Keep them path-based.
  - `http://localhost:5173/callback` — Vite dev server.
  - `http://127.0.0.1/callback` — phase 10 `pth` CLI (AGS IAM ignores the port on loopback hosts; one entry covers every ephemeral port the CLI picks).
  - `<prod-origin>/callback` — once the player bundle is publicly deployed.

  The router is hash-based (`#/callback`, `#/signup`, `#/pending`) for static-host compatibility, but the OAuth redirect lands on the path `/callback`. `src/lib/bootstrap.ts::bridgePathCallback()` runs before the app mounts and rewrites `/callback?code=…` → `/#/callback?code=…` via `history.replaceState`, so the hash-router's existing `callback` route handles it unchanged. For deploys that serve the bundle under a subpath, adjust `bridgePathCallback` alongside the registered redirect URI.
- **Scopes**: `commerce account social publishing analytics` — current `DEFAULT_DISCORD_LOGIN_SCOPE` in `player/src/lib/auth.ts`, inherited from AccelByte templates; trim to `account` once we confirm the backend IAM interceptor doesn't require the broader set.

##### The shared-cloud problem and the proxy RPC

On shared cloud, AGS IAM's hosted `/auth/?request_id=…` SPA does **not** render the Discord button — even though `/iam/v3/public/namespaces/{namespace}/platforms/clients/active` reports Discord as `IsActive=true`. AccelByte docs ("Set up a web login for Discord") confirm self-hosted apps must build their own login surface. We therefore drive `/iam/v3/oauth/platforms/discord/authorize` directly. That endpoint requires a `request_id` from a prior `/iam/v3/oauth/authorize` call, whose 302 is opaque cross-origin (the browser cannot read the `Location` header). The first hop must run server-side.

The flow is:

| Step | Origin | URL | Purpose |
| --- | --- | --- | --- |
| 1 | Backend | `POST {grpcGatewayUrl}/v1/player/discord/login-url` | `Player.GetDiscordLoginUrl` RPC. Player sends `redirect_uri`, `state`, `code_challenge`, `code_challenge_method=S256`, `scope`. PKCE verifier never traverses the wire — it stays in `sessionStorage`. |
| 2 | Backend → AGS | `GET {AGS_BASE_URL}/iam/v3/oauth/authorize?response_type=code&client_id={PLAYER_IAM_CLIENT_ID}&…` | Server-side, with `CheckRedirect=ErrUseLastResponse`. AGS returns 302; backend extracts `request_id` from `Location` query. |
| 3 | Backend → Player | RPC response | `login_url = {AGS_BASE_URL}/iam/v3/oauth/platforms/discord/authorize?request_id=…&client_id={PLAYER_IAM_CLIENT_ID}&redirect_uri=…` |
| 4 | Player → AGS | `GET login_url` | AGS 302 → `https://discord.com/api/oauth2/authorize?...&state={request_id}` |
| 5 | Player → Discord → AGS → Player | Discord OAuth → `/iam/v3/platforms/discord/authenticate` callback (AGS-side) → player `redirect_uri` with `?code=…&state=…` | The AGS-issued code reaches the player on the registered redirect URI. |
| 6 | Player → AGS | `POST {iamBaseUrl}/iam/v3/oauth/token` | `application/x-www-form-urlencoded`; body: `grant_type=authorization_code`, `code`, `code_verifier`, `client_id`, `redirect_uri`. No `Authorization: Basic` for public clients — a public client returns `400 invalid_grant` on a bad code, whereas a confidential client returns `401 invalid_client` (WWW-Authenticate: Basic). That 400-vs-401 flip is the quickest way to confirm a freshly-created client is registered as public. |

Wired in:
- `pkg/service/discord_login.go` — RPC handler (steps 2–3).
- `player/src/lib/auth.ts::fetchDiscordLoginUrl` — step 1.
- `player/src/lib/auth.ts::exchangeCodeForToken` — step 6.

**Why not `idp_hint=discord` against `/oauth/authorize`?** AGS IAM silently ignores `idp_hint`; it's a Keycloak-ism. The 302 still bounces through the hosted `/auth/` SPA, which (on shared cloud) doesn't render the Discord button. Don't use it.

**Redirect URI invalid?** If the proxy RPC returns `InvalidArgument` with a message containing `redirect URI invalid`, the upstream `/oauth/authorize` 302 carried `error_description=redirect+URI+invalid` — the player IAM client's allowlist hasn't caught up. Add the missing entry in the Admin Portal's client edit page.

---

## 8. Temporary AGS platform workarounds

Each entry here compensates for a missing or pre-release AGS Platform feature. Every entry is **expected to be reverted** once the upstream feature lands — the doc exists so we don't forget to do the reversion, and so an outside reader can tell intentional design from load-bearing duct tape.

- **Database**: the service is configured against Neon Postgres in M1 deployments while we still depend on schema migrations via `golang-migrate`. The PRD targets Extend-managed Postgres (Architecture §5.2). Revert when Extend-managed Postgres exposes the migration-runner surface we need (or when we re-architect around whatever it does expose). Touches: `pkg/config`, `pkg/migrate`, cloud deploy env vars.

- **Admin RPC permissions**: every admin method in `proto/playtesthub/v1/playtesthub.proto` currently declares `ADMIN:NAMESPACE:{namespace}:EXTEND:APPUI` as its required resource. That string is *not* the semantically correct permission — it's the AppUI-admin perm that game admins already hold in ISC today. The correct string is `CUSTOM:ADMIN:NAMESPACE:{namespace}:PLAYTEST` (AGS only honours app-defined perms under the `CUSTOM:` prefix). AGS Admin Portal in ISC does not yet let game admins assign `CUSTOM:*` perms to their own user roles; the user-permission-management feature is on the AGS roadmap. When it ships: swap the six `option (playtesthub.v1.resource) = "..."` declarations back to `"CUSTOM:ADMIN:NAMESPACE:{namespace}:PLAYTEST"`, regen stubs (`./proto.sh`), and update any role-grant docs in the README's deployment walkthrough.

- **Dev `extend-helper-cli` binary**: `appui create` + `appui upload` flows use the dev CLI distributed via Google Drive because public `extend-helper-cli` v0.0.10 lacks the `appui` subcommand. Swap back to the public release when the `appui` commands ship there — update `.devcontainer/post-create.sh` (`EXTEND_HELPER_CLI_VERSION`) and README dev-onboarding. Tracked inline in `docs/STATUS.md` M1 phase 8 note.

---

## 9. When this document is wrong

Update it. Engineering decisions drift; a stale `engineering.md` is worse than no `engineering.md`. If a new layer/pattern emerges (e.g. we add a background worker subsystem that needs its own test strategy), add a subsection here before the second instance lands. Three similar ad-hoc patterns is the trigger.
