# playtesthub — Schema Definitions

Full schema and action-enum definitions moved out of the PRD for readability. This document is the **authoritative source of truth** for:

- The `AuditLog` table schema, `action` enum, and the JSONB `before`/`after` payload shapes for each action.
- The `Applicant` entity schema (admin-visible vs player-visible field distinction).
- The `Code` table schema.
- The `leader_lease` table schema.
- The `Survey` entity spec (full column types).
- The approve-flow fenced-finalize SQL (PRD §4.1 step 6b).
- Required indexes for the audit log viewer (PRD §5.7 page 5).

Prose rules — state machines, transaction semantics, reclaim cadence, permission matrix, etc. — remain in the PRD. This document only carries the shapes.

---

## AuditLog table

```
AuditLog {
  id            UUID PK
  namespace     TEXT
  playtestId    UUID?       // nullable for namespace-scoped events
  actorUserId   UUID?       // admin who performed the action; null for system-emitted events
  action        TEXT        // see `action` enum below
  before        JSONB       // full prior state (or metadata for DM/system events)
  after         JSONB       // full new state (or metadata for DM/system events)
  createdAt     TIMESTAMP
}
```

Required indexes (serve the audit log viewer, PRD §5.7 page 5):

- `AuditLog(playtestId, createdAt DESC)` — powers the per-playtest audit viewer listing and cursor pagination.
- `AuditLog(actorUserId, createdAt DESC)` — powers actor-filtered views (admin who triggered an action).

### AGS user-id wire format

Postgres `user_id` / `actor_user_id` columns are typed `UUID` and serialize back in canonical 36-char dashed form on read. AGS itself uses the dashless 32-char hex variant (JWT `sub` claim, IAM API paths). Every customer-visible surface — gRPC response fields, structured logs, and `AuditLog` JSONB payload fields that carry an *AGS user id* (`actorUserId`/`actor_user_id`, the `userId` field on `code.grant_orphaned`, `linkedBy` on `adt_linkage.create`, `createdBy` on `announcement.create`) — is normalised to the dashless form via `pkg/agsid.Format` so admins see the same id shape the AGS portal shows. Internal entity ids (`applicantId`, `codeId`, `playtestId`, `adtLinkageId`, `announcementId`, `surveyId`) are dashed UUIDs and stay dashed everywhere. Request inputs accept both forms (`uuid.Parse` is liberal). Audit JSONB rows persisted **before** this normalisation landed retain dashed AGS user ids in their payloads — historical drift is acceptable since audit rows are frozen per PRD §6 and never backfilled.

## AuditLog — `action` enum with JSONB payload shapes

The full set of audited admin actions in MVP. Each row's `before` and `after` JSONB columns carry the payload shape documented here. The `AuditLog` table schema is defined above; PRD §5.1 carries the accompanying prose rules (permission matrix, accountability note).

