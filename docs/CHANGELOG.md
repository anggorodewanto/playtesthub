# playtesthub — Full Version History

## v2.4 — 2026-05-19

**Auto-approve at signup time (M5 Track A scope freeze)**:
- §5.1 — Playtest gains two new fields: `autoApprove` (`BOOLEAN NOT NULL DEFAULT FALSE`) and `autoApproveLimit` (nullable `INTEGER` 1–100,000; required when `autoApprove=true`). Both are added to the `EditPlaytest` editable whitelist so operators can flip the toggle or retune the cap mid-playtest without recreating the playtest. Distribution-model-agnostic: works for STEAM_KEYS and AGS_CAMPAIGN today; will work for ADT (M5.B) once that track lands.
- §5.4 — new "Auto-approve" subsection covers: cap semantics (`autoApproveLimit` bounds **auto-approvals only**; manual `ApproveApplicant` stays uncapped); concurrency model (per-playtest `pg_advisory_xact_lock` in the signup tx, reusing the existing M2 reserve → fenced finalize → CAS primitives verbatim); pool-empty fallback (auto-approve silently falls through to `PENDING` when the pool is empty — signup itself still returns success); interaction with `RetryFailedDms` (auto-approve misses are PENDING, not DM-failed — manual approve / pool restock is the recovery, not `RetryFailedDms`); system-attributed audit trail.
- `schema.md` — Applicant gains an `autoApproved BOOLEAN NOT NULL DEFAULT FALSE` column (admin-visible only) so the auto-approve cap predicate has an unambiguous count source. New `applicant.auto_approved` `AuditLog` action — system-emitted (`actorUserId = NULL`), records `{applicantId, autoApprovedAt, codeId? (NULL when no code pool)}`, never the raw code value. Distinct from `applicant.approve` so audit-log filters cleanly separate manual vs auto attribution.
- `errors.md` — new `InvalidArgument` row for `CreatePlaytest` / `EditPlaytest` when `auto_approve=true` and `auto_approve_limit` is NULL or out of bounds (byte-exact: `auto_approve_limit must be between 1 and 100000 when auto_approve is true`).
- M5 build plan tracked in [`STATUS_M5.md`](STATUS_M5.md). Track A (auto-approve) ships first; Track B (ADT distribution) is gated on ADT-eng API answers and stays parked behind STATUS_M5.md open questions §1–4.
- **Backwards compatibility note**: all existing M1–M4 playtests default to `autoApprove=false`; behavior is unchanged for every existing playtest until an admin opts in via the new toggle. Manual approve is unaffected — auto-approve plumbs through the same primitives.

## v2.3 — 2026-05-19

**Playtest window enforcement (`startsAt` / `endsAt` are no longer display-only)**:
- §5.1 — `startsAt` / `endsAt` paragraph rewritten. Fields are now UTC and **drive automatic `DRAFT → OPEN → CLOSED` status transitions** via a leader-leased background worker (`internal/window/`). New "Window-driven auto-transition" subsection documents the nullable-date matrix (both / start-only / end-only / neither), the monotonic forward-only invariant, the manual-override precedence (admin transitions always win), and the explicit carve-out that `endsAt` auto-close does **NOT** gate survey submit — APPROVED applicants can still submit surveys post-CLOSED per §5.6.
- §5.9 — new optional env var `WINDOW_TICK_SECONDS` (default `60`, set `0` to disable the worker entirely).
- `errors.md` — new `InvalidArgument` row for `CreatePlaytest` / `EditPlaytest` when both dates set with `endsAt <= startsAt` (byte-exact: `ends_at must be after starts_at`).
- `schema.md` — note on `playtest.status_transition` clarifying that `actorUserId = NULL` (system-emitted) signals an auto-transition from the worker; non-NULL signals a manual `TransitionPlaytestStatus` call.
- M4 build plan tracked in [`STATUS_M4.md`](STATUS_M4.md). No new RPCs gated on dates (status remains the single source of truth); one read-only `GetWorkerHealth` admin RPC will land in M4 phase 5 for the admin worker-health banner.
- **Backwards compatibility note**: every existing M1/M2/M3 playtest with `startsAt` / `endsAt` already populated will start auto-transitioning the moment the M4 phase 3 worker deploys. Operators who relied on "dates are decorative" should either null the dates out (manual mode) or accept the auto-flip.

## v2.2 — 2026-05-08

