# DM queue mechanics

Detailed Discord DM queue behavior. Referenced from PRD §5.4.

## Bounded in-memory FIFO

- The DM send queue is an **in-memory bounded FIFO**.
- **Default max depth: 10,000** pending sends (tunable via backend configuration).
- Worker drains at a configurable safe rate (default ≈5 DMs/sec; see PRD §5.9) to stay within Discord's DM rate limits.
- Approval RPCs return immediately; DM delivery is asynchronous.

## Throttling

- Discord DM sends are internally throttled.
- Approve RPC enqueues a DM task; a worker drains the queue at a safe rate.
- A DM that fails inside the worker follows the standard `lastDmStatus='failed'` + `applicant.dm_failed` path (PRD §4.1 step 6d), surfaced via the "DM failed" filter.

## Overflow behavior

- When an enqueue would exceed the max depth, the approve flow does **not** block.
- The enqueue **fails immediately**.
- The applicant is marked `lastDmStatus='failed'` with `lastDmError='dm_queue_overflow'`.
- An `applicant.dm_failed` audit row is written (same path as any other DM failure).
- The admin retriages via the "DM failed" filter and can use "Retry DM" once the queue drains.

## Restart behavior (in-memory loss)

- The queue is in-memory and **not persisted**.
- On backend restart or crash, any pending (un-sent) DM tasks are **lost**.

### Startup sweep

On process restart the backend scans all `APPROVED` applicants and re-marks lost DMs:

- **Idempotency guard**: the sweep only re-marks applicants where `lastDmStatus IS NULL` or `'pending'`. Applicants already at `lastDmStatus='failed'` are **not** touched, preserving the original error reason (e.g. `dm_queue_overflow`).
- Affected applicants are transitioned to `lastDmStatus='failed'` with `lastDmError='lost_on_restart'`.
- Standard `applicant.dm_failed` audit row is written per §4.1 step 6d.
- The standard "DM failed" filter and Retry-DM button surface them for re-send.
- No pending-state applicants are hidden from admins.
- The Retry-DM gate (`lastDmStatus='failed' AND status=APPROVED`) is unchanged.

## Circuit breaker

- **Trip condition**: 50 consecutive DM send failures within 60s.
- **Action on trip**: pause queue draining for 5 minutes; auto-resume.
- **Admin work not blocked**: while tripped, new approves still enqueue (so admin work isn't blocked) but DMs don't drain.
- **Surface**: any DM attempted while tripped is marked `lastDmStatus='failed'` with `lastDmError='dm_circuit_open'`.
- **Audit events**: `dm.circuit_opened` and `dm.circuit_closed` (system-attributed).

## Bulk retry RPC

- `RetryFailedDms(playtestId)` retries every applicant with `lastDmStatus='failed'` for the given playtest.
- Reuses per-applicant retry semantics (PRD §5.4 — RetryDM).
- Admin UI exposes a **"Retry all failed DMs"** button on the Applicants page.

## `lastDmError` truncation

- `lastDmError` is byte-truncated to **500 chars** (PRD §5.2 — Applicant entity).
- Truncation preserves a **valid UTF-8 boundary**: multi-byte codepoints are not cut mid-codepoint. If the truncation point falls inside a multi-byte sequence, the truncation is shifted backward to the nearest codepoint boundary.