- `playtest.edit` — non-NDA field edits on a `Playtest` row.
- `nda.edit` — `ndaText` change; `before`/`after` store the **full old and new NDA text**.
- `playtest.soft_delete` — `deletedAt` set.
- `playtest.status_transition` — `DRAFT → OPEN` or `OPEN → CLOSED`; `before`/`after` record the status values. `actorUserId` is the admin user id when the transition is driven by `TransitionPlaytestStatus`, and **NULL (system-emitted)** when driven by the `internal/window/` worker hitting a configured `startsAt` / `endsAt` boundary (PRD §5.1 "Window-driven auto-transition").
- `playtest.adt_build_change` — written when `ChangeADTBuild` (PRD §4.8.6) repoints an ADT playtest at a different `(adtGameId, adtBuildId)` pair under its existing — and immutable — `adtNamespace`. Records `{playtestId, adtNamespace, beforeGameId, beforeBuildId, afterGameId, afterBuildId, changedBy}` — identity columns only; `adtNamespace` is the operator-supplied linkage identifier (not a credential, not PII), and the before/after game+build ids let an auditor reconstruct exactly what was swapped. **Admin-attributed**: `actorUserId` = the admin who called the RPC (`changedBy` echoes the same id).
- `applicant.approve` — records `{applicantId, grantedCodeId?, adtUrls?, adtUrlSource?}`. For STEAM_KEYS / AGS_CAMPAIGN playtests `grantedCodeId` is populated and `adtUrls` / `adtUrlSource` are absent; for ADT playtests `grantedCodeId` is absent and `adtUrls` + `adtUrlSource` (`'issued' | 'fallback'`) are populated (PRD §4.8.3). `adtUrls` is a JSON array carrying every URL `adt.Client.IssueDownloadURL` returned in ADT's original order — a single-element list for single-file builds, multiple elements for multi-asset builds, or the static `adtFallbackDownloadUrl` wrapped in a single-element list when `adtUrlSource = 'fallback'`. **The raw code value is never written to the audit log** (cross-reference PRD §6 Observability log-redaction policy — code values are forbidden in logs, and this table carries the same prohibition for code values). **The ADT download URL list IS written** — URLs ≠ codes; forensics require the URL surface to investigate per-applicant download issues.
- `applicant.auto_approved` — records `{applicantId, autoApprovedAt, codeId? (present for STEAM_KEYS / AGS_CAMPAIGN; NULL for ADT — there is no `Code` row to reference), adtUrls? (present for ADT only; the full list of per-applicant or fallback download URLs minted at auto-approve time — same shape as the `applicant.approve` row's `adtUrls`), adtUrlSource? (`'issued' | 'fallback'`; present for ADT only)}`. Written by the signup-time auto-approve path (PRD §5.4 "Auto-approve") when the playtest has `autoApprove=true` and the cap check + grant chain (reserve → fenced finalize for STEAM_KEYS / AGS_CAMPAIGN; `IssueDownloadURL` or static-fallback resolution for ADT — PRD §4.8.3) succeeds. **System-emitted** (`actorUserId = NULL`) — auto-approval is not attributed to any individual admin. **Distinct from `applicant.approve`** so audit-log filters can separate manual vs auto attribution; a successful auto-approve writes exactly one `applicant.auto_approved` row and **no** `applicant.approve` row. **Raw code value is never written** (same redaction rule as `applicant.approve`). **URLs are NOT redacted** — URLs ≠ codes (PRD §4.8.3).
- `applicant.reject` — records `{applicantId, rejectionReason}`.
- `applicant.dm_failed` — written when the Discord DM send fails; records `{applicantId, error (truncated to 500 chars, byte-truncation preserving valid UTF-8 codepoint boundaries), attemptAt}`. See PRD §4.1 step 6d and §5. **System-emitted**.
- `dm.circuit_opened` — written when the DM circuit breaker trips (50 consecutive failures within 60s); records `{trippedAt, recentFailureCount}`. See `dm-queue.md`. **System-emitted**.
- `dm.circuit_closed` — written when the DM circuit breaker auto-resumes (after 5 minutes); records `{closedAt}`. See `dm-queue.md`. **System-emitted**.
- `applicant.dm_sent` — written on a **successful manual Retry DM only** (PRD §5.4), to record the failed→sent transition; records `{applicantId, discordUserId}`. **Not written on initial approve** — initial-approve DM success is implicit in `applicant.approve`. **`actorUserId` = the admin who clicked Retry DM** (admin-attributed, not system-emitted).
- `code.upload` — CSV batch ingest (**STEAM_KEYS only**); records `{count, sha256(csvBytes), filename}`. **Raw code values are never written to this row** (same redaction rule as above; cross-reference PRD §6 Observability).
- `code.upload_rejected` — (**STEAM_KEYS only**) written when a CSV upload is rejected end-to-end (see PRD §4.3); records `{filename, reason, rowCount}` where `reason` is a short machine-readable tag (e.g. `"size_exceeded"`, `"count_exceeded"`, `"charset_violation"`, `"duplicate"`, `"non_utf8"`) and `rowCount` is the number of rows parsed before rejection (or `0` if the file failed pre-parse validation). **System-emitted**; no raw code values in payload.
- `code.grant_orphaned` — written by the finalize step when the fenced SQL update (PRD §4.1 step 6b) affects 0 rows, indicating the reservation was reclaimed/stolen between reserve and finalize; records `{applicantId, codeId, userId, originalReservedAt}`. **System-emitted**; no raw code values.
- `campaign.create` — written when playtesthub successfully creates an AGS Item + Campaign; records `{agsItemId, agsCampaignId, itemName, initialCodeQuantity}`. **System-emitted**.
- `campaign.create_failed` — written when AGS Item/Campaign creation fails; records `{error, cleanupAttempted, cleanupSuccess}`. **System-emitted**.
- `campaign.generate_codes` — written on successful code generation, covering **both initial generation (at playtest creation) and top-up**; records `{agsCampaignId, quantity, totalPoolSize}`. **System-emitted**.
- `campaign.generate_codes_failed` — written on failed code generation, covering **both initial generation failure (at playtest creation) and top-up failure**; records `{agsCampaignId, requestedQuantity, error}`. **System-emitted**.
- `survey.create` — written when a survey is first created for a playtest; records `{playtestId, surveyId, questionCount}`. **System-emitted**.
- `survey.edit` — records the **full before/after question set**. Intentional — survey questions are not secret, and full diffs are the accountability mechanism for survey changes.
- `adt_linkage.create` — written when `CompleteADTLink` successfully inserts an `adt_linkage` row (PRD §4.8.2). Records `{adtLinkageId, studioNamespace, adtNamespace, linkedBy}` — identity columns only; **no credential payload exists to leak** (the linking flow exchanges no credential — auth to ADT on every subsequent API call is the AGS service IAM JWT; PRD §4.8). **Admin-attributed**: `actorUserId` = the admin who completed the link (recovered from the `adt_link_pending.started_by_user_id` column at commit time).
- `adt_linkage.delete` — written when `UnlinkADT` soft-deletes an `adt_linkage` row (PRD §4.8). Records `{adtLinkageId, studioNamespace, adtNamespace}`. **Admin-attributed**: `actorUserId` = the admin who called the RPC. Idempotent on the audit side too — a second `UnlinkADT` against an already-deleted linkage is a no-op and writes no row.
- `adt_linkage.recover` — written when `RecoverADTLinkage` adopts an orphan ADT-side linkage flag and inserts the local row (PRD §4.8). Records `{adtLinkageId, studioNamespace, adtNamespace, linkedBy}` — payload mirrors `adt_linkage.create` so audit consumers can apply the same shape; the distinct action string lets a filter separate orphan-recovery from the regular create flow. **Admin-attributed**: `actorUserId` = the admin who called the recovery RPC.
- `announcement.create` — written when `CreateAnnouncement` (PRD §5.4 "Bulk announcements") successfully resolves the recipient set, inserts the `announcement` row, and fans `announcement_recipient` rows out. Records `{announcementId, playtestId, sendToFilter, recipientCount, createdBy}` only. **Admin-attributed**: `actorUserId` = the admin who authored the broadcast. **`subject` and `message` are NEVER written to the audit JSONB** — PRD §6 Observability extends the existing code-redaction rule to admin-authored DM content because operators may include NDA prompts / project codenames / build URLs in the body. Forensic recovery of "what was sent to whom" lives entirely on the `announcement` + `announcement_recipient` tables; the audit row records only that a broadcast happened, when, and by whom.

