# Discord login — verified end-to-end flow

Reference for the working playtesthub Discord-login flow against an AGS tenant. Captures the byte-exact URL shapes, the AGS / Discord developer-portal configuration each environment must agree on, and the specific failure modes we hit during phase 9.4 e2e verification.

This document is descriptive — it explains how the deployed system behaves today. The setup steps a new operator must perform from scratch live in `docs/runbooks/setup-ags-discord.md` (phase 9.5).

## TL;DR

1. Player navigates to `https://discord.com/oauth2/authorize?response_type=code&client_id=${DISCORD_CLIENT_ID}&redirect_uri=${PLAYER_ORIGIN}/callback&state=${state}&scope=identify+email`.
2. Discord redirects back to `${PLAYER_ORIGIN}/callback?code=...&state=...`. (`bridgePathCallback` rewrites the path-based callback to `#/callback?...` so the hash router takes over.)
3. `Callback.svelte` POSTs `{code, redirect_uri}` to `POST /v1/player/discord/exchange` on the backend.
4. Backend `pkg/service/discord_exchange.go` POSTs `platform_token=<code>&redirect_uri=<...>` with HTTP Basic auth (`AGS_IAM_CLIENT_ID:AGS_IAM_CLIENT_SECRET`) to `${AGS_BASE_URL}/iam/v3/oauth/platforms/discord/token`.
5. AGS IAM exchanges the code with Discord, auto-creates the Justice platform account on first call, returns AGS tokens.
6. Backend forwards `access_token / refresh_token / expires_in / token_type` to the player. Player stores `access_token` in `sessionStorage` and navigates to `pendingLogin.returnTo`.

## Three URLs that MUST agree byte-for-byte

The `redirect_uri` value flows through three independent systems. All three must be identical strings — character for character, including scheme, port, and absence of trailing slash.

| Where it lives | Value (this verified deploy) | Why it matters |
| --- | --- | --- |
| Discord developer portal → OAuth2 → Redirects | `http://localhost:5173/callback` | Discord rejects the `/oauth2/authorize` call with **"Invalid OAuth2 redirect_uri"** if the player sends a value not on this allowlist. |
| Player's call to `discord.com/oauth2/authorize` (`buildDiscordAuthorizeUrl` in `player/src/lib/auth.ts`) | `${window.location.origin}/callback` = `http://localhost:5173/callback` | Discord stores this value with the issued auth code. |
| AGS Admin Portal → `abtestdewa-pong` → Login Methods → Platforms → Discord → **RedirectUri** | `http://localhost:5173/callback` | When AGS later POSTs to `discord.com/api/oauth2/token`, it forwards this configured value as `redirect_uri`. Discord compares this string against the one stored at /authorize step. Mismatch → `400 invalid_grant: Invalid "redirect_uri" in request.` |

The third row is the one most easily missed. AGS's platform-token grant **does not honor a caller-supplied `redirect_uri`** — see [AGS source path verification](#why-ags-ignores-our-form-body-redirect_uri). The form-body parameter our backend sends is dead weight on the wire; AGS uses `platformClient.RedirectURI` from the Admin Portal value instead.

Self-hosted operators must therefore pick one canonical `${PLAYER_ORIGIN}` per AGS tenant + Discord-platform-credential pair. Dev and prod can't share unless they share a player origin.

## Why AGS ignores our form-body `redirect_uri`

Verified against the `justice-iam-service` source on 2026-04-28:

- `pkg/oauth/api/v3handlers.go:2672` — `handleUserPlatformTokenGrantV3` calls `apiRoute.GetPlatformUser(scope, clientNamespace, platformID, accessReq, "", createHeadless)`. The 5th parameter is `state`; no `redirectURI` is threaded through.
- `pkg/oauth/api/common.go:946` — `GetPlatformUser` for `osin.PLATFORM` calls `platformAuthenticationCtx.AuthenticateUser(scope, namespace, platformID, accessReq.Password, publisherNamespace, state, requestClientID, alwaysCreateNew)`. No `redirectURI` argument exists.
- `platforms/discord/discord.go:94-97` — Discord's `AuthenticateUser` defaults `redirectURIRequest := platformClient.RedirectURI` and only overrides if the caller passed a runtime `redirectURI`. Since the platform-token-grant codepath never passes one, `platformClient.RedirectURI` (the Admin Portal value) is what reaches Discord's `/oauth2/token`.

