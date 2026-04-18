# playtesthub — Architecture

Implementation stack and external dependencies for playtesthub. Referenced from PRD §8. The PRD is authoritative for product requirements; this document captures the concrete technology choices and external integrations.

## Stack

The deployable artefact is one Go backend plus two frontends (one per audience), all backed by an Extend-managed Postgres instance.

### Backend
- **Language**: Go.
- **Transport**: gRPC. Concurrency model is goroutine-per-request with no global cap in MVP — Extend gateway defaults apply.
- **DB**: Postgres, Extend-managed. Schema migrations via [golang-migrate](https://github.com/golang-migrate/migrate).
- **Deploy target**: AGS Extend Service Extension.

### Player frontend
- **Framework**: Svelte.
- **Hosting**: GitHub Pages or Vercel (static bundle).
- **Runtime configuration**: `config.json` loaded at app boot — see PRD §5.8 for the full shape and failure-mode rules.

### Admin frontend — Extend App UI
- **Delivery model**: **Extend App UI** (experimental AGS capability). Built as a Module Federation remote that the AGS Admin Portal host loads at runtime and renders inside **Extend → My Extend Apps → App UI**. AccelByte hosts the built bundle — no GitHub Pages / Vercel involvement for the admin surface.
- **Framework / tooling**: React 19 + TypeScript + Vite + `@module-federation/vite`. The bundle exports a `mount(container, hostContext)` function matching the `AppUIModule` contract from `@accelbyte/sdk-extend-app-ui`; the Admin Portal host invokes it with `HostContext = { basePath, sdkConfig, isCurrentUserHasPermission }`.
- **Component library / styling**: **Ant Design v6** + **Tailwind v4** (via `@tailwindcss/vite`, utilities prefixed `appui:` to avoid colliding with host CSS). **`justice-ui-library` is not used** in the Extend App UI pattern.
- **Backend client generation**: `@accelbyte/codegen` consumes `<service_url>/apidocs/api.json` (the grpc-gateway-emitted OpenAPI spec for our `playtesthub.v1` proto) and emits typed endpoint classes + `@tanstack/react-query` hooks. The admin app imports those hooks directly — no hand-rolled fetch code and no repeated request DTOs.
- **Auth**: the Admin Portal host injects `sdkConfig` (baseURL / clientId / namespace / redirectURI) through `HostContext`; the AccelByte JS SDK (`@accelbyte/sdk`, `@accelbyte/sdk-iam`) owns token lifecycle. No `postMessage`, no manual bearer-token wiring inside our code.
- **Local dev**: `npm run dev` boots Vite with `@accelbyte/sdk-extend-app-ui/plugins`' `devProxyPlugin` proxying `/ext-<namespace>-<app>` to AGS with auth attached, and `main.tsx` fabricates a `HostContext` from `VITE_AB_*` env vars so the same bundle runs outside the Admin Portal.
- **Scaffold source**: `AccelByte/extend-app-ui-templates` (canonical post-GA) / `tryajitiono-ab/test-admin-ui` (playtest-stage mirror). Relevant template: `templates/react` (single Extend app) — `templates/react-multiple-extend-apps` is only needed if playtesthub ever fans out across multiple service extensions.
- **Deployment**: `extend-helper-cli appui create` (one-time register) → `extend-helper-cli appui upload` (builds + ships `dist/` to AccelByte). Menu registration is implicit — the Admin Portal auto-surfaces uploaded App UIs.
- **Availability caveat**: Extend App UI is currently available only in **Internal Shared Cloud**. Private Cloud support is pending. Adopters on Private Cloud must wait for GA or fall back to the legacy extension-site pattern. Tracked as a risk in PRD §9.

## External dependencies

The backend integrates with AGS services and one third-party identity provider. All other externals are deployment conveniences.

### AGS services (required)
- **IAM** — Discord OAuth federation and AGS access-token issuance.
- **Platform / Campaign API** — required for the `AGS_CAMPAIGN` distribution model: Item (ENTITLEMENT type) creation, Campaign creation, code generation/retrieval. Not invoked at approve time for either distribution model.

### Extend features
- **Required**: Service Extension (gRPC), Extend-managed Postgres, **Extend App UI** (experimental capability; admin UI delivery).
- **Not used in MVP**: Event Handler, Scheduled Action, Override pattern, legacy `justice-adminportal-extension-website` extension-site path.

### Third-party
- **Discord** — OAuth app + bot token. Bot token is used to fetch Discord handles at signup (see PRD §10 M1) and to deliver DMs on approve (PRD §4.1 step 6d, §5.4).

### Hosting conveniences (deployment-only)
- **GitHub Pages / Vercel** for the Svelte player app.
- **AGS Platform API auth** reuses the IAM client credentials from the Extend app template; no additional studio-configured OAuth client.

### Explicit non-dependencies
- **Steam** — not a dependency; STEAM_KEYS codes are passthrough strings, redeemed by players on Steam directly.
- **Custom domains** — Extend-provided hostname only; no custom DNS configuration.