---

## Applicant entity

Admin-visible vs player-visible field distinction (prose rules in PRD §5.2 and §5.4):

```
Applicant {
  id                UUID PK
  playtestId        UUID FK
  userId            UUID
  discordHandle     TEXT         // raw UTF-8 from Discord API or raw Discord user ID on lookup failure; no sanitization
  platforms         TEXT[]       // STEAM | XBOX | PLAYSTATION | EPIC | OTHER — applicant-owned, not validated against Playtest.platforms
  ndaVersionHash    TEXT?
  status            ENUM         // PENDING | APPROVED | REJECTED — default PENDING
  grantedCodeId     UUID? FK
  approvedAt        TIMESTAMP?
  rejectionReason   TEXT?        // admin-visible, max 500 chars
  lastDmStatus      ENUM         // sent | failed | null
  lastDmAttemptAt   TIMESTAMP?
  lastDmError       TEXT?        // byte-truncated to 500 chars preserving valid UTF-8 codepoint boundaries
  autoApproved      BOOLEAN      // NOT NULL DEFAULT FALSE — true iff this applicant was approved by the M5.A auto-approve path (Playtest.autoApprove=true at signup, under cap). Distinct from manual ApproveApplicant which leaves this false. Drives the autoApproveLimit count predicate in PRD §5.4 "Auto-approve".
  adtDownloadAt              TIMESTAMP?  // M6-targeted ADT telemetry cache; ships dormant in M5.C (always NULL until M6 worker lands). First-time download timestamp surfaced via the ADT telemetry endpoint.
  adtTotalPlaytimeSeconds    INTEGER?    // M6-targeted ADT telemetry cache; ships dormant in M5.C. Aggregated playtime per ADT telemetry.
  adtHardwareSpecs           JSONB?      // M6-targeted ADT telemetry cache; ships dormant in M5.C. Hardware-spec snapshot per ADT telemetry (CPU / GPU / RAM / OS / Storage shape; rendered as the Hardware Specs section in the M6 participant detail modal).
  adtCrashCount              INTEGER     // NOT NULL DEFAULT 0; M6-targeted ADT telemetry cache; ships dormant in M5.C. Per-applicant crash-report count.
  lastSurveyDmId    UUID?       // M5 Track D phase 3 (migration 0008); the survey.id we last queued a survey-publish DM for. Idempotency contract — see paragraph below the table.
  lastSurveyDmAt    TIMESTAMP?  // M5 Track D phase 3 (migration 0008); wall-clock UTC of the most recent MarkSurveyDMSent. Forensic only — no production code path reads it.
  createdAt         TIMESTAMP
}
```

