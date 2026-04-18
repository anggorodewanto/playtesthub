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

## AuditLog — `action` enum with JSONB payload shapes

The full set of audited admin actions in MVP. Each row's `before` and `after` JSONB columns carry the payload shape documented here. The `AuditLog` table schema is defined above; PRD §5.1 carries the accompanying prose rules (permission matrix, accountability note).

- `playtest.edit` — non-NDA field edits on a `Playtest` row.
- `nda.edit` — `ndaText` change; `before`/`after` store the **full old and new NDA text**.
- `playtest.soft_delete` — `deletedAt` set.
- `playtest.status_transition` — `DRAFT → OPEN` or `OPEN → CLOSED`; `before`/`after` record the status values.
- `applicant.approve` — records `{applicantId, grantedCodeId}`. **The raw code value is never written to the audit log** (cross-reference PRD §6 Observability log-redaction policy — code values are forbidden in logs, and this table carries the same prohibition for code values).
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
  createdAt         TIMESTAMP
}
```

**Admin-visible fields** (returned by `ListApplicants` / `GetApplicantStatus` when caller is admin): all fields above.

**Player-visible fields** (returned by `GetApplicantStatus` when caller is the applicant themselves): `status`, `grantedCodeId` (presence only — value retrieved via `GetGrantedCode`), `approvedAt`, `ndaVersionHash` (for the §5.3 re-accept client-side check). **Not visible to the player**: `rejectionReason`, `lastDmStatus`, `lastDmAttemptAt`, `lastDmError`, `discordHandle`, `platforms`.

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
