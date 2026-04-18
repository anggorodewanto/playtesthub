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
- **RBAC / admin tooling**: custom admin RBAC role; bulk approve; editing a submitted survey response; observability beyond structured logs.
- **Messaging / workflows**: cross-service event bus / async workflow engine (the §5.4 DM queue is an internal implementation detail, not a product-visible async workflow); scheduled actions (the §5.5 reclaim job is an in-app leader-elected worker).
- **Data lifecycle**: self-serve "delete my data" flow; soft-delete restore UX (recovery only via direct DB intervention); multi-region data residency, GDPR tooling, SOC 2.
- **Content**: internationalization (English only); advanced survey logic (branching, conditional, file uploads); markdown rendering in `Playtest.description` (plain text in MVP; markdown on post-MVP backlog).

## 3. Target users & use cases

- **Studio admin (live ops / community / QA lead)**: accesses the tool via the AGS Admin Portal extension. Auth via existing Admin Portal session. MVP authorizes any authenticated AGS admin session — no custom `PLAYTEST_ADMIN` role yet (§9 R8). Use cases: create playtest (with distribution model), approve/reject applicants, manage key pool, author survey, review responses.
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

## 5. Functional requirements

### 5.1 Playtest CRUD
- Fields: `id`, `namespace`, `slug`, `title` (max 200 chars), `description` (**plain text in MVP — stored raw, rendered as pre-wrap plain text by the player app; markdown rendering is post-MVP backlog**; max 10,000 chars), `bannerImageUrl` (https-only URL, max 2,048 chars; backend never stores image binaries — XSS mitigated by scheme allow-listing, no further sanitization), `platforms` (`TEXT[]` of `STEAM | XBOX | PLAYSTATION | EPIC | OTHER`; playtests targeted), `startsAt`, `endsAt` (**display-only in MVP** — do NOT gate signup, NDA accept, approve, reject, or survey submit; only OPEN/CLOSED status gates lifecycle), `status` (`DRAFT | OPEN | CLOSED`), `ndaRequired` (bool), `ndaText`, `currentNdaVersionHash`, `surveyId` (nullable), `distributionModel` (`STEAM_KEYS | AGS_CAMPAIGN`; immutable after creation), `agsItemId` (nullable, AGS_CAMPAIGN only), `agsCampaignId` (nullable, AGS_CAMPAIGN only), `initialCodeQuantity` (nullable `INTEGER` 1–50,000; required for AGS_CAMPAIGN; NULL for STEAM_KEYS), `createdAt`, `updatedAt`, `deletedAt` (nullable).
- **`slug`**: admin-chosen, server-validated against `^[a-z0-9][a-z0-9-]{2,63}$` (3–64 chars). Unique per namespace. Input that does not match the regex is rejected with `InvalidArgument`; the server does not silently sanitize. Slug reuse after soft-delete is blocked (uniqueness enforced across live and soft-deleted rows).
- **`namespace` ↔ `AGS_NAMESPACE`**: `Playtest.namespace` rows are populated from the `AGS_NAMESPACE` env var at insert time. No per-request override.
- **`EditPlaytest` whitelist**: editable — `title`, `description`, `bannerImageUrl`, `platforms`, `startsAt`, `endsAt`, `ndaRequired`, `ndaText`. Immutable — `slug`, `namespace`, `status` (use `TransitionPlaytestStatus`), `distributionModel`, `initialCodeQuantity`, all `ags*` IDs, all timestamps. Editing an immutable field returns `InvalidArgument` with the offending field name. Editing `ndaText` recomputes `currentNdaVersionHash` and triggers the re-accept flow per §5.3. All edits write `AuditLog`.
- **Status transitions (strict linear)**: `DRAFT → OPEN → CLOSED` only. `DRAFT → CLOSED` is invalid, rejected with `FailedPrecondition`. No reopen. Per-status RPC allowance is Table A below.
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

**Accountability note**: because MVP ships without a custom `PLAYTEST_ADMIN` RBAC role (§9 R8), the audit log is the accountability model. Every admin-mutating action writes an `AuditLog` row. Retention is indefinite with no size cap in MVP (§9 R9).

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

### 5.5 Code pool — entity & state machine

The `Code` entity is the authoritative per-playtest pool for both distribution models (STEAM_KEYS via CSV §4.3; AGS_CAMPAIGN via AGS API §4.6). Schema in [`schema.md`](schema.md).

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