**Admin-visible fields** (returned by `ListApplicants` / `GetApplicantStatus` when caller is admin): all fields above.

**Player-visible fields** (returned by `GetApplicantStatus` when caller is the applicant themselves): `status`, `grantedCodeId` (presence only — value retrieved via `GetGrantedCode`), `approvedAt`, `ndaVersionHash` (for the §5.3 re-accept client-side check). **Not visible to the player**: `rejectionReason`, `lastDmStatus`, `lastDmAttemptAt`, `lastDmError`, `discordHandle`, `platforms`, `autoApproved`, and all four ADT telemetry cache columns.

The four ADT telemetry columns (`adtDownloadAt`, `adtTotalPlaytimeSeconds`, `adtHardwareSpecs`, `adtCrashCount`) ship in migration 0007 (M5.C) but stay NULL / zero across the M5.C window — there is no telemetry client, worker, or refresh path in M5.C. The columns exist so M6's worker + endpoint hookup lands with zero schema churn. The `GetPlaytestParticipants` response shape includes them so the proto wire is stable across M5.C → M6; admin UI ignores them in M5.C.

**`lastSurveyDmId` / `lastSurveyDmAt` idempotency contract** (M5 Track D phase 3 / migration 0008; STATUS_M5.md Track D D3). The pair is the per-applicant idempotency stamp for the survey-publish DM channel — distinct from `lastDmStatus` (which tracks approval / retry DMs). The single rule: when `lastSurveyDmId == playtest.surveyId` the applicant has already been queued for a survey-publish DM for the current survey version and the fan-out skips them; when `lastSurveyDmId IS NULL` (or holds a stale survey id) and the applicant is APPROVED + NDA-current, the fan-out is eligible to enqueue. The column is informational, not relational — no `FOREIGN KEY` references `survey(id)` because surveys are versioned (every `EditSurvey` creates a new row) and old rows live forever per PRD §5.6. `CreateSurvey` is the only trigger that fans out a survey-publish DM; `EditSurvey` is silent so editorial iteration on prompt copy never spams recipients. The boot-time restart sweep picks up applicants the in-process fan-out missed (queue overflow, process restart mid-stamp, applicants who pre-date a server with the new column wiring).

---

## Approve flow — fenced finalize SQL

Canonical fenced SQL update executed in the approve path (PRD §4.1 step 6b). Keyed on the original reservation identity so that a reclaim-and-steal between reserve and finalize affects 0 rows:

```sql
UPDATE Code
   SET state = 'GRANTED',
       grantedAt = now()
 WHERE id = :codeId
   AND state = 'RESERVED'
   AND reservedBy = :userId
   AND reservedAt = :originalReservedAt
```

On 1 row affected, the applicant row is updated (`status=APPROVED`, `grantedCodeId`, `approvedAt`) **in the same DB transaction**. On 0 rows affected, write a `code.grant_orphaned` audit row and return gRPC `Aborted` per [`errors.md`](errors.md).

---

## Code table