**Discord DM delivery hardened with explicit bot+server prerequisites and a deep-link DM body**:
- §5.9 Runtime configuration — new optional env var `PLAYER_BASE_URL` documented. When set, the approval DM body embeds a deep link to the pending page (`<base>/#/playtest/<slug>/pending`) so applicants tap once and land on the granted-code view; empty preserves the legacy non-clickable copy. Implementation in `pkg/service/retry_dm.go buildApprovalDMBody` covers both auto-send and manual-retry enqueue paths (`approve.go`, `RetryDM`, `RetryFailedDms`).
- §10 M1 / §5.4 — Discord-bot-must-share-a-guild-with-recipient operator constraint surfaced in [`docs/runbooks/setup-ags-discord.md` § 7 "Discord bot + server"](runbooks/setup-ags-discord.md#7-discord-bot--server-required-for-dm-delivery). Discord rejects bot DMs with HTTP 403 / `code 50278` ("Cannot send messages to this user due to having no mutual guilds") whenever the bot and recipient share no Discord server. Applicant-side: studios surface a server invite via the optional `discordInviteUrl` config field rendered on the player Pending page; Pages workflow accepts a new optional `PLAYER_DISCORD_INVITE_URL` Repo Variable. No backend behaviour change for the failure path itself — DM continues to surface the 50278 verbatim through the existing `last_dm_error` channel — this revision just documents the constraint and gives operators a path to satisfy it.

## v2.1 — 2026-05-08

**Admin RBAC reframed: APPUI is the design choice, not a workaround**:
- §6 AuthZ rewritten to document the *existing* per-RPC `(resource, action)` permission gate that has shipped since M1 phase 8 (declared via proto options, enforced by the `accelbyte-go-sdk` permission validator). Resource locked in as **`ADMIN:NAMESPACE:{namespace}:EXTEND:APPUI`** — the AppUI-admin perm held by every namespace-admin role studios already assign (Game Admin / Studio Admin / equivalent). Per-RPC action bits made explicit: CREATE for create/upload/top-up, READ for list/get, UPDATE for edit/approve/reject/retry/transition/sync, DELETE for soft-delete. The PRD now explains *why APPUI* (entry surface match + zero AGS-side role setup) and *why not `CUSTOM:ADMIN:NAMESPACE:{namespace}:PLAYTEST`* (Shared Cloud blocks game admins from assigning `CUSTOM:*` perms — AccelByte-only, on the AGS roadmap).
- §9 R8 reframed — risk downgraded from "ships without RBAC, UNSAFE FOR PRODUCTION" to "RBAC is a coarse single namespace-admin permission bit; everyone with namespace-admin tier becomes a playtest admin; the `AuditLog` is the per-actor accountability layer". A dedicated finer-grained role remains post-MVP and is gated on AGS allowing `CUSTOM:*` assignment in Shared Cloud.
- §2 non-goals, §3 studio admin, §5 accountability note, §8 key assumptions, §10 M3 README walkthrough cross-references updated to point at the documented permission gate (no behavior change vs. v2.0; this is documentation honesty, not a code change).
- README header swapped from the old "not production safe — no admin RBAC" warning to an "admin authorization" note describing the gate and which roles satisfy it.
- `errors.md` adds a `PermissionDenied` row (any admin RPC, missing `EXTEND:APPUI` at required action) and an `Unauthenticated` row (any RPC, missing/invalid Bearer token).
- `engineering.md` rewrite (drop the "AppUI-admin workaround pending CUSTOM:* assignment" framing → document APPUI as the permanent design choice) tracked as M3 phase 18; no proto / generated-stub / interceptor change needed.

## v2.0 — 2026-04-17

**Admin UI delivery migrated to Extend App UI**:
- §5.7 — admin pages now delivered via AGS **Extend App UI** (experimental). React 19 + Vite + Module Federation remote, hosted by AccelByte under **Extend → My Extend Apps → App UI**. Ant Design v6 + Tailwind v4. Typed clients + react-query hooks generated from grpc-gateway OpenAPI (`apidocs/api.json`) via `@accelbyte/codegen`. Auth inherited from Admin Portal `HostContext`; `@accelbyte/sdk-iam` owns token lifecycle. Legacy `justice-adminportal-extension-website` + `justice-ui-library` path is **no longer used**.
- §4.2 — dev onboarding step 5 updated: `extend-helper-cli appui create` + `appui upload` replaces the extension-site registration.
- §8 Extend features — Extend App UI added as required; legacy extension-site path moved to "not used in MVP".
- §9 — new **R11**: Extend App UI availability constrained to **Internal Shared Cloud only** at MVP time; Private Cloud adopters deferred until GA.
- Corresponding updates in `architecture.md` (admin frontend section rewritten) and `engineering.md` (new base-template section, repo layout, test/CI/local-dev guidance for the admin app).

## v1.9 — 2026-04-17

**User decisions**:
- **AGS 429 → fail-fast**: §4.6 retry policy clarified — HTTP 5xx and timeouts retry up to 3× with exponential backoff; 4xx including 429 fail immediately and surface as gRPC `RESOURCE_EXHAUSTED`.
- **DM resilience**: §5.4 adds circuit breaker (50 consecutive failures within 60s → pause queue 5 min, auto-resume; new approves still enqueue while tripped; tripped DMs surface `lastDmError='dm_circuit_open'`). New `RetryFailedDms(playtestId)` bulk RPC and admin "Retry all failed DMs" button. New `dm.circuit_opened` / `dm.circuit_closed` audit actions (system-attributed).
- **Code-pool low-water at ≤10% remaining**: banner on Key Pool page and Applicants page (point-of-use). No audit, no DM, no email. R3 in §9 updated.
- **Strict linear status transitions**: `DRAFT → OPEN → CLOSED` only; `DRAFT → CLOSED` rejected with `FailedPrecondition`. `EditPlaytest` whitelist defined explicitly: editable — `title`, `description`, `bannerImageUrl`, `platforms`, `startsAt`, `endsAt`, `ndaRequired`, `ndaText`. Immutable — `slug`, `namespace`, `status`, `distributionModel`, `initialCodeQuantity`, all `ags*` IDs, all timestamps. Editing an immutable field returns `InvalidArgument` with the offending field name.

**Mechanical fixes**:
- §5.7 audit-log viewer flagged as M3 cut-if-behind candidate (writes in M2 remain mandatory).
- `ListAuditLog`: `actorFilter='system'` defined to map to `actorUserId IS NULL`.
- §5.4 DM restart sweep: idempotency guard added — sweep only re-marks `lastDmStatus IS NULL` or `'pending'`, preserves prior `'failed'` reason.
- §4.6 / M2: AGS sub-cap 4 (code fetch) failure treated same as 1–3 (defer AGS_CAMPAIGN entirely); sub-cap 5 (delete cleanup) failure ships initial-generate-only path with WARN log.
- `CreatePlaytest` request docs read "Playtest fields *except `status`*" (DRAFT default server-side).
- `AuditLog.action` enum source of truth is `schema.md`; PRD references it.
- `CreatePlaytest` slug collision: `AlreadyExists` (gRPC error-code reference table updated).
- Status transitions: explicit "DRAFT → CLOSED is invalid; admins must transition through OPEN."
- §5.1: `Playtest.namespace` populated from `AGS_NAMESPACE` env var at insert time; no per-request override.
- §7 / §8: demo perf measurement via `/scripts/loadtest/` (referenced not implemented), reported in CHANGELOG, not a CI gate for MVP.
- §6 NFR: clock skew assumption (server clock drift < `reservationTtl/2`; NTP sync required).
- §5.2 / §5.4 / `dm-queue.md`: `lastDmError` byte-truncation specified to preserve valid UTF-8 codepoint boundaries.

**Aggressive trim / extractions**:
- New file `docs/ags-failure-modes.md` extracted from §4.6 (retry policy, partial-failure cleanup, code-generation pagination, sub-cap failure matrix). §4.6 reduced to summary + link.
- New file `docs/dm-queue.md` extracted from §5.4 (FIFO mechanics, overflow, restart sweep, circuit breaker, bulk retry, truncation). §5.4 reduced to summary + link.
- §9 R1–R10 risks compressed to one-line entries with cross-references.
- §5.7 admin pages compressed to one-liners per page with cross-references.
- v1.7 / v1.8 inline rationale comments removed from PRD; rationale lives in this CHANGELOG entry and v1.8 entry below.
- PRD preamble (lines 7–11 of v1.8) removed — version history now lives entirely in CHANGELOG.
- §2 non-goals compressed to flat bulleted list (no rationale paragraphs).
- §5.1 mutation matrix and admin-RPC matrix merged into single canonical Table A; redundant prose removed.
- §6 Time zones reduced from 6 bullets to 2.
- §1, §3, §4.2, §4.4, §4.5, §5.2, §5.3, §5.5, §5.6, §5.8, §6 Security/Idempotency/Pagination/Versioning/Observability, §7, §8, §10 trimmed of redundant prose throughout.

**v1.8 rationale (extracted from PRD preamble)**:
- `dmTemplate` override made contributor-optional (open source).
- Soft-delete restore formally non-goal.
- Custom-domain feature dropped (Extend-hostname only).

**v1.7 rationale (extracted from PRD preamble)**:
- `survey.create` admin attribution.
- DM-queue restart sweep.
- M2 sub-caps 6/7 (TopUpCodes, SyncFromAGS).
- M2 audit `applicant.approve` / `applicant.reject`.

---

<details>
<summary>v1.5 changelog (from v1.4)</summary>

**Inconsistencies fixed**:
- §5.1 — Soft-delete absoluteness: a CLOSED-then-soft-deleted playtest revokes direct-link access for previously-approved players. `GetGrantedCode` returns 404 for any soft-deleted playtest regardless of applicant state.
- §5.1 AuditLog comment — clarified `applicant.dm_sent` is admin-attributed (actorUserId = Retry DM clicker); no longer ambiguous with system-emitted list.
- §4.6 step 1 — `CreatePlaytest` ALWAYS returns `status = DRAFT`; no `status` field accepted in create request. Only `TransitionPlaytestStatus` leaves DRAFT.
- §4.6 — Partial-fulfillment detection: rollback trigger is HTTP non-2xx OR AGS error field set. HTTP 2xx with codes commits; `count < requested` emits warning.
- §5.1 — Permission matrix split into **Table A (admin actions by status)** and **Table B (audit events + attribution)**. Audit events fire whenever their trigger fires; status gating is expressed in Table A only.
- §5.4/§5.5/§8/§10 — "Pool-only grant" defined once in §4.1 step 6; other occurrences now cross-reference without restating.

**Content moved to `schema.md`** (new file):
- §5.1 AuditLog `action` enum + JSONB payload shapes for each action.
- §5.5 `Code` table schema + `leader_lease` table schema.
- §5.6 `Survey` + `SurveyResponse` entity specs (column types, question shape, multi-choice option bounds).

**Content deleted from PRD**:
- Appendix B (Resolved questions Q1–Q40).
- Top-of-PRD changelog summary block (mirrored CHANGELOG.md).
- §6 axe-core `^4.10.0` version pin — replaced with "pinned in CI; see CI config".

**Minor gaps closed**:
- §4.6 — AGS_CAMPAIGN top-up concurrency: `pg_advisory_xact_lock(hashtext(playtestId))`, same discipline as CSV upload.
- §5.4 — Discord DM internal throttle: DMs queued post-approval, worker emits at configurable safe rate (≈5/s default); approval RPC returns immediately.
- §8 — Bounds rationale: `initialCodeQuantity 1–50,000`, 100 playtests/namespace, 10 MB / 50k-line CSV are MVP safety limits, not AGS-imposed.
- §6 Performance — Perf target raised to **p95 < 3s end-to-end (user-perceived)**, inclusive of AGS IAM + Discord OAuth redirect time. §7 and §10 proof points updated.
- §6 Time zones — Admin input in admin browser TZ; server stores UTC; `endsAt is past` evaluated against server UTC clock; players see UTC-derived state.
- §5.3 — NDA hash: normalize before sha256 (trim trailing whitespace per line, CRLF→LF, collapse trailing newlines to a single terminal LF).
- §6 Accessibility — Admin UI fully excluded from automated a11y CI (audit log viewer, survey builder, etc.); no manual a11y smoke-test required.
- §5.2 — `discordHandle` storage: raw UTF-8 from Discord API, no sanitization; column is Postgres `TEXT`. Deleted Discord accounts: no reconciliation — archival text.
- §6 Versioning & compatibility — gRPC versioned in proto package (`v1`, `v2`); breaking changes = new package; no formal compat SLA; single deployment owns backend + player app.
- §4.6 — AGS API retry policy: 30s timeout, up to 3 retries with exponential backoff on 5xx/timeout; 4xx no retry. Initial-create sequence keeps its own 300s-no-retry policy.

**New files**:
- `docs/schema.md` — full schema definitions (AuditLog action enum + payload shapes, Code/leader_lease, Survey entity).
- `docs/STATUS.md` — build/implementation status tracker (milestones M0–M4 from §10, all `not started` on v1.5 cut).

**PRD meta**:
- Top-of-PRD now points at `CHANGELOG.md`, `schema.md`, and `STATUS.md` from a single line under the version header.

</details>

<details>
<summary>v1.4 changelog (from v1.3)</summary>

- §2 — Added "External code redemption tracking" to non-goals.
- §4.1 — Rejection UX note: player sees generic "not selected" message; `rejectionReason` is admin-only.
- §4.1 step 6d — DM template details consolidated into §5.1; §4.1 now cross-references.
- §4.6 — TopUpCodes split into generate-only RPC; fetch-first behavior moved to SyncFromAGS. Admin UI convenience action wires both sequentially.
- §4.7 — TopUpCodes idempotency updated to "not idempotent (generates new codes each call)".
- §5.1 — `distributionModel` immutable after creation (unconditionally; was "after any Code row exists").
- §5.1 — PENDING applicants remain PENDING indefinitely on soft-delete (explicit caveat).
- §5.1 — Permission matrix: `campaign.create` and `campaign.create_failed` OPEN → **no** (fires only during CreatePlaytest in DRAFT).
- §5.2/§5.3 — NDA re-accept logic deduplicated; §5.3 is canonical, §4.1 and §5.2 cross-reference.
- §5.5 — Code state machine prose trimmed; cross-references §4.1 step 6.
- §5.6 — Multi-choice option bounds: 2–20 entries, server-enforced.
- §5.9 — Added `DB_MAX_CONNECTIONS` (default `10`) env var.
- §6/§5.4/§5.5/§10 — "Pool-only grant" defined once in §4.1, referenced elsewhere (trimmed repetition).
- §8 — Extend SDK handles AGS token refresh automatically (assumption).
- §8 — AGS Platform Campaign API deduplicated (removed from External dependencies, cross-references AGS services).
- §9 — R11: namespace decommission means data loss; self-host operators responsible for backups.
- §10 M1 — Discord handle fetched once at signup, never refreshed.
- Appendix A — Full changelogs moved to `CHANGELOG.md`.

</details>

<details>
<summary>v1.3 changelog (from v1.2)</summary>

- §10 M1 — Discord handle lookup: fallback to raw Discord user ID on API failure (best-effort).
- §5.5/§5.9 — Backend config mechanism: all config via environment variables; new §5.9 documents required/optional env vars.
- §5.4 — Retry DM: explicitly no cooldown (intentionally unlimited, each attempt audited).
- §5.6 — Survey question IDs: server-generated UUIDs, preserved across version bumps for kept questions.
- §4.6 — Partial code fulfillment: accept partial set + warn admin.
- §6 Pagination — Soft cap of 100 non-deleted playtests per namespace.
- §5.7 — Responses viewer shows "Survey version" column per response row.
- §5.1 — Permission matrix: `applicant.dm_failed`, `applicant.dm_sent`, `RetryDM`, `code.grant_orphaned` → n/a in DRAFT; `code.upload_rejected` → n/a in CLOSED.
- §4.1/§5.1 — Cross-references between dmTemplate 1800-char save validation and 2000-char post-expansion overflow check.
- §4.6 — `initialCodeQuantity` bounds: canonical definition in §5.1, §4.6 now references it.
- §5.2 — IAM-down login failure: generic error + retry message.
- §5.6 — Mid-fill version race explicitly applies in CLOSED (admin can edit survey after closure).
- §8 Stack — Goroutine-per-request concurrency model stated; no global cap in MVP.

</details>

<details>
<summary>v1.2 changelog (from v1.1)</summary>

- C1 §4.1 step 6b — Code GRANTED + Applicant update in same DB transaction; rollback keeps Code RESERVED.
- C2 §4.7 — Added RPC summary table.
- C3 §4.6 — AGS code generation+fetch timeout raised to 300s.
- C4 §4.6 — Top-up non-transactional; each batch independent; retry+dedup handles gaps.
- M1 §5.2 — NdaReacceptRequired uses `IS DISTINCT FROM` for NULL handling.
- M2 §10 M1 — Discord handle via bot token API call, not IAM claims.
- M3 §5.1 — Added `survey.create` audit event.
- M4 §6 — Survey responses cursor `(submittedAt, id)`.
- M5 §5.1 — DRAFT playtests return 404 on direct link.
- M6 §5.5 — Removed `entitlementId` from Code schema.
- M7 §5.1/§4.1 — DM expansion overflow treated as failure at 2000 chars.
- M8 §5.1 — `code.upload` scoped to STEAM_KEYS.
- M9 §5.1 — `description` max 10,000 chars.
- M10 §5.1/§5.4 — CLOSED blocks approve/reject, uploads, AGS operations.
- M11 §5.1 — `platform` → `platforms` (TEXT[] array).
- M12 §4.7 — Removed `CreateAGSCampaignCodes` from RPC table.
- M13 §4.7/§5.2 — Added `GetPlaytestForPlayer` RPC.
- M14 §5.1 — RetryDM allowed in CLOSED.
- M15 §5.1 — `campaign.*` cannot fire in CLOSED footnote.
- M16 §5.1 — `code.upload_rejected` scoped to STEAM_KEYS.
- M17 §5.1/§5.2 — `platforms` semantic distinction (playtest vs. applicant).
- M18 §5.1 — Survey creation/editing allowed in CLOSED.
- M19 — Annotated older `entitlementId` references.
- M20 §5.2 — No server-side `Applicant.platforms` vs `Playtest.platforms` validation.

</details>

<details>
<summary>v1.1 changelog (from v1.0)</summary>

- §4.6 — AGS_CAMPAIGN creation in single DB tx; Sync from AGS recovery.
- §5.3 — NDAAcceptance composite PK `(userId, playtestId, ndaVersionHash)`.
- §5.6 — `createdAt` on Survey; `submittedAt` as pagination cursor.
- §5.2 — Applicant canonical field list with `grantedCodeId`, `approvedAt`, `rejectionReason`.
- §5.4 — REJECTED is terminal.
- §5.8 — `config.json` extended with `iamBaseUrl`, `discordClientId`.
- §2 — Bulk approve added to non-goals.
- §6 — Playtest list unpaginated.
- §8 — golang-migrate for schema migrations.
- §5.1 — `dmTemplate` placeholder set enumerated; `campaign.create` includes `initialCodeQuantity`.
- §9 R8 — RBAC is release blocker for production.
- §10 M2 — Sync from AGS added to M2 scope.

</details>

<details>
<summary>v1.0 changelog (from v0.9)</summary>

- Two distribution models: STEAM_KEYS (CSV upload) + AGS_CAMPAIGN (API-generated codes).
- §5.1 — `distributionModel`, `agsItemId`, `agsCampaignId`, `initialCodeQuantity` fields.
- §4.1 step 6 — Pool-only grant for both models; no AGS Platform call at approve time.
- §4.6 — AGS Campaign code generation flow, partial-failure cleanup, `agsCodeBatchSize = 1000`.
- §5.5 — Code entity serves both models.
- §5.1 — `distributionModel` immutable after creation; AGS audit events added.
- §8 — AGS Platform Campaign API as dependency; Steam NOT a dependency.
- §10 — AGS Campaign integration moved to M2; M1 is STEAM_KEYS only.

</details>

<details>
<summary>v0.6–v0.9 changelogs</summary>

**v0.9**: dmTemplate validation (1800 char cap, `{code}` required), slug rejection (no silent lowercase), reclaim-tick log volume, AuditLog.actorUserId nullable, NDAAcceptance field naming.

**v0.8**: Fenced SQL update for approve finalize, gRPC-gateway exposure model (CORS allowlist), DM template placeholders, applicant queue filters (PENDING/APPROVED/REJECTED/DM_FAILED), reclaim-job liveness log, slug admin-chosen, `playtest.restore` removed, named constants (`reclaimInterval=30s`, `reservationTtl=60s`).

**v0.7**: Webhook fallback deferred uniformly, `lastDmStatus` enum cleaned up (`sent|failed|null`), `applicant.dm_sent` audit action added.

**v0.6**: AuditLog expanded to full admin surface, DM failure first-class (`lastDmStatus`/`lastDmAttemptAt`/`lastDmError`), NDA edits blocked in CLOSED, soft-delete UX, idempotency model (natural keys only), CSV advisory lock, `bannerImageUrl` URL-only, `UNIQUE(playtestId, value)` on Code, `config.json` malformed-definition rules.

</details>
