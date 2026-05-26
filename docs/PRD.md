# playtesthub — Product Requirements Document

Version: 2.0 (MVP)
Owner: playtesthub maintainers
Status: Draft
Last updated: 2026-04-17
Companion docs: [`CHANGELOG.md`](CHANGELOG.md), [`schema.md`](schema.md), [`STATUS.md`](STATUS.md), [`errors.md`](errors.md), [`architecture.md`](architecture.md), [`ags-failure-modes.md`](ags-failure-modes.md), [`dm-queue.md`](dm-queue.md).

---

## 1. Problem & context

Indie and mid-size game studios on AccelByte Gaming Services (AGS) need a lightweight way to run closed playtests: signups, NDA gating, curated approval, key/entitlement distribution, structured feedback. Existing commercial playtest SaaS tools don't tenant-isolate per AGS namespace.

playtesthub is an open-source, self-hosted Extend application. Studios deploy the backend to their own AGS namespace, install an Admin Portal extension, and host a Svelte player app (GitHub Pages / Vercel). Player identity is Discord-federated through AGS IAM. Two distribution models per playtest: **STEAM_KEYS** (CSV passthrough, manual Steam redemption) and **AGS_CAMPAIGN** (AGS Platform Campaign API, in-game redemption). Both models share one internal code pool and state machine.

## 2. Goals / non-goals

### Goals (MVP / v1.0)
- Deliver an end-to-end golden flow: **Signup → NDA → approve → key → feedback**.
- Ship as an Extend app (Service Extension over gRPC + Extend-managed Postgres).
- Integrate with AGS IAM (Discord OAuth federation) and AGS Platform (Campaign API for the AGS_CAMPAIGN distribution model).
- Support two distribution models per playtest: **Steam Keys** (CSV upload, manual Steam redemption) and **AGS Campaign Codes** (AGS-generated codes, in-game redemption).
- Provide a studio-admin UI inside the AGS Admin Portal as an extension site.
- Provide a player-facing Svelte web app deployable to GitHub Pages / Vercel.
- Be fully open source (MIT) and reproducible on a clean AGS sandbox from the README.

### Non-goals (for MVP)
- **Identity / tenancy**: non-Discord player identity providers; multi-tenant SaaS hosting.
- **Hosting / distribution**: hosted demo instance; custom domain hosting (Extend-provided hostname only); Steam API integration; external code redemption tracking.
- **RBAC / admin tooling**: custom playtesthub-specific RBAC role (MVP gates on the built-in `ADMIN:NAMESPACE:{namespace}:EXTEND:APPUI` permission — see §6 AuthZ); bulk approve; editing a submitted survey response; observability beyond structured logs.
- **Messaging / workflows**: cross-service event bus / async workflow engine (the §5.4 DM queue is an internal implementation detail, not a product-visible async workflow); scheduled actions (the §5.5 reclaim job is an in-app leader-elected worker).
- **Data lifecycle**: self-serve "delete my data" flow; soft-delete restore UX (recovery only via direct DB intervention); multi-region data residency, GDPR tooling, SOC 2.
- **Content**: internationalization (English only); advanced survey logic (branching, conditional, file uploads); markdown rendering in `Playtest.description` (plain text in MVP; markdown on post-MVP backlog).

## 3. Target users & use cases

- **Studio admin (live ops / community / QA lead)**: accesses the tool via the AGS Admin Portal extension. Auth via existing Admin Portal session. MVP authorizes admin sessions whose IAM permission claim grants `ADMIN:NAMESPACE:{namespace}:EXTEND:APPUI` at the per-RPC action bit (held by namespace-admin roles like Game Admin / Studio Admin; §6 AuthZ, §9 R8). Use cases: create playtest (with distribution model), approve/reject applicants, manage key pool, author survey, review responses.
- **Player (external tester / community member)**: accesses a public Svelte app pointing at the studio's Extend gRPC-gateway. Auth via Discord OAuth federated through AGS IAM. Use cases: browse open playtests, sign up, accept NDA, retrieve key, submit survey.

## 4. User stories / key flows

### 4.1 Golden flow (player) — **Signup → NDA → approve → key → feedback**