The `Code` entity is the authoritative per-playtest pool backing all grants. It serves both distribution models; see PRD §5.5 for the state-machine prose.

```
Code {
  id           UUID PK
  playtestId   UUID FK
  value        TEXT       // free-form string (Steam key for STEAM_KEYS; AGS-generated alphanumeric code for AGS_CAMPAIGN)
  state        ENUM       // UNUSED | RESERVED | GRANTED
  reservedBy   UUID?      // userId currently holding the reservation
  reservedAt   TIMESTAMP? // when reservation began
  grantedAt    TIMESTAMP? // when the pool-only grant finalized
  createdAt    TIMESTAMP

  UNIQUE (playtestId, value)  -- explicit DB constraint: a given code value can appear at most once per playtest
}
```

**Post-MVP note**: if a distribution model requires tracking an external entitlement ID, add `entitlementId TEXT?` back to the Code schema.

---

## leader_lease table

Used exclusively by the reclaim-job leader election described in PRD §5.5.

```
leader_lease {
  name         TEXT PK    // e.g. "reclaim-job"
  holder       TEXT       // replica/process identifier
  acquiredAt   TIMESTAMP
  expiresAt    TIMESTAMP  // short TTL, refreshed by the active leader
}
```

---

## adt_linkage table

Identity row for a successful studio ↔ ADT-namespace link. See PRD §4.8 for the linking flow and the no-credential-storage rationale.

```
adt_linkage {
  id                 UUID PK
  studio_namespace   TEXT NOT NULL    // derived server-side from the backend's AGS service IAM JWT (union_namespace ?? namespace) — NOT the calling admin's request token; see PRD §4.8.1
  adt_namespace      TEXT NOT NULL    // echoed by ADT on the redirect-back URL; validated indirectly via subsequent ADT API calls
  linked_by_user_id  UUID NOT NULL    // AGS user id of the admin who completed the link
  linked_at          TIMESTAMP NOT NULL
  deleted_at         TIMESTAMP?       // soft-delete set by UnlinkADT; row preserved for audit chain integrity

  UNIQUE (studio_namespace, adt_namespace) WHERE deleted_at IS NULL
  -- partial unique index so a studio can re-link the same adt_namespace after unlink (the old row stays for audit)
}
```

**Identity-only row by design**: no `adt_credential_*` columns, no ciphertext, no KEK version. Every ADT API call from playtesthub is authed by minting a fresh AGS service IAM JWT (existing `AGS_IAM_CLIENT_*` env vars) and sending it as `Authorization: Bearer …` — ADT validates against AGS IAM JWKS and reads studio identity from `iss` / `union_namespace` claims (PRD §4.8.2). Migration unit tests assert the absence of any `adt_credential_*` column as a regression canary against future drift.

---

## adt_link_pending table

Short-lived nonce store for the linking redirect round-trip (PRD §4.8.2). One row per in-flight `StartADTLink` call; consumed by the matching `CompleteADTLink` or swept after `ADT_LINKAGE_PENDING_TTL_SECONDS` (default 600).

```
adt_link_pending {
  state                 TEXT PK         // 32-byte CSRF-style nonce, base64-encoded; carried by the redirect through ADT and back
  studio_namespace      TEXT NOT NULL
  started_by_user_id    UUID NOT NULL   // AGS user id of the admin who clicked Proceed
  expires_at            TIMESTAMP NOT NULL
}
```

**Sweep policy**: each `CompleteADTLink` call runs an inline `DELETE FROM adt_link_pending WHERE expires_at < now()` to keep the table small (no background sweeper needed at this cardinality). `state` is single-use — `CompleteADTLink` consumes the row on success.

---

## announcement + announcement_recipient tables

Backs PRD §5.4 "Bulk announcements". One row in `announcement` per admin-authored broadcast; N rows in `announcement_recipient` (one per resolved applicant) carry per-recipient DM delivery state. The fan-out reuses the M2 RetryDM machinery — each recipient row is paired with a `dm_outbox` row enqueued through the same circuit-broken queue that backs approve-DMs.