### 5.7 Admin pages inside AGS Admin Portal (Extend App UI)
- Built with **React + TypeScript + Vite** as an **Extend App UI** Module Federation remote. Uses **Ant Design v6** components + **Tailwind v4** utilities. Typed backend clients + react-query hooks are generated from the grpc-gateway OpenAPI spec (`apidocs/api.json`) via `@accelbyte/codegen` — no hand-rolled request DTOs. Auth inherited from the Admin Portal `HostContext`; `@accelbyte/sdk-iam` owns token lifecycle. Hosted by AccelByte infrastructure (not GitHub Pages / Vercel) and rendered under **Extend → My Extend Apps → App UI**. See [`architecture.md`](architecture.md) for the full admin stack. **Availability caveat**: Extend App UI is Internal Shared Cloud only at MVP time — see §9 R11.
- Five pages:
  - Page 1: **Playtest list + create/edit** — distribution-model selector and `initialCodeQuantity` field on create; soft-delete (no restore); flows per §5.1; **no pagination — unbounded list with soft-cap 100 per namespace (§6)**.
  - Page 2: **Applicants list + approve/reject** — per playtest, with status/DM-failed filters, "Retry DM" per applicant, "Retry all failed DMs" bulk action, and a low-pool banner (≤10% remaining); flows per §5.4; pagination per §6. **`RejectApplicant` UX**: confirm dialog with an optional reason text field.
  - Page 3: **Survey builder + responses viewer** — per playtest, "Survey version" column on responses; flows per §5.6; pagination per §6.
  - Page 4: **Key pool management** — STEAM_KEYS CSV upload or AGS_CAMPAIGN generate/sync/top-up; raw code values displayed (admin UI is exempt from log-redaction); low-pool banner (≤10% remaining); flows per §4.3 and §4.6; pagination per §6.
  - Page 5: **Audit log viewer** — paginated read-only list of `AuditLog` rows, filterable by actor (`actorFilter='system'` maps to `actorUserId IS NULL`) and action; JSON diff of `before`/`after`; no edit/delete; flows per §5.1; pagination per §6. **M3 cut-if-behind candidate** (audit *writes* in M2 remain mandatory).

### 5.8 Runtime configuration (Svelte player app)
- Reads `config.json` at app load before any RPC. Contents: `{ grpcGatewayUrl, iamBaseUrl, discordClientId }`.
- Served as static asset alongside the bundle with `Cache-Control: no-store` (or query-string cache-bust). Lets the same bundle re-point at a different namespace without rebuild.
- Missing or malformed `config.json` is a hard boot failure. Malformed = JSON parse error, missing required key, or value failing its type check (URL for `grpcGatewayUrl`/`iamBaseUrl`; non-empty string for `discordClientId`).

### 5.9 Runtime configuration (Go backend)
- The Go backend reads all configuration from **environment variables only** — no config files.
- **Required env vars** (MVP): `DATABASE_URL`, `DISCORD_BOT_TOKEN` (via Extend secrets), `AGS_IAM_CLIENT_ID`, `AGS_IAM_CLIENT_SECRET`, `AGS_BASE_URL`, `AGS_NAMESPACE`.
- **Optional env vars with defaults**: `RESERVATION_TTL_SECONDS` (default `60`), `RECLAIM_INTERVAL_SECONDS` (default `30`), `LEADER_LEASE_TTL_SECONDS` (default `30`), `LEADER_HEARTBEAT_SECONDS` (default `10`), `AGS_CODE_BATCH_SIZE` (default `1000`), `DM_TIMEOUT_SECONDS` (default `5`), `DM_DRAIN_RATE_PER_SEC` (default `5`) — DM worker drain rate, `DB_MAX_CONNECTIONS` (default `10`) — recommended connection pool size per replica.
- Missing required env vars are a **hard failure at startup** — the backend logs the missing key names and exits.

## 6. Non-functional requirements

### Security
- **NDA record integrity**: append-only, versioned by hash, no edit RPC.
- **PII minimization**: Discord handle, AGS userId, form answers. No IP on NDA acceptances. No email collected. **Exception — `discordHandle`**: archival and retained indefinitely alongside `userId`. This is a deliberate minimization exception because DM delivery and admin triage both require a human-readable identity.
- **AuthN**: all player RPCs require AGS access token except the public unauth playtest-by-slug read (restricted field set per §5.2). All admin RPCs require a valid AGS Admin Portal session. **Admin session → `actorUserId`**: the backend extracts the admin UUID from the `sub` claim of the AGS IAM JWT carried in the Bearer token (gRPC metadata `authorization`). The JWT is validated against the AGS IAM JWKS. The extracted `sub` is used as `actorUserId` on every audit-log row for the request.
- **AuthZ (MVP)**: any authenticated AGS admin session is permitted on all admin RPCs. **Unsafe for production** (§9 R8). RBAC is a release blocker for prod.
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
- Admin users already have AGS Admin Portal access (no finer check; §9 R8).
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
- **R8**: MVP ships without admin RBAC — UNSAFE FOR PRODUCTION; `AuditLog` is the accountability model; treat RBAC as release blocker for prod (see §6 AuthZ).
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
- README walkthrough (with "no admin RBAC in MVP — not production safe" warning), demo video, OSS repo published under MIT.

### Deferred / post-MVP backlog
- Webhook-channel notification fallback (R2; §4.1 step 6d).
- `AuditLog` retention pruning / archival (R9; §6 Data retention).
- Responses CSV export (§4.5; tracked in [`STATUS.md`](STATUS.md) M3 cut-if-behind).
- Self-serve "delete my data" RPC (§6 Data retention).
- Explicit per-IP / per-user signup rate limiting (§6 Rate limiting).
- Markdown rendering for `Playtest.description` (§5.1 — plain text in MVP).


