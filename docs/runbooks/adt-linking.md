# Runbook — ADT linking + distribution

Studio operator walkthrough for the M5.B ADT distribution model (PRD §4.8 / [`docs/STATUS_M5.md`](../STATUS_M5.md)). Read this before linking your first ADT namespace.

> **Quickstart** — to run an ADT playtest you need to (1) set `ADT_BASE_URL` + `ADT_REDIRECT_BASE_URL` env vars on the backend (§2), (2) click **Link new ADT Namespace** on the Playtests list page once per studio (§3), (3) create a playtest with `distributionModel=ADT` and pick an ADT game + build (§4). The deliverable in the approval DM is a download URL, not a code (§5). The same AGS service IAM creds used by AGS Platform auth ADT; there is no separate ADT credential to manage.

## 1. Background

ADT (AccelByte Development Toolkit) playtests distribute an in-development build via a download URL, not a redemption code. The deliverable is the build itself; there is **no code pool** to upload or sync.

playtesthub authenticates to ADT on every API call using its own AGS service IAM JWT (the same client-credentials grant used for AGS Platform calls). ADT validates the JWT against AGS IAM's JWKS and derives the studio identity from the token's `iss` + `union_namespace` claims. **No credential is stored** on the playtesthub side beyond the existing `AGS_IAM_CLIENT_ID` / `AGS_IAM_CLIENT_SECRET` env vars.

Linking is **per-studio, not per-playtest**: one `adt_linkage` row covers every game namespace and every playtest under your studio. You re-link only on credential rotation or after an explicit unlink — not on every new playtest.

## 2. Environment variables

Before linking, set these on the backend deploy (PRD §5.9):

| Var | Required when | Default | Notes |
| --- | --- | --- | --- |
| `ADT_BASE_URL` | any `adt_linkage` row exists OR any playtest has `distributionModel='ADT'` | (none) | One of `develop.blackbox.accelbyte.io`, `staging.blackbox.accelbyte.io`, `blackbox.accelbyte.io`. Origin only; no path. |
| `ADT_REDIRECT_BASE_URL` | `ADT_BASE_URL` is set | (none) | The URL where the AGS Admin Portal serves the playtesthub Extend App UI — the operator's browser must be able to navigate to `${ADT_REDIRECT_BASE_URL}/adt-link-callback`. For ISC namespaces the shape is `https://<parent-ns>.internal.gamingservices.accelbyte.io/admin/namespaces/<game-ns>/extend/app-ui/playtesthub` (e.g. `https://abtestdewa.internal.gamingservices.accelbyte.io/admin/namespaces/abtestdewa-pong/extend/app-ui/playtesthub`). |
| `ADT_LINKAGE_PENDING_TTL_SECONDS` | optional | `600` | TTL on the `adt_link_pending` nonce row — the `state` returned from `StartADTLink` is rejected by `CompleteADTLink` after this many seconds. |

No `ADT_DEFAULT_API_KEY` or `ADT_CREDENTIAL_KEK` env var exists by design — auth is the AGS service IAM JWT (see §1), not a separately-issued credential.

## 3. Link redirect flow

1. Sign into the AGS Admin Portal and open the playtesthub Extend App UI.
2. On the **Playtests** list page, scroll to the **ADT Linkages** panel and click **Link new ADT Namespace**.
3. A modal explains the redirect; click **Proceed**. The admin UI:
   - Persists any open create-playtest form draft to `sessionStorage` so you don't lose work.
   - Calls `StartADTLink`; the backend mints a 32-byte `state` nonce, persists `adt_link_pending`, and returns `linkUrl = ${ADT_BASE_URL}/oauth/link?state=…&redirect_uri=…&studio_namespace=…`.
   - Assigns `window.location.href = linkUrl`.
4. On ADT, sign in and pick the namespace you want to link. ADT records its side's `(adt_namespace, studio_namespace) linked = true` flag, then redirects back to `${ADT_REDIRECT_BASE_URL}/adt-link-callback?state=…&result=success&adt_namespace=<picked>`.
5. The admin UI callback route reads `state` + `adt_namespace` from the URL, calls `CompleteADTLink(state, adt_namespace)`; the backend validates the pending row, inserts the `adt_linkage` identity row, writes the `adt_linkage.create` audit row, and flashes "ADT namespace linked".
6. The form draft from step 3 is rehydrated.

### How `studio_namespace` is derived

The `studio_namespace` baked into the linkUrl is computed **server-side** from the playtesthub backend's own AGS service IAM JWT (`token.union_namespace ?? token.namespace`) — NOT from the calling admin's request token. This is the only way for the playtesthub-side linkage row to match ADT's side: every downstream ADT API call from playtesthub carries the backend service token, so ADT keys its flag on what *that* token represents. If the backend's service token carries neither `union_namespace` nor `namespace`, `StartADTLink` returns `FailedPrecondition` per [`errors.md`](../errors.md).

### Why there's no credential rotation step

Auth to ADT on every API call is a freshly-minted AGS service IAM JWT (existing `pkg/ags` token getter, `AGS_IAM_CLIENT_*` env vars). Rotation happens automatically via the AGS IAM client-credentials grant. No `adt_credential_*` column exists on `adt_linkage`; the migration test pins this as a regression canary.

