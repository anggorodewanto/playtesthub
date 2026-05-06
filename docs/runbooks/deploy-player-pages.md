# Deploy — player Svelte bundle to GitHub Pages

Step-by-step setup a self-hosting playtesthub operator must complete to ship the player frontend on GitHub Pages, end-to-end with a deployed AGS Extend backend. Written assuming you already have the backend deployed and the Discord+AGS setup from [`setup-ags-discord.md`](setup-ags-discord.md) done.

This runbook is **prescriptive**. The architectural framing (why the player is static + same-origin-friendly + hash-routed) lives in [`docs/architecture.md`](../architecture.md) and PRD §5.8. The byte-exact Discord redirect-URI rule is the same one documented in [`setup-ags-discord.md` § Three URLs that must agree byte-for-byte](setup-ags-discord.md#three-urls-that-must-agree-byte-for-byte) — the only thing this doc adds for that rule is the Pages-specific `${PLAYER_ORIGIN}` value (§ 4 below).

If you only read one section of this file, read [§ 4 The two URLs that diverge under a subpath deploy](#4-the-two-urls-that-diverge-under-a-subpath-deploy). Pages serves the bundle under `/<repo>/`, which makes the player **origin** and the player **callback URL** different shapes — easy to swap and the symptom is silent (CORS-blocked or `Invalid OAuth2 redirect_uri`).

## Prerequisites

- Public GitHub repo (your fork of `playtesthub` or a private repo with GitHub Pro / Team — Pages on private repos requires either).
- A deployed playtesthub backend on AGS Extend with a publicly resolvable URL — typically `https://<ags-host>/ext-<namespace>-<app>`. Check `extend-helper-cli get-app-info` (or the `custom-service-manager` `GetAppV2` API) → `servicePublicURL`.
- Discord OAuth app + AGS Discord platform credential already configured per [`setup-ags-discord.md`](setup-ags-discord.md) steps 1–3. You'll be **adding** the Pages redirect URI to those, not replacing.
- `gh` CLI logged in with `repo` + `workflow` scopes.

## Steps

### 1. Enable GitHub Pages with the workflow build source

Pages must be set to **Build and deployment → Source: GitHub Actions** (not "Deploy from a branch"). The workflow at `.github/workflows/pages.yml` uploads the `player/dist/` bundle as a Pages artifact and the `actions/deploy-pages@v4` step publishes it.

UI: **Settings → Pages → Build and deployment → Source: GitHub Actions**.

API equivalent (for scripted forks):

```sh
gh api -X POST repos/<owner>/<repo>/pages -f build_type=workflow
```

This is one-time per repo. Verify:

```sh
gh api repos/<owner>/<repo>/pages --jq '{has_pages, build_type, html_url}'
# → {"has_pages":true,"build_type":"workflow","html_url":"https://<owner>.github.io/<repo>/"}
```

The `html_url` is your **player origin + base path** — write it down; it shows up in steps 2, 3, and 4.

### 2. Set the three repo Variables

The workflow refuses to build if any are unset, so the first run after a fork **must** include this step. These are public values (Discord client ID is meant to ship in the bundle, the gateway URL is reachable by anyone) — store them as **Variables**, not Secrets, so they show up in workflow logs and you can see at a glance what's deployed.

UI: **Settings → Secrets and variables → Actions → Variables tab → New repository variable**, three times.

CLI equivalent:

```sh
gh variable set PLAYER_GRPC_GATEWAY_URL --body "https://<ags-host>/ext-<namespace>-<app>"
gh variable set PLAYER_IAM_BASE_URL     --body "https://<ags-host>"
gh variable set PLAYER_DISCORD_CLIENT_ID --body "<Discord OAuth Client ID>"
```

| Variable | Source | Notes |
| --- | --- | --- |
| `PLAYER_GRPC_GATEWAY_URL` | `extend-helper-cli get-app-info` → `servicePublicURL`. | The full base of the grpc-gateway HTTP surface. The workflow writes this verbatim into `player/public/config.json` as `grpcGatewayUrl`; the loader hard-fails if it isn't a valid URL (PRD §5.8). |
| `PLAYER_IAM_BASE_URL` | The AGS host root. | No path component. SDK plumbing reads this even though Discord exchange goes through the backend now. |
| `PLAYER_DISCORD_CLIENT_ID` | Discord developer portal → OAuth2 → Client ID (the public one — **not** Client Secret). | Public per Discord's OAuth model; safe in repo Variables and in the static bundle. |

The workflow's "Verify required Repository Variables are set" step is the canary — if it fails, the run aborts before publishing a half-configured bundle.

### 3. Allowlist the Pages origin on the backend's CORS gate

Pages serves the player off-origin from the AGS-hosted backend. Without backend CORS handling, the browser preflights `OPTIONS /v1/...` and the gateway responds **`501 Method Not Allowed`** — every cross-origin call dies before the gRPC layer sees it.

The backend reads `CORS_ALLOWED_ORIGINS` (comma-separated) and reflects matched origins with `Access-Control-Allow-Credentials: true`. Empty or unset → no CORS handling (vanilla grpc-gateway), which is fine only for same-origin local dev.

Set it on the deployed Extend app:

```sh
extend-helper-cli update-var \
  --namespace <namespace> --app <app> \
  --key CORS_ALLOWED_ORIGINS \
  --value "https://<owner>.github.io,http://localhost:5173" \
  --description "browser origins permitted to call the gateway" \
  --force   # required on first set; AGS otherwise rejects unknown vars
```

Then redeploy so the var takes effect:

```sh
extend-helper-cli deploy-app \
  --namespace <namespace> --app <app> \
  --image-tag <current-image-tag> \
  --wait --wait-limit 300
```

(`<current-image-tag>` from `get-app-info` → `deploymentImageTag`; you don't need a fresh image just to pick up an env-var change, but you do need to re-deploy.)

Verify the gateway honours the new origin:

```sh
curl -i -X OPTIONS "https://<ags-host>/ext-<namespace>-<app>/v1/public/playtests/__nope__" \
  -H "Origin: https://<owner>.github.io" \
  -H "Access-Control-Request-Method: GET" \
  -H "Access-Control-Request-Headers: authorization,content-type"
```

Expected: `HTTP/2 204` with `access-control-allow-origin: https://<owner>.github.io` + `access-control-allow-credentials: true` + `access-control-allow-methods: GET, POST, PUT, PATCH, DELETE, OPTIONS`. A bare 501 here means the new image isn't running yet — wait for `appStatus=deployment-running` in `get-app-info`.

The middleware lives in `pkg/common/cors.go`; `"*"` reflects the request origin (credentialed responses can't use a literal star).

### 4. The two URLs that diverge under a subpath deploy

Pages project sites serve the bundle under `https://<owner>.github.io/<repo>/`. That subpath is on the **URL path**, not the origin. Two consequences a forker has to internalise before going further:

| Concept | Value for a Pages deploy | Where it gets used |
| --- | --- | --- |
| **Player origin** (`window.location.origin`) | `https://<owner>.github.io` (scheme + host, no path) | The **CORS allowlist** value in step 3. |
| **Player callback URL** (Discord redirect) | `https://<owner>.github.io/<repo>/callback` (full URL with path) | The **Discord redirect URI** allowlists in step 5. |

Mixing these up is a silent failure: putting the full callback URL into `CORS_ALLOWED_ORIGINS` matches no origin and every API call fails; putting the bare origin into the Discord allowlist mismatches and Discord refuses the OAuth roundtrip with **"Invalid OAuth2 redirect_uri"**.

**Note for user/org sites.** If your Pages site is `https://<owner>.github.io/` (a special-case repo named `<owner>.github.io`), the bundle is at the root and the two values collapse: origin = `https://<owner>.github.io`, callback = `https://<owner>.github.io/callback`. The workflow's `VITE_BASE: /${{ github.event.repository.name }}/` value is wrong in that case — override it to `/` by editing the workflow (or rename the repo and accept project-site shape).

### 5. Allowlist the Pages callback in Discord + AGS

The byte-exact rule from [`setup-ags-discord.md` § Three URLs that must agree byte-for-byte](setup-ags-discord.md#three-urls-that-must-agree-byte-for-byte) applies unchanged. Pages just adds **one more environment** to register:

1. **Discord developer portal → OAuth2 → Redirects** — add `https://<owner>.github.io/<repo>/callback`. Discord matches byte-for-byte.
2. **AGS Admin Portal → Login Methods → Platforms → Discord → RedirectUri** — set this **byte-equal** to the value above.

The "exactly one RedirectUri per Discord platform credential" trap from `setup-ags-discord.md` § 6 still holds. If you also need local-dev and CLI-loopback Discord login working, you'll need separate AGS namespaces (one Discord credential each), or separate Discord OAuth apps. There's no single-credential way to register multiple `RedirectUri` values.

### 6. First deploy

Push any change under `player/**` to `main`, or trigger manually:

```sh
gh workflow run pages.yml
gh run watch --exit-status   # picks the latest run
```

Two jobs run: `build player` (verify Variables → npm ci → tests → emit `public/config.json` → `npm run build` with `VITE_BASE=/<repo>/` → upload artifact) and `deploy to Pages`. End-to-end is ~30–60s on a small bundle.

Verify the deploy:

```sh
curl -sI "https://<owner>.github.io/<repo>/"
# → HTTP/2 200, content-type: text/html

curl -s "https://<owner>.github.io/<repo>/config.json"
# → JSON object with the three values from your repo Variables
```

In a browser, navigate to `https://<owner>.github.io/<repo>/#/playtest/<seeded-slug>`. The DevTools Network tab should show a GET to `<PLAYER_GRPC_GATEWAY_URL>/v1/public/playtests/<slug>` returning 200 (or 404 with `access-control-allow-origin: https://<owner>.github.io` if the slug doesn't exist — the page renders "This playtest is not available." for 404).

## Verification ladder

Run in order. Don't skip; each step builds on the last.

1. **Pages enabled with workflow build source** — `gh api repos/<owner>/<repo>/pages` returns `build_type=workflow`. If 404, step 1 didn't take.
2. **All three Variables set** — `gh variable list` shows `PLAYER_GRPC_GATEWAY_URL`, `PLAYER_IAM_BASE_URL`, `PLAYER_DISCORD_CLIENT_ID`. Missing any → workflow's "Verify required Repository Variables are set" step fails fast with a per-variable `::error::` annotation.
3. **CORS preflight returns 204 from the deployed gateway** — the `curl -X OPTIONS` probe in step 3. A 501 means `CORS_ALLOWED_ORIGINS` either isn't set or the new var didn't take (`extend-helper-cli get-app-info` → check `redeploymentInfo.shouldRedeploy`).
4. **Pages bundle loads and renders** — navigate to `https://<owner>.github.io/<repo>/`. Should render the "No playtest selected" landing copy. Empty page or boot-error copy means `config.json` is malformed (PRD §5.8) — view the file directly at `/<repo>/config.json` to see what got written.
5. **Cross-origin GET succeeds end-to-end** — navigate to `#/playtest/<known-slug>`. Network panel: the GET to `<PLAYER_GRPC_GATEWAY_URL>/v1/public/playtests/<slug>` should return 200 (or 404 for an unseeded slug) **with** `access-control-allow-origin: https://<owner>.github.io`. Console: no `Access to fetch …has been blocked by CORS policy` error.
6. **Discord login round-trip** — click Sign up, complete Discord consent, land back on `https://<owner>.github.io/<repo>/callback?code=…`, the bridge rewrites to `/#/callback?code=…`, the exchange POST returns 200, and the player ends up on `/#/signup`. Failures here are Discord/AGS misconfig — see [`setup-ags-discord.md` § Common failure modes](setup-ags-discord.md#common-failure-modes).

If any step fails, see [§ Common failure modes](#common-failure-modes) before changing config.

## Common failure modes

| Symptom | Root cause | Fix |
| --- | --- | --- |
| Workflow's **"Verify required Repository Variables are set"** step fails with three `::error::` annotations. | One or more repo Variables unset. | Set them per step 2. The error annotations name the missing variable directly. |
| Pages bundle loads but renders the **`BootError`** screen with `config.json missing required key` or `is not a valid URL`. | Workflow wrote a partial config (Variable was set to an empty string). | View `https://<owner>.github.io/<repo>/config.json`; the offending key will be empty. Re-set the Variable and re-run the workflow. |
| Browser console shows **`Access to fetch at '<gateway>' from origin 'https://<owner>.github.io' has been blocked by CORS policy: Response to preflight request doesn't pass access control check: No 'Access-Control-Allow-Origin' header is present on the requested resource`**. | Backend's `CORS_ALLOWED_ORIGINS` doesn't include `https://<owner>.github.io`, **or** the change wasn't picked up by a redeploy. | Check `extend-helper-cli get-app-info` → it doesn't surface env vars; cross-check by reading the deployed log lines for the gateway-startup `corsAllowedOrigins=[...]` field. Re-set + `deploy-app` per step 3. |
| Same CORS-blocked error, but only on **POST** requests; **GET** works. | Edge case where the gateway answered the preflight but the browser sends `Access-Control-Request-Headers` for headers the middleware doesn't reflect. The middleware reflects request headers verbatim — so this only happens if the request bypassed preflight (e.g. a non-CORS-safe header, missing on GET). | Check the failing request's `Access-Control-Request-Headers` value against the OPTIONS response's `Access-Control-Allow-Headers`. If they don't match the request will be blocked. |
| Discord rejects the OAuth roundtrip with **"Invalid OAuth2 redirect_uri"** rendered on `discord.com`. | Pages callback URL `https://<owner>.github.io/<repo>/callback` is not on Discord's developer-portal Redirects allowlist. | Add it (step 5.1). The path component matters — `/<repo>/` not `/`. |
| Player lands back at `/<repo>/callback?code=…` but never advances; **`POST /v1/player/discord/exchange → 400`** with `Invalid "redirect_uri" in request.` | AGS Admin Portal Discord `RedirectUri` doesn't byte-equal `https://<owner>.github.io/<repo>/callback`. | Set them byte-equal (step 5.2). The trap is the path: `/callback` (root) ≠ `/<repo>/callback`. |
| Pages-deployed bundle loads assets from the wrong origin (404 on `/assets/index-*.js`). | `VITE_BASE` mismatch. The workflow sets `/${{ github.event.repository.name }}/`, which works for project sites. User/org sites (`<owner>.github.io` repo) need root. | Edit `.github/workflows/pages.yml` and set `VITE_BASE: /` for the user/org-site case. |
| Discord OAuth round-trip lands on `https://<owner>.github.io/<repo>/callback?code=…` and Pages renders its **default 404 page** ("There isn't a GitHub Pages site here."). The bundle never loads, so `bridgePathCallback` never gets to rewrite `/<repo>/callback` → `/<repo>/#/callback`. | Pages doesn't rewrite unknown paths to `index.html`, so the path-based callback is a 404 by default. | The workflow's "SPA fallback" step copies `dist/index.html` → `dist/404.html`. Pages serves `404.html` for any unknown path under `/<repo>/`, which loads the bundle and lets the bridge run. If a fork removes that step or upgrades the workflow without preserving it, this regresses. |
| **Pages deploy succeeds but the URL still 404s**, even after several minutes. | Pages site exists but build_type isn't `workflow`. | `gh api repos/<owner>/<repo>/pages --jq .build_type` — if it's `legacy`, switch to `GitHub Actions` in Settings → Pages. |

## Cross-references

- [`setup-ags-discord.md`](setup-ags-discord.md) — Discord OAuth + AGS platform credential prerequisites. The byte-exact redirect-URI rule lives there; this doc only adds the Pages-shaped value.
- [`docs/architecture.md`](../architecture.md) § "Hosting conveniences" — why GitHub Pages / Vercel for the player.
- [`docs/PRD.md`](../PRD.md) §5.8 — runtime config contract (`config.json`, hard-fail on malformed).
- `.github/workflows/pages.yml` — the deploy workflow itself; the source of truth for what gets built and how.
- `pkg/common/cors.go` — CORS middleware; `CORSMiddleware` doc comment covers the wildcard / credentialed-response semantics.

## Out of scope

- Vercel + custom-domain deploys. The same shape works (set `VITE_BASE=/`, point your DNS at Vercel, allowlist your custom origin in `CORS_ALLOWED_ORIGINS`), but the click-through is Vercel-specific and not duplicated here.
- AGS tenant provisioning, Discord app provisioning. Covered by [`setup-ags-discord.md`](setup-ags-discord.md).
- Bot-token DM delivery (`DISCORD_BOT_TOKEN`). Independent of the player deploy; see the deploy guide / env-var reference.
