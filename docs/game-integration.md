# playtesthub — Game integration

How to wire playtesthub's player data into your game. The short version: **playtesthub identifies players by their Discord-federated AGS user, the game probably identifies them by their Steam-federated AGS user, and those are two different AGS userIds for the same human** unless you bridge them. This doc covers why, and the four integration patterns a game dev can pick from.

This is integration guidance for the game team consuming playtesthub output. It is not a backend requirement of playtesthub itself — playtesthub only hands out codes (Steam keys or AGS Campaign codes); it does not grant in-game entitlements or progression to an AGS userId.

---

## 1. Why the userIds differ

AGS IAM auto-creates one **headless** Justice account per platform login, keyed on `<platformId>:<platformUserID>`. Lookup is by that composite key — **not by email** for Discord or Steam (`platforms/platforms.go:600,703-790` in `justice-iam-service`). Email-based auto-link only fires for Google / GenericOAuth with a domain allowlist, which is not the path Discord or Steam take.

Consequence for a playtesthub deployment:

| Surface | Login method | AGS account created |
| --- | --- | --- |
| Player app (signup, NDA, get code, survey) | Discord OAuth → AGS platform-token grant | `Acc1` — Discord-headless, userId `X` |
| Game client (gameplay, progression) | Steam OIDC → AGS platform-token grant | `Acc2` — Steam-headless, userId `Y ≠ X` |

Two AGS users, same human. The playtest activity logged against `X` is invisible to anything running against `Y`, and vice versa.

This is fine if you only need playtesthub to hand the player a code they redeem in Steam (the default M1 / M2 flow). It matters the moment the game wants to **read playtesthub-side data** (e.g., "is this player approved for the playtest?", "did they accept the latest NDA?") keyed on AGS userId.

## 2. The IAM linking constraint

A natural reflex is: "the game will just link Discord to the player's in-game AGS account on first launch." That reflex is correct in spirit but hits a real AGS constraint.

When the game-side session (authed as `Acc2`, Steam-headless) calls `POST /v3/.../link/token/exchange` with the player's Discord token, AGS reverse-looks-up the Discord identity, finds it is **already attached to `Acc1`**, and rejects with `HTTP 409` / `LinkDifferentPlatformAccountsIsNotAllowed` (`pkg/oauth/api/v3handlers.go:5435-5438` in `justice-iam-service`). There is **no auto-merge** and no "claim headless" endpoint.

In other words: the platform that is added first to AGS wins. Whichever flow the player went through first owns the AGS account; the other side has to come to it.

## 3. Integration patterns

Four viable patterns. Pick one — they're mutually exclusive for a given game.

### Option A — Link Steam to the Discord account from inside the game (recommended)

The game treats Discord as the canonical identity and asks the player to authenticate with Discord on first launch, then links Steam onto that account.

1. Player completes the playtesthub flow (Discord login → approved → gets code). AGS now has `Acc1` (Discord-headless, userId `X`).
2. Player redeems the code in Steam, installs, launches the game.
3. **First-launch gate in-game** — "Sign in with Discord to access your playtest." Game does Discord OAuth → AGS platform-token grant → authed as `Acc1`.
4. From that session, game calls `POST /v3/.../link/token/exchange` with the player's Steam token. Steam attaches to `Acc1` (Steam not yet on any account → no 409). Single AGS userId from here on.
5. Subsequent launches: Steam login from the game resolves to `Acc1` natively. No further prompts.

**Pros**: single AGS userId, the game can read playtesthub data keyed on `X`, works for AGS Campaign code redemption too (the campaign grant lands on `Acc1`, which is the account the game will be using).
**Cons**: one extra OAuth gate on first launch. Player has to have Discord installed / browser-reachable.

### Option B — Discord ID as the cross-system join key (no IAM linking)

The game ignores AGS userId for the playtesthub join and uses **Discord user ID** as the foreign key.

1. Game-side backend keeps its own user table. On first launch, game has the player do Discord OAuth (just enough to extract the Discord user ID — no AGS linking).
2. Game-side backend stores `(steam_ags_userId, discord_user_id)`.
3. To ask "is this player approved for the playtest?", game-side backend calls playtesthub keyed on `discord_user_id`, not AGS userId.

**Pros**: zero IAM linking calls, no 409 risk, works regardless of which AGS account the game logs into. Cleanest separation if the game already has its own backend with player records.
**Cons**: playtesthub backend would need a "lookup by Discord ID" surface — not currently exposed; would need an admin RPC or an extension. The game-side has to own the join. AGS-side progression / entitlements still live on two separate userIds.

### Option C — Switch the player app to Steam login

If the playtest is Steam-only and you don't need Discord for DM delivery, drop Discord OAuth from the player app and use Steam OpenID instead. Player app and game both produce Steam-headless accounts → same AGS userId natively. No linking, no first-launch gate.

**Pros**: simplest possible identity story.
**Cons**: loses the Discord-DM code delivery channel (playtesthub's `RetryDM` worker and Discord bot become inert). Player app must run somewhere Steam OpenID can return to (Steam OpenID redirect URIs are real URLs, fine for Pages / Vercel). Doesn't help mixed-platform playtests.

### Option D — Manual link via AGS Admin Portal

Operator-side escape hatch for a small playtest cohort: an admin unlinks Discord from `Acc1` and links it to `Acc2` by hand in the Admin Portal. Useful for support cases ("I already played, my progress is stuck on the wrong account"). Not a real integration strategy.

## 4. Recommendation

For most studios already using AGS who run a Steam playtest with Discord DM delivery, **Option A** is the path: one in-game Discord gate on first launch, then everything (playtesthub data, AGS entitlements, AGS progression, AGS Campaign grants) consolidates onto one userId.

**Option B** is the right answer if the game already has its own backend with player records, doesn't want to touch IAM linking, and is happy treating playtesthub as a satellite system joined on Discord ID. Note that playtesthub does not yet expose a stable read-by-Discord-ID surface — if you go this route, file an issue describing the RPC shape you need.

**Option C** wins for Steam-only playtests where Discord DM delivery isn't a requirement.

## 5. References

- AGS headless account creation per platform login — `justice-iam-service/platforms/platforms.go:600,703-790`.
- Email-based auto-link is Google / GenericOAuth only — `justice-iam-service/platforms/platforms.go:615-660`.
- Platform link conflict (`HTTP 409`, `LinkDifferentPlatformAccountsIsNotAllowed`) — `justice-iam-service/pkg/oauth/api/v3handlers.go:5435-5438`.
- One-time-code link endpoint — `justice-iam-service/pkg/oauth/api/v3handlers.go:5941-6053`.
- playtesthub Discord federation — [`docs/engineering.md`](engineering.md), [`docs/runbooks/discord-login.md`](runbooks/discord-login.md).
- playtesthub Discord-DM constraints (bot + applicant must share a guild) — [`docs/runbooks/setup-ags-discord.md` § 7](runbooks/setup-ags-discord.md#7-discord-bot--server-required-for-dm-delivery).