1. **Landing (unauth)**: Player opens a public URL like `https://<studio>.github.io/playtesthub/#/playtest/<slug>`. Svelte app loads `config.json` at startup to discover the gRPC-gateway URL, then fetches playtest metadata via an unauthenticated gRPC-gateway call. The unauth landing view shows **only title, description, dates, and platforms** — NDA text is not shown until after Discord login.
2. **Signup — Discord login**: Player clicks "Sign up". The app kicks off an AGS IAM login using the **Discord** identity provider (federated). On return, the player has an AGS access token.
3. **Signup — form**: Player submits a short application form. Fields: Discord handle (fetched via Discord API using the player's Discord ID from AGS IAM claims; see §10 M1) and **`platforms`** — a multi-select of platforms owned (Steam / Xbox / PlayStation / Epic / Other) stored as an array of platform enum values on the `Applicant` row. No region, hours/week, or free-text pitch in MVP.
4. **NDA click-accept**: If the playtest requires an NDA, the player (now authenticated) is shown the NDA text, checks an "I agree" box, and clicks Accept. Backend stores `{userId, acceptedAt, ndaVersionHash}` immutably. If the NDA text changes after initial acceptance, the player must re-accept before proceeding — see §5.3 for the full re-accept logic.
5. **Pending state**: Player sees a "your application is under review" screen. An applicant row is created with status `PENDING`. If the admin rejects the applicant, the player sees a generic **'not selected'** message — the optional `rejectionReason` is admin-visible only (see §5.4).
6. **Approval (admin side)** — **reserve → mark GRANTED (pool-only, no external grant call)**: Studio admin opens the applicants page, reviews the queue, clicks Approve. The flow is the same for both distribution models — no AGS Platform entitlement grant RPC is called at approve time for either model. Backend performs:
   a. **Reserve (DB tx)**: atomically select one `Code` row with `state=UNUSED` for the playtest and transition it to `state=RESERVED`, setting `reservedBy=userId`, `reservedAt=now()`, `reservationTtl≈60s`.
   b. **Finalize (fenced)**: mark the code `state=GRANTED` via a fenced SQL update keyed on the original reservation identity; on 1 row affected, record `status=APPROVED`, `grantedCodeId`, `approvedAt` on the applicant **in the same DB transaction**; on 0 rows affected (reservation reclaimed and re-reserved by a different admin mid-approve), do **not** release the code, write a `code.grant_orphaned` audit at WARN, roll applicant back to `PENDING`, and return `Aborted` per [`errors.md`](errors.md). Exact SQL in [`schema.md`](schema.md).
   c. Background reclaim job releases `RESERVED` rows where `reservedAt + reservationTtl < now()`.
   d. Sends a **Discord DM** to the player with the code and instructions. DM-only in MVP. **5s timeout, no retry, non-fatal**: approval succeeds even if the DM fails. MVP ships a **single fixed DM template baked into the backend** — plain text, with the title set to the playtest title and the body containing the code and redemption instructions appropriate to the distribution model. Exact copy is TBD at implementation time.
      - **On DM success**: set `lastDmStatus='sent'`, `lastDmAttemptAt=now()`, `lastDmError=NULL`.
      - **On DM failure** (timeout, 4xx/5xx from Discord, network error, deleted Discord account, etc.): set `lastDmStatus='failed'`, `lastDmAttemptAt=now()`, `lastDmError=<truncated error>` (byte-truncation preserving valid UTF-8 codepoint boundaries) on the `Applicant` row and write an `applicant.dm_failed` row to `AuditLog` (§5.1). Drives the "DM failed" queue filter (§5.4). **DMs-disabled UX (player side)**: the DM hint is admin-surface only. If the DM fails, the player simply reloads and sees the code in the UI — no player-facing DM-failed banner. `lastDmStatus='failed'` plus `lastDmError` are admin-visible in the Applicants page.
7. **Player retrieves key**: player reloads (or clicks DM link), sees status Approved, copies the code from the UI. STEAM_KEYS → redeem on Steam (passthrough). AGS_CAMPAIGN → redeem in-game via AGS `PublicRedeemCode`. If `NdaReacceptRequired` is true, the code stays visible per §5.3; only survey submit is blocked.
8. **Play & feedback (conditional, when `Playtest.surveyId IS NOT NULL`)**: player fills out the configured survey. Responses are one-shot immutable — DB rejects further writes for the same `(playtestId, userId)`. If no survey, the flow ends at step 7.
   - **Survey discovery surfaces** (three discovery channels, all gated on `Playtest.surveyId != NULL` + the applicant being APPROVED + NDA-current). First, the **Pending page CTA**: an enabled `Submit feedback` link renders when `applicant.surveyResponseSubmittedAt` is nil and a disabled `Feedback submitted ✓` label renders once it is set — clients learn the timestamp from `GetApplicantStatus`. Second, the **approval-DM append**: every approval DM (manual `ApproveApplicant`, auto-approve, and `RetryDM`) carries a trailing survey-link line — tappable when `PLAYER_BASE_URL` is configured, non-clickable nudge otherwise. Third, the **`CreateSurvey` fan-out DM**: when an admin creates a survey for a playtest that already has an APPROVED + NDA-current cohort, the backend enqueues one standalone survey-publish DM per applicant. `EditSurvey` is silent — only the initial `CreateSurvey` event triggers the fan-out, so admins iterating on prompt copy never re-broadcast. Per-applicant idempotency lives on `applicant.lastSurveyDmId`: re-running `CreateSurvey` (or the boot-time restart sweep) is a no-op for applicants already DMed for the current survey id.

### 4.2 Dev onboarding flow (one-time, per studio)
1. Clone the repo.
2. Register a Discord OAuth app + bot token.
3. Configure AGS IAM to federate the Discord OAuth app as an identity provider.
4. Provision Extend Service Extension app in the target AGS namespace (Docker image upload via `extend-helper-cli image-upload` → `deploy-app`; Extend-managed Postgres enabled).
5. Register + upload the **Extend App UI** admin bundle: `extend-helper-cli appui create` then `extend-helper-cli appui upload` against the same namespace. (Requires Internal Shared Cloud — see §9 R11.)
6. Configure and deploy the Svelte player app (GitHub Pages / Vercel) with `config.json` alongside the static bundle.
7. Verify per README walkthrough.

**AGS_CAMPAIGN permissions**: requires Extend app IAM client to have Platform Campaign API permissions (Item, Campaign, code generation) — may require namespace-level grants. Survey authoring is optional (§5.6).

### 4.3 Code upload — STEAM_KEYS distribution model

Applies only to playtests with `distributionModel = STEAM_KEYS`.

1. Admin opens "Key pool management" and uploads a CSV of codes.
2. **CSV validation**: codes are free-form strings, trimmed of leading/trailing whitespace. Per-code rules: length 1–128, charset `[A-Za-z0-9._\-]`. File-level bounds: max 10 MB and max 50,000 codes per upload. Backend dedupes within the file AND against existing `Code` rows for the playtest. Any violation rejects the **entire upload** with a response listing offending line numbers / values; no partial import. Each rejection writes a `code.upload_rejected` audit row.
3. On success, each code becomes a `Code` row with `state=UNUSED`.

**CSV encoding**: UTF-8 only; leading BOM stripped. Non-UTF-8 → `InvalidArgument`.

**Concurrency**: dedup + insert run inside a single Postgres tx holding `pg_advisory_xact_lock(hashtext(playtestId::text))` — serializes uploads against other uploads for the same playtest. Approves do not take the advisory lock; uploads and approves interleave freely (approves use row-level locks on `Code` per §4.1 step 6).

### 4.4 Survey authoring
Admin opens Survey builder, adds typed questions (text / rating 1-5 / multi-choice) with prompt + required flag + options, reorders via drag/up-down, saves. Each save bumps the version (§5.6).

### 4.5 Reviewing responses
Admin opens Responses tab — table view, one row per respondent, columns per question, with basic aggregates (avg rating, multi-choice histograms). Row-click shows full answers. CSV export is a stretch goal; if shipped, timestamps are UTC RFC3339.

### 4.6 AGS Campaign code generation — AGS_CAMPAIGN distribution model

Applies only to playtests with `distributionModel = AGS_CAMPAIGN`.

1. **Playtest creation**: admin specifies `initialCodeQuantity`. `CreatePlaytest` always returns `status = DRAFT`; the only path out is `TransitionPlaytestStatus`.
2. **Auto-provisioning (DB tx)**: open DB tx → insert playtest row → AGS API calls → commit/rollback.
   a. Create ENTITLEMENT-type Item (derived from `title`/`description`); store as `agsItemId`.
   b. Create Campaign referencing the Item; store as `agsCampaignId`.
   c. Generate `initialCodeQuantity` codes (AGS auto-generates 6–20 char alphanumerics).
3. **Code ingestion**: fetch generated codes in `agsCodeBatchSize=1000` batches; insert each as `Code state=UNUSED` within the open DB tx. On any failure, the DB tx rolls back (playtest row and any inserted codes removed). AGS-side cleanup per `ags-failure-modes.md`. Codes orphaned on AGS recoverable via `SyncFromAGS` if the same campaign is reused.
4. **Top-up (`TopUpCodes`)**: generate-only, no DB tx (each batch inserted independently). Not idempotent. Holds `pg_advisory_xact_lock(hashtext(playtestId::text))` — same discipline as CSV upload. Admin UI provides a `SyncFromAGS` then `TopUpCodes` convenience action.
5. **`SyncFromAGS`**: fetch-only recovery action; idempotent by `UNIQUE(playtestId, value)` dedup.

**`initialCodeQuantity` bounds**: validated per §5.1 (`integer, 1–50,000`). Values outside this range are rejected with gRPC `InvalidArgument`.

**AGS failure handling (summary)**: AGS API calls use a **30s timeout, up to 3 retries with exponential backoff on HTTP 5xx or timeout; HTTP 4xx including 429 fail immediately** (429 surfaces as gRPC `ResourceExhausted`; HTTP 5xx / timeouts exhausted after 3 retries surface as gRPC `Unavailable` — see [`errors.md`](errors.md)). The initial-create generation+fetch sequence is the exception: **300s timeout, no retries, all-or-nothing**. On `CreatePlaytest` failure, the DB tx is rolled back and any pre-existing AGS resources are cleaned up (Item / Campaign deleted; failures logged WARN). HTTP 2xx with `count < requested` and no error field is treated as **partial fulfillment** — commit and warn admin. See [`ags-failure-modes.md`](ags-failure-modes.md) for the full retry policy, cleanup matrix, pagination rules, and M2 sub-cap failure matrix.

**M1 behavior**: `CreatePlaytest` with `distributionModel=AGS_CAMPAIGN` returns gRPC `Unimplemented` in M1 (the selector is visible in the admin UI but disabled). AGS_CAMPAIGN lands functionally in M2 — see §10.

### 4.7 gRPC RPC summary

| RPC | Auth | Request (key fields) | Response (key fields) | Idempotency | Milestone |
| --- | ---- | -------------------- | --------------------- | ----------- | --------- |
| GetPlaytest (unauth) | none | `slug` | title, description, platforms, dates | read-only | M1 |
| GetPlaytestForPlayer | player | `slug` | full player-visible field set incl. `ndaText`, `currentNdaVersionHash`, `surveyId` | read-only | M1 |
| GetPlaytest (admin) | admin | `playtestId` | full Playtest entity | read-only | M1 |
| ListPlaytests | admin | `namespace` | `[]Playtest` (unpaginated) | read-only | M1 |
| CreatePlaytest | admin | Playtest fields *except `status`* (defaults to DRAFT server-side), `distributionModel`, `initialCodeQuantity?` | Playtest | natural key (`namespace`, `slug`); `AlreadyExists` on slug collision within namespace | M1 (STEAM_KEYS only; `distributionModel=AGS_CAMPAIGN` returns gRPC `Unimplemented` in M1 — §4.6 / §10) |
| EditPlaytest | admin | `playtestId`, mutable fields only (`title`, `description`, `bannerImageUrl`, `platforms`, `startsAt`, `endsAt`, `ndaRequired`, `ndaText`) | Playtest | last-write-wins; `InvalidArgument` (with offending field name) on attempt to edit any immutable field | M1 |
| SoftDeletePlaytest | admin | `playtestId` | success/empty | idempotent (re-delete is no-op) | M1 |
| TransitionPlaytestStatus | admin | `playtestId`, `targetStatus` | Playtest | idempotent (transition to current status is no-op) | M1 |
| Signup | player | `playtestId`, `platforms` | Applicant | natural key (`playtestId`, `userId`) | M1 |
| AcceptNDA | player | `playtestId` | NDAAcceptance | natural key (`userId`, `playtestId`, `ndaVersionHash`) — second accept on the same tuple returns the existing `NDAAcceptance`, no error | M2 |
| GetApplicantStatus | player | `playtestId` | Applicant (own, restricted fields) | read-only | M1 |
| GetGrantedCode | player | `playtestId` | code value (if APPROVED) | read-only | M2 |
| ListApplicants | admin | `playtestId`, `statusFilter?`, cursor | paginated `[]Applicant` | read-only | M2 |
| ApproveApplicant | admin | `applicantId` | Applicant (with `grantedCodeId`) | natural key (`applicantId` — re-approve returns existing) | M2 |
| RejectApplicant | admin | `applicantId`, `rejectionReason?` | Applicant | natural key (`applicantId` — re-reject returns existing) | M2 |
| RetryDM | admin | `applicantId` | DM status | not idempotent (re-sends DM) | M2 |
| RetryFailedDms | admin | `playtestId` | bulk retry summary | not idempotent (re-sends DMs); retries every applicant with `lastDmStatus='failed'` for the playtest | M3 |
| UploadCodes | admin | `playtestId`, CSV file | upload result (count) | not idempotent; re-upload rejected on duplicate collision | M2 |
| TopUpCodes | admin | `playtestId`, `quantity` | code pool summary | not idempotent (generates new codes each call) | M2 |
| SyncFromAGS | admin | `playtestId` | code pool summary | idempotent (dedup by code value) | M2 |
| GetCodePool | admin | `playtestId` | pool stats + code list | read-only | M2 |
| CreateSurvey | admin | `playtestId`, questions | Survey | natural key (`playtestId` — first survey only) | M3 |
| EditSurvey | admin | `surveyId`, questions | Survey (new version) | not idempotent (bumps version each call) | M3 |
| GetSurvey | player | `playtestId` | Survey (current version) | read-only | M3 |
| SubmitSurveyResponse | player | `playtestId`, `surveyId`, answers | SurveyResponse | natural key (`playtestId`, `userId` — one-shot; second submit returns gRPC `AlreadyExists` with empty body) | M3 |
| ListSurveyResponses | admin | `playtestId`, `surveyId?`, cursor | paginated `[]SurveyResponse` | read-only | M3 |
| ListAuditLog | admin | `playtestId`, `actorFilter?` (`'system'` maps to `actorUserId IS NULL`), `actionFilter?`, cursor | paginated `[]AuditLog` | read-only | M3 |
| ListADTLinkages | admin | (none — studio derived from the backend's service IAM token) | `[]ADTLinkage` (identity columns only — no credential) | read-only | M5.B |
| StartADTLink | admin | (none — studio derived from the backend's service IAM token) | `{linkUrl, state}` (state is a single-use 32-byte nonce; `linkUrl` carries `state` + `studio_namespace` query params) | not idempotent (each call mints a new `state`) | M5.B |
| CompleteADTLink | admin | `state`, `adt_namespace` | `ADTLinkage` | natural key (`state` — single-use; replay returns `InvalidArgument`) | M5.B |
| UnlinkADT | admin | `adtLinkageId` | success/empty | idempotent (re-unlink is no-op); best-effort propagates the unlink to ADT (`adt.Client.DeleteLinkage`) in the same flow — failures are logged + counted on `ADTUnlinkADTSideFailures` but do not block the local soft-delete (PRD §4.8). | M5.B |
| RecoverADTLinkage | admin | `adtNamespace` | `ADTLinkage` | not idempotent (single-use orphan adoption); rejected with `AlreadyExists` when a live row for the pair already exists | M5.B |
| ListADTBuilds | admin | `adtLinkageId`, `adtGameId` | `[]Build` (proxied through `adt.Client`) | read-only | M5.B |
| ListADTGames | admin | `adtLinkageId` | `[]Game` (proxied through `adt.Client.ListGames`; drives the create-playtest build-picker top-level dropdown — see [`STATUS_M5.md`](STATUS_M5.md) "Addendum 2026-05-21 — games-list endpoint") | read-only | M5.B |
| ChangeADTBuild | admin | `playtestId`, `adtGameId`, `adtBuildId` | Playtest | repoints an ADT playtest at a new `(adtGameId, adtBuildId)` pair under its existing `adtNamespace` (immutable here — see §4.8.6); verifies the pair against the linkage via the same `adt.Client.ListBuilds` round-trip `CreatePlaytest` runs before persisting; `FailedPrecondition` on a non-ADT playtest | M5.C |
| GetADTDownloadInfo | player | `playtestId` | `{url, expiresAt?, source ('issued'|'fallback')}` (gated on `applicant.status='APPROVED'` exactly like `GetGrantedCode`) | read-only (each call may mint a fresh URL per ADT semantics) | M5.B |
| GetPlaytestParticipants | admin | `playtestId`, `statusFilter?` | `[]ParticipantRow` (joins applicant + the latest `dm.sent` audit row for STEAM_KEYS / AGS_CAMPAIGN to derive `codeSentAt`; the four nullable ADT telemetry cache fields are returned but stay NULL/zero across M5.C — telemetry refresh deferred to M6) | read-only | M5.C |
| CreateAnnouncement | admin | `playtestId`, `sendToFilter` (`ALL` / `APPROVED_ONLY` / `PENDING_ONLY`), `subject`, `message` | `Announcement` (recipients resolved at call time, NOT a stored snapshot; fan-out enqueues `dm_outbox` rows reusing the M2 RetryDM machinery) | not idempotent (re-call sends a new broadcast) | M5.C |
| ListAnnouncements | admin | `playtestId` | `[]Announcement` (history; `status` aggregated from `announcement_recipient.dm_status`: `SENT` if all sent, `PARTIAL` if mixed, `SENDING` if any still queued, `FAILED` if all failed) | read-only | M5.C |

### 4.8 ADT distribution model — linking flow, per-build URL resolution, fallback

Applies only to playtests with `distributionModel = ADT` (M5.B).

ADT (AccelByte Development Toolkit) playtests distribute an in-development build rather than a redemption code. There is **no code pool**: the deliverable is a download URL minted by ADT (preferred) or a static URL the operator configures on the playtest row (fallback). The grant primitive at approve time is "resolve a URL" not "reserve a code".

#### 4.8.1 Linkage scope and identity derivation

- **Per-studio linkage, not per-playtest, not per-game-namespace.** One `adt_linkage` row covers every game namespace and every playtest under a studio. Keyed `(studio_namespace, adt_namespace)`.
- **`studio_namespace` is derived server-side from the playtesthub backend's own AGS service IAM JWT** as `token.union_namespace ?? token.namespace`, NOT from the calling admin's request token. The backend mints the service JWT via the existing client-credentials grant (`AGS_IAM_CLIENT_ID` / `AGS_IAM_CLIENT_SECRET` against `${AGS_BASE_URL}/iam/v3/oauth/token`) and decodes the claims; this is the same token ADT sees on every downstream API call, so keying linkage rows on the service token's claims is the only way for the playtesthub-side `adt_linkage` row to match ADT's `(adt_namespace, studio_namespace) linked = true` flag in all cases. The admin UI never composes the value; the backend embeds it in the `linkUrl` returned from `StartADTLink`. Studio-namespace service tokens carry `namespace` equal to the studio namespace; game-namespace service tokens always carry `union_namespace`. A service token with neither claim is rejected at `StartADTLink` with `FailedPrecondition` per [`errors.md`](errors.md).

#### 4.8.2 Linking signal — state-bearing redirect, no `grantCode` exchange

1. Admin clicks **Link ADT Namespace** in the admin UI (Playtests list page, not Create form — linkage is studio-scoped so it lives outside any single playtest).
2. Admin UI persists any open form draft to `sessionStorage`, calls `StartADTLink`. Backend generates a 32-byte `state` nonce, persists `adt_link_pending(state, studio_namespace, started_by, expires_at)` (TTL = `ADT_LINKAGE_PENDING_TTL_SECONDS`), returns `{linkUrl, state}`.
3. Admin UI assigns `window.location.href = linkUrl` where `linkUrl = ${ADT_BASE_URL}/oauth/link?state=…&redirect_uri=${ADT_REDIRECT_BASE_URL}/adt-link-callback&studio_namespace=${studio_namespace}`. `studio_namespace` is **mandatory** in the query string — ADT keys its side's linkage flag on `(adt_namespace, studio_namespace)`.
4. ADT performs its own sign-in + namespace picker, records `(adt_namespace, studio_namespace) linked = true` on its side, then redirects to `${ADT_REDIRECT_BASE_URL}/adt-link-callback?state=…&result=success&adt_namespace=<picked>` (or `result=failed&reason=…` on user cancel / ADT-side error).
5. The admin UI callback route reads `state` + `result` + `adt_namespace` from the URL and calls `CompleteADTLink(state, adt_namespace)`. Backend validates the `adt_link_pending` row matches and has not expired, inserts an `adt_linkage` identity row, deletes the consumed pending row, writes an `adt_linkage.create` audit row. **No outbound call to ADT is made** — `adt_namespace` is echoed on the redirect-back URL precisely so the linkage commit is a one-sided DB write.
6. Restored form draft (from step 2's `sessionStorage`) is rehydrated by the callback route on success.

**Why echoing `adt_namespace` on the redirect URL is safe**: `state` defends CSRF (single-use, server-bound, short TTL). Operator-browser tampering with `adt_namespace` is self-defeating because the first downstream ADT API call (e.g. `ListBuilds` at `CreatePlaytest`, or `IssueDownloadURL` at approve) surfaces `FailedPrecondition` — ADT's flag for `(adt_namespace_tampered, studio_namespace)` is missing and the call returns 4xx. Failure is fast and loud, never silent.

**No credential, no token, no `grantCode` is exchanged at any step.** Every subsequent ADT API call is authed by playtesthub minting a fresh AGS service IAM JWT (existing `AGS_IAM_CLIENT_ID` / `AGS_IAM_CLIENT_SECRET` via `pkg/ags`) and sending it as `Authorization: Bearer …`. ADT validates the JWT against AGS IAM JWKS and derives studio identity from `iss` + `union_namespace` claims. Consequence: no `adt_credential_*` columns, no KEK env var, no credential rotation step. Rotation happens automatically via the AGS IAM client-credentials grant.

#### 4.8.3 Build URL strategy at approve time

Per the 2026-05-20 ADT API spec (see [`STATUS_M5.md`](STATUS_M5.md) §"Open questions"), ADT issues **per-build** download URLs — not per-applicant. Every approved applicant for a given playtest receives the same URL; ADT bounds it with a fixed 24-hour CDN TTL. Per-applicant audit attribution lives on the playtesthub side (`applicant.approve` row carries the applicant id + URL); per-applicant revocation is not available via ADT — `RejectApplicant` is the revocation primitive (cuts off `GetADTDownloadInfo` access on the playtesthub side; the URL itself stays valid for the TTL).

`ApproveApplicant` against an ADT playtest:
- Skips code reservation entirely (no `Code` row).
- Calls `adt.Client.IssueDownloadURL(adtNamespace, adtGameId, adtBuildId)` against `GET <ADT_BASE>/profiling/namespaces/<adt_namespace>/agsplaytesthub/games/<adt_game_id>/builds/<adt_build_id>/downloadUrls?limit=20`. ADT returns `{urls: [...], expiresAt}`; playtesthub surfaces the **full URL list** in ADT's original order so multi-asset builds (game binary + patcher + manifest, etc.) round-trip without data loss. Single-file builds map to a single-element list.
- On ADT `401` (linkage flag missing or revoked — could be operator-side unlink on ADT or a `union_namespace` claim shift): returns `FailedPrecondition` ("adt linkage no longer exists or service token rejected, re-link required"); applicant stays `PENDING`.
- On ADT 4xx/5xx with the linkage row still present: falls back to a single-element list containing `playtest.adtFallbackDownloadUrl` if set (audit records `{adtUrlSource: 'fallback'}`). Otherwise returns `Unavailable`; applicant stays `PENDING`.
- DM body shape (ADT): single-URL builds → `Download your playtest build for "<title>": <url>` (one line); multi-URL builds → the same heading followed by `1) <url>` / `2) <url>` / … one URL per line. See [`dm-queue.md`](dm-queue.md) §"DM body shape — ADT distribution" for examples. `RetryDM` re-mints fresh URLs (the prior 24h TTL may have expired).
- The `applicant.approve` audit row carries `{adtUrls, adtUrlSource}` where `adtUrls` is a JSON array of every URL minted at approve time and `adtUrlSource ∈ {'issued', 'fallback'}`. **URLs are not redacted** — URLs ≠ codes, and forensics require the URL list. The §6 Observability log-redaction rule applies to code values only.

Auto-approve (§5.4) works for ADT identically to STEAM_KEYS / AGS_CAMPAIGN — the signup-time chain into `ApproveApplicant` hits the ADT branch instead of the pool branch. The `applicant.auto_approved` audit row is emitted regardless of distribution model; for ADT the row's `codeId` field is NULL (no `Code` row to reference).

#### 4.8.4 Player retrieval surface

For ADT playtests, the player UI calls `GetADTDownloadInfo(playtestId)` instead of `GetGrantedCode(playtestId)`. The RPC is gated on `applicant.status='APPROVED'` exactly like `GetGrantedCode` — soft-deleted playtests return `NotFound`, unapproved callers cannot probe the distribution model via the RPC surface (§6 Security). Response carries `{urls, expiresAt?, source}` where `urls` is the ADT-minted URL list (single-element list for single-file builds, multi-element for multi-asset builds, or the single-element static-fallback list) and `source ∈ {'issued', 'fallback'}` so the UI can render an expiry banner when ADT provides one, render one tappable link per URL for multi-asset builds, and label fallback URLs as "shared playtest download (operator-managed)" for the static path.

#### 4.8.5 What ADT playtests do not have

- **No code pool**: `GetCodePool`, `UploadCodes`, `TopUpCodes`, `SyncFromAGS` on an ADT playtest return `FailedPrecondition` per [`errors.md`](errors.md). See §5.5.
- **No `initialCodeQuantity`**: rejected at `CreatePlaytest` with `InvalidArgument` if set on an ADT playtest. Symmetrically, an `adt_*` field set on a non-ADT playtest is also rejected.
- **No AGS Campaign / Item provisioning**: `agsItemId` / `agsCampaignId` stay NULL.

#### 4.8.6 ChangeADTBuild — swapping the distributed build

`ChangeADTBuild(playtestId, adtGameId, adtBuildId)` repoints an existing ADT playtest at a different build without recreating it. The linked `adtNamespace` is **immutable** here — it is the studio-linkage scope keyed `(studio_namespace, adt_namespace)` (§4.8.1), so re-pointing it is a relink (unlink + link), not a build change; only the `(adtGameId, adtBuildId)` pair under that same namespace moves. The new pair is verified against the linkage via the **same `adt.Client.ListBuilds` round-trip `CreatePlaytest`'s ADT branch runs** before the row is persisted — a build absent from `(adt_namespace, adt_game_id)` returns `InvalidArgument`, an absent linkage returns `FailedPrecondition`, and an ADT `401` returns `FailedPrecondition` ("re-link required"), all per [`errors.md`](errors.md). Calling it on a non-ADT playtest returns `FailedPrecondition`.

Because ADT URLs are minted at approve time (§4.8.3), changing the build is **not retroactive**: already-approved applicants keep the download URL already DM'd (ADT does not revoke prior per-build URLs; they stay valid for their CDN TTL), while future approvals and `RetryDM` re-mint against the new build. The change writes a `playtest.adt_build_change` audit row recording the before/after game+build ids (see [`schema.md`](schema.md)). The admin UI surface is the **Change Build** action on the Distribution tab (§5.7, M5.C).

## 5. Functional requirements

### 5.1 Playtest CRUD
- Fields: `id`, `namespace`, `slug`, `title` (max 200 chars), `description` (**plain text in MVP — stored raw, rendered as pre-wrap plain text by the player app; markdown rendering is post-MVP backlog**; max 10,000 chars), `bannerImageUrl` (https-only URL, max 2,048 chars; backend never stores image binaries — XSS mitigated by scheme allow-listing, no further sanitization), `platforms` (`TEXT[]` of `STEAM | XBOX | PLAYSTATION | EPIC | OTHER`; playtests targeted), `startsAt`, `endsAt` (UTC; **drive automatic `DRAFT → OPEN → CLOSED` transitions** when set — see "Window-driven auto-transition" below; `endsAt > startsAt` enforced at create/edit when both set), `status` (`DRAFT | OPEN | CLOSED`), `ndaRequired` (bool), `ndaText`, `currentNdaVersionHash`, `surveyId` (nullable), `distributionModel` (`STEAM_KEYS | AGS_CAMPAIGN | ADT`; immutable after creation), `agsItemId` (nullable, AGS_CAMPAIGN only), `agsCampaignId` (nullable, AGS_CAMPAIGN only), `initialCodeQuantity` (nullable `INTEGER` 1–50,000; required for AGS_CAMPAIGN; NULL for STEAM_KEYS and ADT), `adtNamespace` (nullable `TEXT`, ADT only — the studio's linked ADT namespace; required for ADT, NULL otherwise), `adtGameId` (nullable `TEXT`, ADT only — the ADT-side game identifier; required for ADT, NULL otherwise), `adtBuildId` (nullable `TEXT`, ADT only — the specific ADT build distributed by this playtest; required for ADT, NULL otherwise), `adtFallbackDownloadUrl` (nullable `TEXT`, ADT only — static https URL used when ADT cannot mint a download URL at approve time; see §4.8.3), `autoApprove` (`BOOLEAN NOT NULL DEFAULT FALSE` — when true, signup auto-grants up to `autoApproveLimit` applicants; see "Auto-approve" in §5.4), `autoApproveLimit` (nullable `INTEGER` 1–100,000; required when `autoApprove=true`; NULL when `autoApprove=false`), `createdAt`, `updatedAt`, `deletedAt` (nullable).
- **`slug`**: admin-chosen, server-validated against `^[a-z0-9][a-z0-9-]{2,63}$` (3–64 chars). Unique per namespace. Input that does not match the regex is rejected with `InvalidArgument`; the server does not silently sanitize. Slug reuse after soft-delete is blocked (uniqueness enforced across live and soft-deleted rows).
- **`namespace` ↔ `AGS_NAMESPACE`**: `Playtest.namespace` rows are populated from the `AGS_NAMESPACE` env var at insert time. No per-request override.
- **`EditPlaytest` whitelist**: editable — `title`, `description`, `bannerImageUrl`, `platforms`, `startsAt`, `endsAt`, `ndaRequired`, `ndaText`, `autoApprove`, `autoApproveLimit`, `adtFallbackDownloadUrl`. Immutable — `slug`, `namespace`, `status` (use `TransitionPlaytestStatus`), `distributionModel`, `initialCodeQuantity`, all `ags*` IDs, `adtNamespace`, `adtGameId`, `adtBuildId`, all timestamps. Editing an immutable field returns `InvalidArgument` with the offending field name. Editing `ndaText` recomputes `currentNdaVersionHash` and triggers the re-accept flow per §5.3. Editing `autoApprove` / `autoApproveLimit` mid-playtest is intentionally allowed so operators can raise / lower the cap or flip the toggle off without recreating the playtest — the cap rule from §5.4 ("counts existing `auto_approved=true` applicants") makes a lowered cap a no-op for already-auto-approved applicants but stops further auto-grants until the count drops back below the new limit. Editing `adtFallbackDownloadUrl` mid-playtest is intentionally allowed so operators can repoint the fallback at a new static build URL without recreating the playtest (`adtNamespace` stays immutable — it is the studio-linkage scope, mirroring `distributionModel` / `agsItemId`; re-pointing it is a relink, not a build change). `adtGameId` and `adtBuildId` are **no longer changed via `EditPlaytest`** — they are mutable only via the dedicated `ChangeADTBuild` RPC (§4.8.6), which re-verifies the new build against the linkage before persisting. Repointing the build does not retroactively re-mint URLs: already-approved applicants keep the download URL already DM'd; future approvals + `RetryDM` re-mint against the new build (URLs are minted at approve time per §4.8.3). All edits write `AuditLog`.
- **Status transitions (strict linear)**: `DRAFT → OPEN → CLOSED` only. `DRAFT → CLOSED` is invalid, rejected with `FailedPrecondition`. No reopen. Per-status RPC allowance is Table A below.
- **Window-driven auto-transition**: when `startsAt` and/or `endsAt` are set, a background worker (`internal/window/`, leader-leased, `WINDOW_TICK_SECONDS` default 60s — §5.9) advances status at each boundary:

  | `startsAt` | `endsAt` | Behavior |
  | --- | --- | --- |
  | set | set | auto `DRAFT → OPEN` at `startsAt`, then auto `OPEN → CLOSED` at `endsAt` |
  | set | NULL | auto `DRAFT → OPEN` at `startsAt`; manual close only |
  | NULL | set | manual open only; auto `OPEN → CLOSED` at `endsAt` |
  | NULL | NULL | fully manual (status driven entirely by `TransitionPlaytestStatus`) |

  Auto-transitions are **monotonic forward-only** and follow the same `DRAFT → OPEN → CLOSED` linear rule as manual `TransitionPlaytestStatus`; the worker never reverts and never skips a state. Each auto-flip writes a `playtest.status_transition` row to `AuditLog` with `actorUserId = NULL` (system-emitted; see [`schema.md`](schema.md)). Admin manual transitions always win against the worker — the worker's CAS predicate matches only the pre-window status, so an admin who flips to OPEN at T=10 (despite `startsAt=T=20`) is preserved; the worker just sees status=OPEN at T=20 and skips. Same for early manual CLOSE. **`endsAt` auto-close does NOT gate survey submit** — APPROVED applicants can still submit surveys post-CLOSED per §5.6 (auto-close only affects what status-gated RPCs already check via Table A).
- **Concurrent `EditPlaytest` + `TransitionPlaytestStatus`**: both hold a row-level lock on the Playtest row; last-committed wins.
- **Soft-delete**: sets `deletedAt`; hides from all list views (public and admin) and returns `NotFound` on direct link for all player RPCs. One-way and final — no restore UX ever ships; recovery only via direct DB intervention. Underlying rows (applicants, NDA acceptances, codes, survey, responses) are preserved intact. PENDING applicants remain PENDING indefinitely. Immediate: in-flight signup / NDA-accept fails with `NotFound`.
- **`ndaText` when `ndaRequired = false`**: allowed but ignored end-to-end.
- **Visibility**: unauth `GetPlaytest` returns `NotFound` for DRAFT, CLOSED, or soft-deleted playtests (indistinguishable from non-existent). Authenticated `GetPlaytestForPlayer` returns a CLOSED playtest only to already-approved players; others get `NotFound`. DRAFT is `NotFound` for every player RPC.

**`AuditLog` entity** (append-only; no edit/delete RPCs). Fields: `id`, `namespace`, `playtestId` (nullable for namespace-scoped events), `actorUserId` (nullable for system-emitted events), `action`, `before` (JSONB), `after` (JSONB), `createdAt`. Full column types, required indexes, the `action` enum, and the JSONB payload shapes are in [`schema.md`](schema.md). DM-event rows (`applicant.dm_sent`, `applicant.dm_failed`) carry DM attempt metadata rather than entity diffs; NDA-edit rows store full old+new `ndaText` in `before`/`after`.

**Permission matrix — Table A (admin RPC allowance by playtest status)**. Canonical; audit-event rules are `rpc blocked → corresponding audit action cannot fire`.

| Admin RPC                                         | DRAFT | OPEN | CLOSED |
| ------------------------------------------------- | :---: | :--: | :----: |
| `playtest.edit`                                   | yes   | yes  | yes    |
| `nda.edit`                                        | yes   | yes  | **no** |
| `playtest.soft_delete`                            | yes   | yes  | yes    |
| `playtest.status_transition`                      | yes   | yes  | n/a    |
| `applicant.approve` / `applicant.reject`          | **no**¹ | yes  | **no** |
| RetryDM / RetryFailedDms                          | n/a   | yes  | yes    |
| `code.upload` (STEAM_KEYS)                        | yes   | yes  | **no** |
| `code.generate/top-up/sync` (AGS_CAMPAIGN)        | yes *CreatePlaytest only* | yes *top-up/sync* | **no** |
| `survey.create` / `survey.edit`                   | yes   | yes  | yes    |

¹ Signup is impossible in DRAFT (per §5.1 visibility: DRAFT is `NotFound` for every player RPC), so no applicants exist in DRAFT and the approve/reject "no" entries are defensive-only rules. The server still enforces them and surfaces `FailedPrecondition` per [`errors.md`](errors.md).

Audit-event attribution (admin vs system) is defined in [`schema.md`](schema.md) alongside the action enum.

**Accountability note**: admin RBAC is a single coarse namespace-admin permission (`ADMIN:NAMESPACE:{namespace}:EXTEND:APPUI` — §6 AuthZ, §9 R8) rather than a custom `PLAYTEST_ADMIN` role, so the audit log is the per-actor accountability layer. Every admin-mutating action writes an `AuditLog` row. Retention is indefinite with no size cap in MVP (§9 R9).

**Soft-delete UX**:
- Hidden entirely from the player app — absolute, overrides all other state. Soft-deleted slugs return `NotFound` (indistinguishable from never-existed). Includes previously-APPROVED players on CLOSED-then-soft-deleted playtests.
- **AGS_CAMPAIGN caveat**: AGS Item and Campaign remain alive on the AGS side after soft-delete. Studios clean up manually if needed.

### 5.2 Signup (unauth landing + Discord login)
- Public landing page fetches playtest by slug via unauth RPC. **Unauth view fields**: `title`, `description`, `bannerImageUrl`, `platforms`, `startsAt`, `endsAt` only. `ndaText` is gated behind authenticated `GetPlaytestForPlayer`.
- "Sign up" triggers AGS IAM login (Discord IdP). On IAM 5xx/network error: generic "Login failed — please try again later" (no special outage detection).
- Authenticated signup RPC creates an `Applicant` row. **Admin-visible fields** (all): status, grantedCodeId, approvedAt, rejectionReason, lastDmStatus, lastDmAttemptAt, lastDmError, discordHandle, platforms, ndaVersionHash, createdAt, plus identity fields. **Player-visible fields** (own row only): status, grantedCodeId (presence only — value via `GetGrantedCode`), approvedAt, ndaVersionHash. Rejection reason, DM state, and Discord handle are never returned to the player. Full column types in [`schema.md`](schema.md).
- No `WITHDRAWN` state and no player self-cancel in MVP.
- `platforms` (owned by applicant) is the only user-supplied form field beyond Discord handle. **Applicant-owned platforms are collected for admin triage only; they are not validated against `Playtest.platforms`.** A player may sign up for a Steam-only playtest while only owning Xbox — this is intentional (admin decides).
- **Derived state `NdaReacceptRequired`**: `applicant.ndaVersionHash IS DISTINCT FROM playtest.currentNdaVersionHash`. See §5.3.
- Idempotent: second signup returns existing applicant.
- Deleted Discord accounts: `discordHandle` is archival; no reconciliation. DM failures surface naturally via `lastDmStatus='failed'`.

### 5.3 NDA click-accept with versioning
- Per-playtest NDA text. On save, backend computes `currentNdaVersionHash = sha256(normalize(ndaText))`.
- **Normalization before hashing**: trim trailing whitespace per line, CRLF → LF, collapse trailing newlines to a single terminal LF. Hash is over the UTF-8 bytes of the result. Cosmetic whitespace changes don't bump the hash.
- `NDAAcceptance`: `{userId, playtestId, ndaVersionHash, acceptedAt}` with composite PK `(userId, playtestId, ndaVersionHash)`. Append-only; no IP/UA field (PII minimization).
- **Version change forces re-accept**: `applicant.ndaVersionHash IS DISTINCT FROM playtest.currentNdaVersionHash` → applicant must re-accept (handles initial NULL). While in this state survey submit is blocked; previously `GRANTED` codes stay visible.
- **Client detection of re-accept state**: clients compute `NdaReacceptRequired` by comparing `GetPlaytestForPlayer.currentNdaVersionHash` with `GetApplicantStatus.ndaVersionHash` — when they differ (including the initial `NULL` case), the player must re-accept before submitting the survey.
- NDA edits disallowed in CLOSED (see Table A). When `ndaRequired = false`, any persisted `ndaText` is ignored end-to-end.

### 5.4 Applicant queue with approve/reject
- Paginated list, filterable. Filter set: `PENDING | APPROVED | REJECTED | DM_FAILED`. `DM_FAILED` is derived (`status=APPROVED AND lastDmStatus='failed'`); persisted `Applicant.status` is `PENDING | APPROVED | REJECTED`.
- **Approve**: reserve → fenced finalize (§4.1 step 6) + DM (5s timeout, non-fatal).
- **Reject**: marks `status=REJECTED` with optional admin-visible `rejectionReason` (player sees generic "not selected"). REJECTED is terminal; re-approve attempt returns `FailedPrecondition` (`"applicant is rejected and cannot be re-approved"`). Only valid transitions: `PENDING → APPROVED`, `PENDING → REJECTED`.
- **Concurrent approve on same applicant**: first wins; second returns `FailedPrecondition` (`"applicant already approved"`). Enforced by DB CAS on `Applicant.status`.
- **Empty pool**: returns `ResourceExhausted` with model-specific message (see [`errors.md`](errors.md)). Applicant stays `PENDING`; admin restocks and retries.
- **Approve/Reject blocked in CLOSED**: see Table A; exact code/message in [`errors.md`](errors.md).
- **DM queue (summary)**: bounded in-memory FIFO (default 10k). Overflow + restart loss + circuit-open surface as `lastDmStatus='failed'` with distinct `lastDmError` reasons (`dm_queue_overflow`, `lost_on_restart`, `dm_circuit_open`). Circuit breaker pauses queue on 50 consecutive failures within 60s for 5 minutes (auto-resume); approves still enqueue while tripped. Restart sweep is idempotent (re-marks only `lastDmStatus IS NULL` or `'pending'`; preserves prior `'failed'` reason). Admins triage via "DM failed" filter + per-applicant Retry DM or `RetryFailedDms` bulk RPC. See [`dm-queue.md`](dm-queue.md).
- **Retry DM**: re-attempts DM without re-granting a code. On success: flips `lastDmStatus='sent'` and writes `applicant.dm_sent` audit row. On failure: another `applicant.dm_failed`. **No cooldown; double-click will send two DMs.**
- **`RetryFailedDms` admission control** (bulk retry): walks every applicant with `lastDmStatus='failed'` for the playtest and enqueues each into the DM queue — the **same enqueue path as approve**, respecting the 10k cap and the configured drain rate. On overflow, the affected applicants stay `lastDmStatus='failed'` with `lastDmError='dm_queue_overflow'` (identical handling to approve-time overflow).
- Low-water banner (≤10% remaining) surfaces on this page (canonical definition in §5.5).

**Auto-approve** (M5.A; opt-in per playtest via `autoApprove` / `autoApproveLimit` on the Playtest row — see §5.1):

- **Distribution-model-agnostic**: works for `STEAM_KEYS`, `AGS_CAMPAIGN`, and `ADT` (the third model added in M5.B; see §4.8). For STEAM_KEYS / AGS_CAMPAIGN the auto-approve path reuses the same reserve → fenced finalize primitives that manual `ApproveApplicant` uses (§4.1 step 6); every approve-time invariant — pool reservation, fenced finalize against the original reservation identity, CAS on `Applicant.status` — applies identically. For ADT the auto-approve path hits the ADT branch of `ApproveApplicant` instead: no `Code` reservation, instead a `adt.Client.IssueDownloadURL` call (or static fallback) per §4.8.3. The CAS on `Applicant.status` and the system-attributed `applicant.auto_approved` audit row apply for all three models.
- **Cap semantics**: `autoApproveLimit` bounds **auto-approvals only**. Manual `ApproveApplicant` against a `PENDING` applicant is unaffected and **uncapped** — an admin who needs to admit one more applicant after the cap is hit can still do so. The `auto_approved BOOLEAN NOT NULL DEFAULT FALSE` column on `Applicant` (see [`schema.md`](schema.md)) is the unambiguous count source: the cap predicate is `count(applicants where playtest_id=$1 AND auto_approved=true) < autoApproveLimit`. Manually-approved applicants do not consume the cap.
- **Concurrency**: the signup handler chains into the auto-approve path inside the signup transaction under `pg_advisory_xact_lock(hashtext('autoapprove:' || playtestId))` so the cap-count → grant pair is atomic per playtest. The advisory lock is playtest-scoped (does not serialize across the namespace); the existing CAS predicate on `Applicant.status` is the second line of defense if the lock is ever bypassed (e.g. by future direct DB writers).
- **Pool-empty fallback (STEAM_KEYS / AGS_CAMPAIGN)**: when the pool is empty (`ResourceExhausted` from `Code.Reserve`) the auto-approve attempt **silently falls back to `PENDING`**: signup still returns success, the applicant row is inserted with `status=PENDING` + `auto_approved=false`, and the admin restocks the pool then either re-auto-approves the next signup burst or manually approves the stragglers. **No signup-time error** is surfaced to the player and no `applicant.auto_approved` audit row is written for the failed attempt.
- **URL-resolution fallback (ADT)**: when `adt.Client.IssueDownloadURL` returns 4xx/5xx and the playtest has no `adtFallbackDownloadUrl`, the auto-approve attempt also silently falls back to `PENDING` with the same shape — signup succeeds, applicant row stays `PENDING` + `auto_approved=false`, no `applicant.auto_approved` audit row. The admin re-links the ADT namespace (or sets a fallback URL) and the next signup burst retries. ADT `401` ("linkage gone") is treated identically.
- **Interaction with `RetryFailedDms`**: an auto-approve attempt that fails because the pool was empty leaves the applicant in `PENDING`, not in `lastDmStatus='failed'`. **`RetryFailedDms` is the wrong recovery** — it only walks DM-failed applicants. The recovery for pool-empty auto-approve misses is "restock the pool then manually approve, or wait for the next signup to retry auto-approve". The auto-approve path itself never enqueues a DM until the underlying reserve → finalize succeeds, so the DM queue overflow / circuit breaker rules from this section apply unchanged (a DM that fails post-auto-approve still lands on the same `lastDmStatus='failed'` path).
- **Audit attribution**: a successful auto-approve emits one `applicant.auto_approved` row (system-attributed; `actorUserId = NULL`) — a distinct action from `applicant.approve` so audit-log filters can cleanly separate manual vs auto attribution. See [`schema.md`](schema.md) for the JSONB payload shape.

**Bulk announcements** (M5.C; admin-authored DM broadcast addressed at one playtest's applicant set):

- **Surface**: `CreateAnnouncement(playtestId, sendToFilter, subject, message)` returns a new `Announcement` row scoped to the playtest. `sendToFilter ∈ {ALL, APPROVED_ONLY, PENDING_ONLY}` is resolved at call time against the current applicant set — adding an applicant after the broadcast does NOT auto-include them. `ListAnnouncements(playtestId)` returns history rows; per-row `status` is aggregated from `announcement_recipient.dm_status` (`SENT` / `SENDING` / `PARTIAL` / `FAILED`).
- **Fan-out**: one `announcement_recipient` row per matched applicant + one `dm_outbox` row enqueued through the existing M2 RetryDM machinery (same 10k cap, same circuit breaker, same drain rate). Discord rate-limit handling is identical to the approve DM path; a per-recipient failure is captured on `announcement_recipient.dm_error_code` rather than the applicant-level `lastDmError`.
- **PII / observability**: `subject` and `message` are treated as PII-sensitive per §6 Observability — they are NEVER written to structured logs, metrics, or audit JSONB. The `announcement.create` audit row carries `{announcementId, playtestId, sendToFilter, recipientCount, createdBy}` only (admin-attributed). Forensic recovery of the per-applicant delivery state lives entirely on `announcement_recipient`, not in audit / logs.
- **Closed-playtest write block**: `CreateAnnouncement` on a CLOSED playtest returns `FailedPrecondition` per [`errors.md`](errors.md). Reading history (`ListAnnouncements`) is always allowed regardless of status.
- **Bounds**: `subject` 1–`ANNOUNCEMENT_MAX_SUBJECT_LEN` chars (default 200); `message` 1–`ANNOUNCEMENT_MAX_MESSAGE_LEN` chars (default 4,000 — Discord DM is hard-capped at 2,000 chars per message, but the form cap is 4,000 for editorial flexibility and the fan-out auto-chunks at the Discord boundary). Empty subject / empty message rejected with `InvalidArgument` per [`errors.md`](errors.md).
- **Retention**: announcements are kept forever — row sizes are tiny and announcements are forensically valuable for compliance reviews. No cleanup worker in M5.C.

**Code Sent Date — derived field** (M5.C; participants surface):

- The admin participants table (M5.C `GetPlaytestParticipants`) surfaces a `codeSentAt` timestamp for STEAM_KEYS / AGS_CAMPAIGN applicants. This is **derived at read time** from `applicant.last_dm_attempt_at` when `applicant.last_dm_status='sent'` — there is no new `applicant.code_sent_at` column. The applicant row already carries the latest DM attribution (PRD §5.4 "DM queue"); reading off that pair captures both the initial-approve DM and any subsequent successful `RetryDM`. Pending / failed DMs leave `codeSentAt` NULL.
- ADT-distribution applicants have NULL `codeSentAt` in M5.C. The analogue is the per-applicant **Download Date** sourced from ADT telemetry — fully deferred to M6 alongside the four nullable applicant cache columns (`adtDownloadAt`, `adtTotalPlaytimeSeconds`, `adtHardwareSpecs`, `adtCrashCount`; see §5.1 and [`schema.md`](schema.md)). The columns ship dormant in M5.C so M6 lands the worker + endpoint hookup with zero schema churn.

### 5.5 Code pool — entity & state machine

The `Code` entity is the authoritative per-playtest pool for the two pool-backed distribution models (STEAM_KEYS via CSV §4.3; AGS_CAMPAIGN via AGS API §4.6). **ADT playtests have no code pool** — the grant is a download URL minted at approve time, not a code reserved from a pre-staged set (§4.8). `GetCodePool`, `UploadCodes`, `TopUpCodes`, and `SyncFromAGS` on an ADT playtest return `FailedPrecondition` per [`errors.md`](errors.md); the admin UI Key Pool page (§5.7) renders an ADT empty-state instead of a code list. Schema in [`schema.md`](schema.md).

State machine (reserve → finalize → reclaim per §4.1 step 6):
- `UNUSED → RESERVED` (approve tx; sets `reservedBy`, `reservedAt`).
- `RESERVED → GRANTED` (successful fenced finalize; sets `grantedAt`). Terminal.
- `RESERVED → UNUSED` (0-row fenced update, or reclaim job when `reservedAt + reservationTtl < now()`).

**Reservation TTL & reclaim cadence**: `reservationTtl = 60s` (env `RESERVATION_TTL_SECONDS`), `reclaimInterval = 30s` (env `RECLAIM_INTERVAL_SECONDS`). Reclaim job uses DB-backed leader election (`leader_lease`) for multi-replica safety. Liveness signal: each tick emits `{event:"reclaim_tick", released:N, leaseHolder:instanceId}` INFO log line.

**Leader-lease policy**: TTL 30s (env `LEADER_LEASE_TTL_SECONDS`), heartbeat 10s (env `LEADER_HEARTBEAT_SECONDS`). Worst-case handoff gap is ~30s (one lease TTL). The 30s reclaim cadence may therefore skip one tick during a handoff — this is acceptable because `reservationTtl = 60s` is strictly longer than the worst-case gap, so a reservation cannot expire unnoticed across a handoff.

**Reclaim DB-error backoff**: on DB errors the reclaim tick logs at WARN and skips; the next tick retries at the normal 30s cadence. **No exponential backoff; no circuit breaker.**

**Low-water banner**: when remaining UNUSED codes ≤10% of total pool, surface a banner on Key Pool page and Applicants page (point-of-use). No audit row, no DM, no email — UI signal only.

### 5.6 Survey builder (text / rating / multi-choice) + response storage
- Surveys are optional per playtest (`Playtest.surveyId IS NOT NULL` gates §4.1 step 8). Entity schemas in [`schema.md`](schema.md).
- **Every edit bumps the version**: edit creates a new `Survey` row with `version = previous + 1`. `Playtest.surveyId` points at the newest row. Previous rows kept forever; responses viewer splits aggregates by `surveyId`.
- **Mid-fill version race**: submissions are recorded against the `Survey` version the client fetched; a concurrent admin version-bump does not invalidate an in-flight submit. Applies equally in CLOSED.
- **One-shot immutable**: DB-level `UNIQUE (playtestId, userId)` on `SurveyResponse` — one submission per player per playtest regardless of version. No response-edit flow in MVP.
- **Schema bounds (server-enforced on save)**: max 50 questions per survey; text answers max 4,000 chars; multi-choice max 20 options per question; rating is fixed 1–5.
- **Survey-publish DM fan-out** (the §4.1 step 8 discovery channel that closes the gap for pre-survey applicants). `CreateSurvey` walks every APPROVED + NDA-current applicant on the playtest and enqueues one standalone survey-publish DM per applicant whose `lastSurveyDmId` differs from the freshly-minted survey id. The DM body is the **same hash-router shape** as the approval-DM survey-link append (tappable when `PLAYER_BASE_URL` is configured; non-clickable nudge otherwise). `EditSurvey` is deliberately silent — admins iterating on prompt copy do not re-broadcast. Idempotency rides on `Applicant.lastSurveyDmId`: a successful enqueue is followed by a `MarkSurveyDMSent` stamp, so re-running `CreateSurvey` (or the boot-time restart sweep) is a no-op for applicants already DMed. The fan-out is best-effort — `CreateSurvey` returns success even when the DM channel is degraded; the boot-time **survey-publish restart sweep** (run once before the DM worker starts) re-applies the same predicate to catch applicants the in-process fan-out missed.

### 5.7 Admin pages inside AGS Admin Portal (Extend App UI)
- Built with **React + TypeScript + Vite** as an **Extend App UI** Module Federation remote. Uses **Ant Design v6** components + **Tailwind v4** utilities. Typed backend clients + react-query hooks are generated from the grpc-gateway OpenAPI spec (`apidocs/api.json`) via `@accelbyte/codegen` — no hand-rolled request DTOs. Auth inherited from the Admin Portal `HostContext`; `@accelbyte/sdk-iam` owns token lifecycle. Hosted by AccelByte infrastructure (not GitHub Pages / Vercel) and rendered under **Extend → My Extend Apps → App UI**. See [`architecture.md`](architecture.md) for the full admin stack. **Availability caveat**: Extend App UI is Internal Shared Cloud only at MVP time — see §9 R11.
- **M5.C admin shell restructure** — the legacy modal-per-action shape is replaced by a **list + detail-page-with-tabs** shell. The Playtests list (page 1 below) survives unchanged in shape; pages 2–5 collapse into a single per-playtest **detail page** at `/playtest/<slug>` whose top-of-page header carries breadcrumb + title + date range + status pill (`Draft` / `Published` / `Closed`) + **Publish** (visible in DRAFT only — fires `TransitionPlaytestStatus(OPEN)`) / **Stop Playtest** (visible in OPEN only — fires `TransitionPlaytestStatus(CLOSED)`) header buttons + Playtest Link copy-to-clipboard share. Below the header sit four tabs (`?tab=` query param preserves selection across reloads):
  - **Playtest Info** — read-only summary card (Title / Slug / Description / Banner / Start / End / Platforms / NDA / Distribution Model / Approval Method / Max Participants) + an `Edit` button that opens the existing edit modal flow.
  - **Distribution** — per-model rendering with a shared empty-state scaffold (D6 in [`STATUS_M5.md`](STATUS_M5.md)): ADT shows the M5.B linkage state + namespace/build picker; STEAM_KEYS shows the M1 code-pool table + CSV upload + remaining count; AGS_CAMPAIGN shows the campaign / item summary + sync. A yellow tab-dot fires whenever the publish prerequisite for the selected model is unmet.
  - **Participants** — telemetry-aware 6-column table (Discord Handle / AGS User ID / Sign-up Date / NDA Accepted / Code Sent Date (STEAM_KEYS / AGS_CAMPAIGN — derived from `dm.sent` audit row; "—" for ADT) / Status / Action). Inline Approve / Reject on PENDING rows; per-applicant detail modal on terminal rows. The Hardware Specs + Crash Reports + Download Date + Total Playtime sections are reserved for M6 — fully hidden / NULL across M5.C.
  - **Discord Bot Tools** — admin-authored bulk DM broadcast form (`Send To` filter / Subject / Message) + persistent announcement history. Closed-playtest writes are rejected (form disabled). Fan-out reuses the existing M2 RetryDM machinery; subject + message are PII-sensitive and never logged.
- The header verbs `Publish` (DRAFT→OPEN) + `Stop Playtest` (OPEN→CLOSED) are **pure UI copy renames** over §5.1's existing state machine — no new transitions, no PRD §5.1 state-machine change. M4's window-driven auto-transition (`startsAt` / `endsAt`) continues to drive automatic transitions unchanged; the new buttons are manual-override surfaces.
- Five pages (pre-M5.C historical reference; the detail-page-with-tabs shell above is the M5.C-current shape):
  - Page 1: **Playtest list + create/edit** — distribution-model selector (`STEAM_KEYS | AGS_CAMPAIGN | ADT`) and model-specific create fields (`initialCodeQuantity` for AGS_CAMPAIGN; `adtNamespace` / `adtGameId` / `adtBuildId` picker + optional `adtFallbackDownloadUrl` input for ADT); soft-delete (no restore); flows per §5.1; **no pagination — unbounded list with soft-cap 100 per namespace (§6)**. M5.B adds an **"ADT Linkages" tab** on this page (or a flat section beneath the playtests grid as a cut-if-behind fallback — see [`STATUS_M5.md`](STATUS_M5.md)) listing every `ListADTLinkages` row labelled "Studio-wide linkage" to make the scope explicit (§4.8.1), plus a **"Link new ADT Namespace"** button that opens the modal-then-redirect linking flow (§4.8.2).
  - Page 2: **Applicants list + approve/reject** — per playtest, with status/DM-failed filters, "Retry DM" per applicant, "Retry all failed DMs" bulk action, and a low-pool banner (≤10% remaining; STEAM_KEYS / AGS_CAMPAIGN only — ADT has no pool to track); flows per §5.4; pagination per §6. **`RejectApplicant` UX**: confirm dialog with an optional reason text field. **In M5.C this page is the Participants tab on the detail page.**
  - Page 3: **Survey builder + responses viewer** — per playtest, "Survey version" column on responses; flows per §5.6; pagination per §6.
  - Page 4: **Key pool management** — STEAM_KEYS CSV upload or AGS_CAMPAIGN generate/sync/top-up; raw code values displayed (admin UI is exempt from log-redaction); low-pool banner (≤10% remaining); flows per §4.3 and §4.6; pagination per §6. For ADT playtests this page renders an **empty-state** card explaining "This playtest distributes via ADT — no code pool to manage. Change the distributed game + build via the **Change Build** action (`ChangeADTBuild`, §4.8.6); the linked ADT namespace stays fixed. Update `adtFallbackDownloadUrl` via Edit playtest if you need to repoint the static fallback URL." **In M5.C this page is the Distribution tab on the detail page.**
  - Page 5: **Audit log viewer** — paginated read-only list of `AuditLog` rows, filterable by actor (`actorFilter='system'` maps to `actorUserId IS NULL`) and action; JSON diff of `before`/`after`; no edit/delete; flows per §5.1; pagination per §6. **M3 cut-if-behind candidate** (audit *writes* in M2 remain mandatory).

### 5.8 Runtime configuration (Svelte player app)
- Reads `config.json` at app load before any RPC. Contents: `{ grpcGatewayUrl, iamBaseUrl, discordClientId }`.
- Served as static asset alongside the bundle with `Cache-Control: no-store` (or query-string cache-bust). Lets the same bundle re-point at a different namespace without rebuild.
- Missing or malformed `config.json` is a hard boot failure. Malformed = JSON parse error, missing required key, or value failing its type check (URL for `grpcGatewayUrl`/`iamBaseUrl`; non-empty string for `discordClientId`).

### 5.9 Runtime configuration (Go backend)
- The Go backend reads all configuration from **environment variables only** — no config files.
- **Required env vars** (MVP): `DATABASE_URL`, `DISCORD_BOT_TOKEN` (via Extend secrets), `AGS_IAM_CLIENT_ID`, `AGS_IAM_CLIENT_SECRET`, `AGS_BASE_URL`, `AGS_NAMESPACE`.
- **Optional env vars with defaults**: `RESERVATION_TTL_SECONDS` (default `60`), `RECLAIM_INTERVAL_SECONDS` (default `30`), `LEADER_LEASE_TTL_SECONDS` (default `30`), `LEADER_HEARTBEAT_SECONDS` (default `10`), `AGS_CODE_BATCH_SIZE` (default `1000`), `DM_TIMEOUT_SECONDS` (default `5`), `DM_DRAIN_RATE_PER_SEC` (default `5`) — DM worker drain rate, `WINDOW_TICK_SECONDS` (default `60`) — `internal/window/` worker tick interval; sets value `0` to disable the worker entirely (status then sticks at whatever value it has until a manual `TransitionPlaytestStatus`; see §5.1 "Window-driven auto-transition"), `DB_MAX_CONNECTIONS` (default `10`) — recommended connection pool size per replica, `CORS_ALLOWED_ORIGINS` (default empty — no CORS handling) — comma-separated list of browser origins permitted to call the grpc-gateway HTTP surface; required when the player is hosted off-origin (GitHub Pages, Vercel, custom domain) so cross-origin preflights resolve. Empty matches the vanilla grpc-gateway behaviour (501 on OPTIONS) which is fine for same-origin dev. `PLAYER_BASE_URL` (default empty) — public origin (with optional sub-path) of the player Svelte bundle; when set, the approval DM body embeds a deep link to the pending page so applicants jump straight to the granted-code view. Empty preserves the legacy non-clickable DM copy. `ADT_BASE_URL` (default empty; **required when any `adt_linkage` row exists or any playtest has `distributionModel='ADT'`**) — origin (with optional path) of the ADT instance studios link against; backend rejects `StartADTLink` with `FailedPrecondition` when unset. `ADT_REDIRECT_BASE_URL` (default empty; **required when `ADT_BASE_URL` is set**) — public origin of the admin UI used as the `redirect_uri` query param on the linking redirect; ADT redirects the operator back to `${ADT_REDIRECT_BASE_URL}/adt-link-callback?state=…&result=…&adt_namespace=…` (§4.8.2). `ADT_LINKAGE_PENDING_TTL_SECONDS` (default `600`) — TTL on the `adt_link_pending` nonce row; the `state` returned from `StartADTLink` is rejected by `CompleteADTLink` after this many seconds. `ANNOUNCEMENT_MAX_SUBJECT_LEN` (default `200`) — upper bound on `Announcement.subject` (§5.4 "Bulk announcements"); `CreateAnnouncement` rejects strings longer than this with `InvalidArgument` per [`errors.md`](errors.md). `ANNOUNCEMENT_MAX_MESSAGE_LEN` (default `4000`) — upper bound on `Announcement.message`. Discord DM is hard-capped at 2,000 chars per outbound message; the form cap is intentionally higher so operators can author editorial copy, and the DM fan-out chunks at the Discord boundary.
- Missing required env vars are a **hard failure at startup** — the backend logs the missing key names and exits.

## 6. Non-functional requirements

### Security
- **NDA record integrity**: append-only, versioned by hash, no edit RPC.
- **PII minimization**: Discord handle, AGS userId, form answers. No IP on NDA acceptances. No email collected. **Exception — `discordHandle`**: archival and retained indefinitely alongside `userId`. This is a deliberate minimization exception because DM delivery and admin triage both require a human-readable identity.
- **AuthN**: all player RPCs require AGS access token except the public unauth playtest-by-slug read (restricted field set per §5.2). All admin RPCs require a valid AGS Admin Portal session. **Admin session → `actorUserId`**: the backend extracts the admin UUID from the `sub` claim of the AGS IAM JWT carried in the Bearer token (gRPC metadata `authorization`). The JWT is validated against the AGS IAM JWKS. The extracted `sub` is used as `actorUserId` on every audit-log row for the request.
- **AuthZ (MVP)**: every admin RPC declares a required `(resource, action)` via proto options (`option (playtesthub.v1.resource)` + `option (playtesthub.v1.action)`) and the auth interceptor enforces it against the AGS IAM permission claim using the `accelbyte-go-sdk` permission validator (`PermissionDenied` on miss). The required resource is **`ADMIN:NAMESPACE:{namespace}:EXTEND:APPUI`** — the built-in AppUI-admin permission already held by every namespace-admin role studios assign in the AGS Admin Portal (Game Admin / Studio Admin / equivalent). Required action varies per RPC: `CREATE` (bit 1) for resource-creating POSTs (`Create*`, `Upload*`, `TopUp*`), `READ` (bit 2) for `List*` / `Get*`, `UPDATE` (bit 4) for `Edit*` / `Approve*` / `Reject*` / `RetryDM*` / `Transition*` / `Sync*`, `DELETE` (bit 8) for soft-delete. **Why APPUI specifically**: it is the perm tied to "you can render Extend App UIs in this namespace" — exactly the entry surface playtesthub's admin uses; held by every role studios already assign to admin staff, so no playtesthub-specific role setup is needed on the AGS side. **Why not `CUSTOM:ADMIN:NAMESPACE:{namespace}:PLAYTEST`**: AGS Shared Cloud does not let game admins assign `CUSTOM:*` perms to their own user roles (custom-permission management is AccelByte-only there); APPUI is the closest built-in proxy that requires no AGS-side role creation. The `AuditLog` (§5.7) remains the per-action accountability layer (one shared role bit ≠ per-user identity).
- **Secrets**: Discord bot token via Extend secrets. Rotation: update + restart backend; in-flight DMs at rotation time surface as `lastDmStatus='failed'` and are retryable.
- **`config.json` integrity (accepted risk)**: served unsigned over HTTPS; mitigated by trusted static hosts (GitHub Pages / Vercel).
- **gRPC-gateway exposure**: publicly addressable. CORS allowlist per-studio (AGS Admin Portal origin + player app origin). TLS handled by Extend, served exclusively from Extend-provided hostname (custom domains out of scope).

### gRPC error-code reference

See [`errors.md`](errors.md) for byte-exact gRPC codes/messages. If the prose in this PRD and the table in `errors.md` diverge, the PRD prose is authoritative and the table row is a bug.

### Rate limiting
- No app-layer rate limit in MVP; Extend gateway defaults apply. Duplicate calls handled by natural-key idempotency (replay returns same result, not error). Per-IP / per-user limits deferred.

### Pagination
- Cursor-based for unbounded admin list views (applicant queue §5.4, audit log §5.7, survey responses §5.7). **Page size 50, ordering `createdAt DESC`.** Cursor is opaque base64 `(createdAt, id)` tuple. Survey responses viewer uses `(submittedAt, id)` instead. Offset-based pagination is not used.
- **Playtest list view is unpaginated** — expected small cardinality per namespace. All playtests are returned in a single response. **Soft cap: 100 non-deleted playtests per namespace.** `CreatePlaytest` returns gRPC `ResourceExhausted` when the cap is reached. Studios that need more must soft-delete old playtests first.

### Data retention
- **Indefinite retention** in MVP. Deletion of applicant, response, NDA acceptance, or code data requires a studio operator to run SQL against the Extend-managed Postgres.
- **`AuditLog` retention**: **indefinite, no size cap in MVP** — the table grows without bound. Pruning/archival is deferred post-MVP (noted as a risk in §9).
- **Explicit non-goal**: no self-serve "delete my data" RPC for players in MVP. Adopters with GDPR obligations must build this themselves or wait for a post-MVP release.

### Time zones
- UTC on DB and wire (RFC3339 `Z`); frontend owns conversion on input and render. Server decisions use UTC only.

### Performance
- **Target scenario**: 500 signups over 10 minutes on the demo namespace with **p95 signup latency < 3s end-to-end** (player click "Sign up" → approved/pending visible in UI, inclusive of AGS IAM + Discord OAuth redirect time).
- **Approve RPC target**: p95 < 2s under nominal load (one DB reserve + DB finalize, no external AGS call at approve time, no Discord-DM blocking).
- **Multi-replica perf** is not contractually measured for v1.0 (single-replica is the measurement baseline).
- **Measurement**: performed via the load script in `/scripts/loadtest/` (referenced, not implemented as part of MVP), and reported in `CHANGELOG.md` per release. **Not a CI gate for MVP.**

### Clock skew
- Server clock drift assumed `< reservationTtl/2` (i.e. < 30s). NTP sync required.

### Availability
- Best-effort. No formal SLO in MVP. Multi-replica deploys are safe (the reclaim job uses DB-backed leader election per §5.5); AGS namespace outage implies app outage.

### Accessibility
- Target WCAG 2.1 AA for the player UI with automated CI enforcement.
- **Admin UI** is targeted at WCAG 2.1 AA **by design intent but is NOT measured by CI in MVP**.
- **CI gate (player UI)**: `@axe-core/playwright` (version pinned in CI) on five player pages (landing, signup form, NDA accept, approved/code view, survey form) with zero critical violations. Rule tag filter: `wcag2a, wcag2aa, wcag21a, wcag21aa` only.
- The `config.json` malformed-boot error page is excluded from the CI gate.

### Browser support
- **Evergreen only**: any current major browser (Chrome, Firefox, Safari, Edge) at the two most-recent major releases. No explicit fixed version matrix; no IE/legacy support.

### Versioning & compatibility
- gRPC versioned via proto package (`playtesthub.v1`, `v2`). Breaking changes create a new package. No formal compat SLA in MVP — single deployment ships backend + player app together; `config.json` shape is part of the versioned bundle.

### Internationalization
- Deferred. English only. Strings centralized.

### Observability
- Structured JSON logs only. Every request log line includes `requestId`, `userId` (when authed), `playtestId` (when in scope), `action`.
- **Log redaction**: NDA text, survey free-text answers, and code values MUST NOT appear in logs. `AuditLog` is exempt (authoritative edit history).
- Metrics and distributed tracing are explicit non-goals for MVP.

## 7. Success metrics

v1.0 success is a demo + docs bar, not usage metrics.

- Demo video ≤5 min showing the golden flow against a real AGS namespace (maintainer-owned sandbox; OSS adopters BYO).
- README walkthrough reproducible by a new engineer on a clean AGS sandbox in ≤60 minutes from clone to working golden flow.
- Public repo on GitHub, MIT `LICENSE` at root, contributing guide, CI green.
- CI (GitHub Actions) on every PR: Go (`go vet` / `golangci-lint` / `go test ./...`), proto stubs check (`buf`), Svelte `npm run build`, a11y (axe-core — see §6).
- Full golden flow green: fresh Discord account signs up, accepts NDA, is approved, receives DM code, submits a survey response.
- Perf proof point: 500 signups / 10 min, p95 < 3s end-to-end (see §6 Performance — measurement via `/scripts/loadtest/`, reported in CHANGELOG; not a CI gate).
- Stretch: at least one external studio reports a successful self-host.

## 8. Constraints, assumptions, dependencies

### AGS services (required)
- **IAM** — Discord OAuth federation, AGS token issuance.
- **Platform / Campaign API** — for AGS_CAMPAIGN: Item (ENTITLEMENT type), Campaign, code generation/retrieval. Not used at approve time.

### Extend features
- **Required**: Service Extension (gRPC), Extend-managed Postgres, Admin Portal extension (experimental).
- **Not used in MVP**: Event Handler, Scheduled Action, Override pattern.

### External dependencies
- Discord OAuth app + bot token.
- GitHub Pages / Vercel for the player app.
- AGS Platform API auth reuses IAM client credentials from the Extend app template.
- Steam is NOT a dependency (STEAM_KEYS codes are passthrough strings).
- **AccelByte Development Toolkit (ADT)** — optional, required only for playtests with `distributionModel=ADT` (§4.8). Auth from playtesthub to ADT uses the same AGS service IAM JWT as AGS Platform calls (no separate credential / API key). ADT must be reachable from the backend, the operator's admin UI browser must be able to redirect to `${ADT_BASE_URL}/oauth/link?…` and back to `${ADT_REDIRECT_BASE_URL}/adt-link-callback?…`, and the studio's ADT namespace must already exist on the ADT side before linking.

### Stack
- **Backend**: Go + gRPC + Postgres.
- **Player frontend**: Svelte static app.
- **Admin frontend**: React in the AGS Admin Portal extension.

See [`architecture.md`](architecture.md) for the full stack + external dependency detail.

### Bounds rationale
- `initialCodeQuantity` 1–50,000; 100 playtests/namespace; 10 MB / 50,000-line CSV. Internal MVP safety limits, not AGS-imposed.

### License
- MIT. `LICENSE` file at repo root is a v1.0 deliverable.

### Key assumptions
- Admin users hold a namespace-admin role granting `ADMIN:NAMESPACE:{namespace}:EXTEND:APPUI` (Game Admin / Studio Admin / equivalent). Studios assign these via the AGS Admin Portal during onboarding; no playtesthub-specific role setup required (§6 AuthZ, §9 R8).
- One Extend deploy per namespace; no cross-namespace sharing.
- Approve is synchronous; admin retries on failure; reclaim job frees stranded `RESERVED`.
- AGS Platform Campaign API availability validated in M2 (see `ags-failure-modes.md`).
- Extend SDK handles AGS Platform token refresh automatically.

## 9. Risks & open questions

### Risks
- **R1**: experimental admin-portal extension capability — keep admin UI able to run standalone as a fallback dev tool (see §5.7).
- **R2**: Discord DM deliverability — surface code in player UI regardless of DM success; webhook fallback deferred (see §4.1 step 6d, §5.4).
- **R3**: code pool exhaustion — admin UI surfaces a low-water banner at **≤10% remaining** on Key Pool and Applicants pages; approve blocked with `ResourceExhausted` when empty; for AGS_CAMPAIGN admin can generate more on demand (see §4.6, §5.4).
- **R4**: synchronous approve path — 5s DM timeout, no retry, non-fatal; player reload shows code regardless (see §4.1 step 6d).
- **R5**: AGS platform dependency (residency + Campaign API availability) — studios verify namespace region; STEAM_KEYS fallback has no external API dependency (see §4.6).
- **R6**: stranded reservations — TTL-based reclaim job mandatory; per-tick INFO log line for liveness (see §5.5).
- **R7**: open-source adoption friction — invest in README + scripted setup (see §10).
- **R8**: admin RBAC piggybacks on the built-in `ADMIN:NAMESPACE:{namespace}:EXTEND:APPUI` permission instead of a dedicated `PLAYTEST_ADMIN` role. Coarse — anyone holding a namespace-admin tier (Game Admin / Studio Admin / equivalent) becomes a playtest admin. Acceptable because that permission set already grants the operator wider blast radius than playtesthub itself (full Extend App UI management on the namespace); the `AuditLog` provides per-action attribution. A dedicated `CUSTOM:ADMIN:NAMESPACE:{namespace}:PLAYTEST` role with finer-grained permissions is post-MVP and gated on AGS Shared Cloud allowing game admins to assign `CUSTOM:*` perms to their own user roles (currently AccelByte-only; on the AGS roadmap). See §6 AuthZ.
- **R9**: `AuditLog` unbounded growth — pruning/archival deferred post-MVP (see §6 Data retention).
- **R10**: namespace decommission means data loss — self-host operators own DB backups (see §8).
- **R11**: Extend App UI is an **experimental capability** and currently available in **Internal Shared Cloud only**. Private Cloud adopters cannot run the admin UI in MVP; they must wait for Extend App UI GA or run the admin surface out-of-band against the gRPC-gateway until then. Tracked for MVP as an adoption-friction risk; no engineering mitigation planned (see §5.7, [`architecture.md`](architecture.md)).

### Open questions

No open questions remain.

## 10. Milestones / scope cuts

Scope-at-risk features are tracked in [`STATUS.md`](STATUS.md) under **Cut-if-behind tracking**.

### M1 — Backend + admin CRUD + signup (STEAM_KEYS only)
- Discord handle sourcing: bot token → Discord API `GET /users/{userId}` (Discord ID from AGS IAM claims; OAuth access token is not available). On failure (404 / 5xx / network error): signup proceeds with raw Discord user ID stored as `discordHandle`. Fetched once at signup, never refreshed.
- Go Service Extension skeleton; Postgres schema for `Playtest` (with `distributionModel`, `ags*` IDs, `initialCodeQuantity`), `Applicant`, `Code` (state machine), `leader_lease`. Only `STEAM_KEYS` functional in M1.
- Admin Portal extension skeleton: Playtest list + create/edit (+ soft-delete). Create form includes distribution-model selector (visible but disabled for AGS_CAMPAIGN) and `initialCodeQuantity` input. `CreatePlaytest` with `distributionModel=AGS_CAMPAIGN` returns gRPC `Unimplemented` in M1.
- Player Svelte app skeleton: landing + Discord-through-IAM login + signup form. `config.json` loader wired.
- MIT `LICENSE` at repo root.
- CI scaffolding: Go lint+tests, proto check, Svelte build, axe-core a11y check.
- Golden flow stops at "applicant is PENDING".

### M2 — NDA + approval + code grant (pool-only) + AGS Campaign API integration + RetryDM
- NDA click-accept + versioned acceptance storage.
- Admin applicants page with approve/reject; low-pool banner.
- Key pool management: STEAM_KEYS CSV upload; AGS_CAMPAIGN pool status + "Generate more codes" + "Sync from AGS".
- AGS Campaign API integration: auto-provisioning, code fetch/ingest, top-up, sync. Validate against sandbox.
- Approve API: reserve → fenced finalize (§4.1 step 6); background reclaim job with TTL.
- Per-applicant **RetryDM** RPC (bulk `RetryFailedDms` remains in M3).
- `AuditLog` table + write path wired for `playtest.edit`, `nda.edit`, `applicant.approve/reject`, `applicant.dm_failed`, `code.upload`, `code.upload_rejected`, `code.grant_orphaned`, `campaign.*` (full old+new `ndaText` captured on NDA edits). No viewer UI yet.
- Golden flow stops at "player sees code in UI".
- **AGS_CAMPAIGN M2 go/no-go**: validate seven sub-capabilities against sandbox (Item create, Campaign create, CreateCodes 1000-batch, Code fetch, Item/Campaign delete, TopUpCodes, SyncFromAGS). Partial-ship matrix in [`ags-failure-modes.md`](ags-failure-modes.md).

### M3 — Survey + Discord notifications + polish/docs/demo
- Survey builder (text / rating / multi-choice). Optional per playtest.
- Survey response submission (one-shot immutable) + responses viewer (aggregates split by `surveyId`).
- Discord DM notification on approval; circuit breaker + bulk retry RPC (`RetryFailedDms`).
- Audit log viewer UI (§5.7 page 5).
- Perf proof point per §6 / §7.
- README walkthrough (with admin-authorization note pointing studios at the `ADMIN:NAMESPACE:{namespace}:EXTEND:APPUI` permission gate; §6 AuthZ), demo video, OSS repo published under MIT.

### Deferred / post-MVP backlog
- Webhook-channel notification fallback (R2; §4.1 step 6d).
- `AuditLog` retention pruning / archival (R9; §6 Data retention).
- Responses CSV export (§4.5; tracked in [`STATUS.md`](STATUS.md) M3 cut-if-behind).
- Self-serve "delete my data" RPC (§6 Data retention).
- Explicit per-IP / per-user signup rate limiting (§6 Rate limiting).
- Markdown rendering for `Playtest.description` (§5.1 — plain text in MVP).