## 4. Creating an ADT playtest

After at least one linkage exists, the **Create playtest** form's distribution-model selector gains the **ADT** option. Picking it reveals:

- **ADT linkage** — Select backed by `ListADTLinkages`. Picking one auto-populates `adtNamespace`.
- **ADT game id** — free-text Input (matches the ADT-side game id).
- **ADT build id** — Select backed by `ListADTBuilds(linkageId, adtGameId)` once both inputs are set. Falls back to a free-text Input if ADT's build-list endpoint is unavailable.
- **Static fallback download URL** — optional https URL used when ADT cannot mint a download URL at approve time (see §6).

Submit; the backend verifies the picked `adt_build_id` belongs to `(adt_namespace, adt_game_id)` via a defense-in-depth call to `adt.Client.ListBuilds`, then commits the playtest with `distribution_model='ADT'`. The three ADT identifiers (`adt_namespace`, `adt_game_id`, `adt_build_id`) are **immutable post-create**; only `adt_fallback_download_url` is editable via `EditPlaytest`.

CLI alternative: `pth playtest create --distribution-model=ADT --adt-namespace=… --adt-game-id=… --adt-build-id=… [--adt-fallback-url=…]`.

## 5. Approve flow

`ApproveApplicant` against an ADT playtest:

- Skips code reservation entirely.
- Calls `GET <ADT_BASE>/profiling/namespaces/<adt_namespace>/agsplaytesthub/games/<adt_game_id>/builds/<adt_build_id>/downloadUrls?limit=20`.
- On ADT 401 (linkage missing or revoked on the ADT side): the applicant stays `PENDING`; the admin sees `FailedPrecondition` "adt linkage no longer exists or service token rejected, re-link required". See §7 for recovery.
- On ADT 4xx/5xx with the linkage row still present: falls back to a single-element list containing `adt_fallback_download_url` if set (audit records `{adtUrlSource: 'fallback'}`); otherwise the applicant stays `PENDING` with `Unavailable`.
- On success the welcome DM body lists every URL ADT returned. Single-file builds → `Download your playtest build for "<title>": <url>` (one line, no markdown). Multi-asset builds → the same heading followed by `1) <url>` / `2) <url>` / … one URL per line. See [`dm-queue.md`](../dm-queue.md) §"DM body shape — ADT distribution" for examples. URLs are **per-build**, not per-applicant — every approved applicant for a given playtest receives the same URL list, and ADT bounds them with a fixed 24-hour CDN TTL.
- The `applicant.approve` audit row carries `{adtUrls, adtUrlSource}` where `adtUrls` is a JSON array of every URL minted at approve time. URLs are not redacted (URLs ≠ codes; forensics require the URL list).

`RetryDM` and `RetryFailedDms` re-mint fresh URLs because the prior 24h TTL may have expired. The audit row's `adtUrls` snapshot is **not** rewritten on retry — it reflects the URLs minted at the original approve time.

## 6. Revocation

ADT does not expose per-applicant revocation (per-build URLs only). To cut off a single tester, use `RejectApplicant` — that closes off `GetADTDownloadInfo` access on the playtesthub side, but the ADT-issued URL itself stays valid until the 24h TTL expires. If you need stricter revocation, point the playtest at your own CDN via `adtFallbackDownloadUrl` and rotate the URL there.

## 7. Unlink + relink recovery

If ADT reports a linkage flag mismatch (`IssueDownloadURL` 401 / approve returns "adt linkage no longer exists or service token rejected, re-link required"):

1. Open **ADT Linkages** on the Playtests list page.
2. Click **Unlink** on the affected row. This soft-deletes the playtesthub-side `adt_linkage` row, writes an `adt_linkage.delete` audit, and best-effort `DELETE`s the ADT-side flag (`DELETE <ADT_BASE>/profiling/namespaces/<adt_namespace>/agsplaytesthub/linkage`). ADT eventual-consistency is tolerated; the unlink is idempotent.
3. Run the **Link new ADT Namespace** flow again (§3). The partial unique index on `adt_linkage` permits re-linking the same `adt_namespace` after a soft-delete.
4. Existing ADT playtests resume working on the next approve / `RetryDM` once the new linkage is in place — no playtest re-creation needed (the playtest's `adt_*` identifiers stay the same; only the linkage flag the ADT side keys on is refreshed).

CLI alternative for §3 + the unlink in §2: `pth adt linkage unlink --id <adt_linkage_id>` then `pth adt linkage start` (echoes `linkUrl` + `state`; open the link in a browser, complete the ADT side, then run `pth adt linkage complete --state … --adt-namespace …` with the values from the callback URL).

## 8. Open follow-ups

- The HTTP-backed adapter in `pkg/adt` (NewHTTPClient) is enabled in production when `AuthEnabled && ADT_BASE_URL` is set; dev / smoke / e2e boots fall back to `MemClient`. Live round-trip validation against `develop.blackbox.accelbyte.io` is gated on ADT-eng confirming the path shapes in `pkg/adt/http.go` match production.
- The end-to-end e2e test (`e2e/golden_m5_test.go`) currently skips; CLI dry-run coverage is in `scripts/smoke/pth.sh` and unit-level coverage is in `pkg/service/adt_*_test.go` + `pkg/adt/http_test.go`.