```
announcement {
  id                    UUID PK
  playtest_id           UUID NOT NULL REFERENCES playtest(id)
  send_to_filter        TEXT NOT NULL CHECK (send_to_filter IN ('ALL', 'APPROVED_ONLY', 'PENDING_ONLY'))
  subject               TEXT NOT NULL CHECK (length(subject) BETWEEN 1 AND 200)
  message               TEXT NOT NULL CHECK (length(message) BETWEEN 1 AND 4000)
  status                TEXT NOT NULL CHECK (status IN ('SENDING', 'SENT', 'PARTIAL', 'FAILED')) DEFAULT 'SENDING'
  recipients_total      INTEGER NOT NULL                  // count resolved at fan-out time; immutable after insert
  recipients_sent       INTEGER NOT NULL DEFAULT 0        // recomputed by the RetryDM worker; load-bearing for the `status` aggregation
  created_by_user_id    UUID NOT NULL                     // admin who authored the broadcast
  created_at            TIMESTAMP NOT NULL DEFAULT now()
}
```

```
announcement_recipient {
  announcement_id   UUID NOT NULL REFERENCES announcement(id) ON DELETE CASCADE
  applicant_id      UUID NOT NULL REFERENCES applicant(id)
  dm_status         TEXT NOT NULL CHECK (dm_status IN ('QUEUED', 'SENT', 'FAILED')) DEFAULT 'QUEUED'
  dm_sent_at        TIMESTAMP?
  dm_failed_at      TIMESTAMP?
  dm_error_code     TEXT?                  // short machine-readable tag mirroring `applicant.lastDmError` semantics
  PRIMARY KEY (announcement_id, applicant_id)
}
```

**Status aggregation** (`ListAnnouncements` computes `announcement.status` at read time via SQL aggregate over `announcement_recipient.dm_status`):

- `SENT` — every recipient row is `'SENT'`.
- `SENDING` — at least one recipient row is `'QUEUED'`.
- `PARTIAL` — no `'QUEUED'` rows, but a mix of `'SENT'` and `'FAILED'`.
- `FAILED` — every recipient row is `'FAILED'`.

**Why a `recipients_sent` cache column despite the aggregate**: the list view (`ListAnnouncements`) does not need the per-recipient roll-up on every request — incrementing `recipients_sent` on each successful DM avoids an `O(N_recipients)` scan per announcement in the list query. The aggregate is still the source of truth for `status`; `recipients_sent` is a denormalized hint for the UI.

**PII guarantee**: `subject` and `message` columns store the admin-authored body. Neither value is ever written into structured logs, metrics, audit JSONB, or DM-error telemetry — `dm_error_code` carries only short machine-readable tags. The audit table records `announcement.create` with IDs + counts only; recovery of the per-applicant delivery state lives entirely on `announcement_recipient`.

---

## Survey entity spec

See PRD §5.6 for the authoring flow, versioning semantics, and one-shot response rules.

`Survey` entity: `{id, playtestId, version (int, starts at 1, for display/ordering only), questions (jsonb ordered array), createdAt (TIMESTAMP)}`.

- **Max 50 questions per survey** (server rejects on save if exceeded).
- Each question: `{id, type, prompt, required, options?}`.
  - **`id`** is a server-generated UUID, assigned when the question is first created. On survey edit (version bump), existing question IDs are **preserved** for questions that are kept (even if reordered); only newly added questions receive new UUIDs. This ensures histogram aggregation keys remain stable across version bumps.
  - **`prompt`** max length: 1,000 chars.
  - For **multi-choice** questions, `options` has shape **`Array<{id: string, label: string}>`** with **2–20 entries** (the server rejects on save if outside this range); **`label`** max length: 200 chars. **Response aggregation is keyed on `option.id`**, not on `label`, so editing a label within a single survey version (or across versions) does not break histograms — the `id` is the stable aggregation key and the `label` is the render-time display string.

`SurveyResponse` entity: `{id, playtestId, userId, surveyId, answers (jsonb), submittedAt}`.

- **`submittedAt`** serves as the pagination cursor for the responses viewer (PRD §5.7 page 3).
- **`surveyId`** is a per-version foreign key — it points at the exact `Survey` row (version) the client fetched and submitted against. There is **no separate `surveyVersion` column**; version is recovered by joining to `Survey`.
- DB-level `UNIQUE (playtestId, userId)` on `SurveyResponse` enforces one survey submission per player per playtest, regardless of survey version bumps. A player who submitted against v1 cannot submit v2.