Backend reference: `pkg/service/discord_exchange.go` still includes `redirect_uri` in the form body. It is harmless on the wire (AGS ignores it) and kept for two reasons: (a) future AGS releases may start honoring it; (b) it documents intent at the call site that `redirect_uri` is part of the contract conceptually, even if the upstream is buggy today.

## Failure modes seen during phase 9.4 verification

| Symptom | Root cause | Fix |
| --- | --- | --- |
| `Invalid OAuth2 redirect_uri` rendered on `discord.com/oauth2/authorize` (no callback) | `${PLAYER_ORIGIN}/callback` not on Discord developer portal allowlist | Add it under OAuth2 → Redirects. |
| `Login failed — please try again later` on `/callback`, network shows `POST /v1/player/discord/exchange → 400`, body contains `discord.com/api/oauth2/token 400 {"error": "invalid_grant", "error_description": "Invalid \"redirect_uri\" in request."}` | AGS Admin Portal Discord RedirectUri ≠ player's `${PLAYER_ORIGIN}/callback` | Set the AGS Admin Portal value to byte-exact match. |
| `Login failed — please try again later` on `/callback`, network shows `POST /v1/player/discord/exchange → 400`, body contains `discord.com/api/oauth2/token 400 {"error": "invalid_grant", "error_description": "Invalid \"code\" in request."}` | The Discord code is bogus / already used / expired (also what the smoke probe sends) | Player retries — fresh OAuth roundtrip. |
| `Login failed — please try again later` on `/callback`, network shows `POST /v1/player/discord/exchange → 503` on the **first** call after a long idle period, but the same code replayed via `curl` seconds later returns 200 | Suspected: AGS Discord-call latency or vite dev-proxy short timeout on cold path. Requires further instrumentation. Tracked as STATUS.md follow-up | Retry — second attempt typically succeeds. |

## Verified payload shape (sanitized)

Successful `POST /v1/player/discord/exchange` response (200), captured 2026-04-28 against `abtestdewa-pong`:

```json
{
  "accessToken":  "<JWT, ~990 bytes>",
  "refreshToken": "<JWT, ~620 bytes>",
  "expiresIn":    3600,
  "tokenType":    "Bearer"
}
```

Decoded `accessToken` claims (PII-stripped):

| Claim | Value |
| --- | --- |
| `iss` | `https://internal.gamingservices.accelbyte.io` |
| `namespace` | `abtestdewa-pong` |
| `parent_namespace` | `abtestdewa` |
| `sub` | `<AGS user UUID>` (becomes `Applicant.userId` after signup) |
| `client_id` | matches `AGS_IAM_CLIENT_ID` (the confidential client) |
| `scope` | `account commerce social publishing analytics` |
| `ipf` | `discord` |
| `union_id` | `<UUID>` (AGS-internal cross-namespace handle) |

Refresh-token claims include `platform_id: "discord"` so AGS can track the federation source on token rotation.

After signup (`POST /v1/player/playtests/{slug}/signup` → 200), `GET /v1/player/playtests/{slug}/applicant` returns the row with `status: APPLICANT_STATUS_PENDING` and `userId` matching the AGS JWT `sub`. M1 golden flow target met.

## Smoke probe interpretation

`scripts/smoke/cloud.sh` posts `{code: "smoke-bogus-<random>", redirect_uri: "http://localhost:5173/callback"}` and asserts HTTP 400 with `invalid_grant` somewhere in the body. That probe validates **routing + AGS Basic-auth + the AGS-wraps-Discord-invalid_grant detection** in `mapAGSExchangeError` (`pkg/service/discord_exchange.go`). It does NOT validate the live success path — there is no way to drive a real Discord OAuth roundtrip from bash without a browser. Live success is captured here.
