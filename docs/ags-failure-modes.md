# AGS failure modes

Detailed AGS Platform / Campaign API failure handling for the AGS_CAMPAIGN distribution model. Referenced from PRD §4.6.

## Retry policy

All AGS API calls **except** the initial-create generation sequence use a uniform policy:

- **30s timeout per call.**
- **HTTP 5xx and timeouts**: up to 3 retries with exponential backoff.
- **HTTP 4xx (including 429)**: fail immediately, no retry.
- **HTTP 429** surfaces to the admin as gRPC `RESOURCE_EXHAUSTED`.

Covers: `TopUpCodes`, `SyncFromAGS`, invalidate/cleanup calls, and any state queries.

**Exception** — the initial playtest-creation generation+fetch sequence retains its own **300s timeout and no retries**. Initial creation is a single all-or-nothing transaction; retry is the admin's job (start over with a fresh Item + Campaign).

## Partial-failure cleanup (during `CreatePlaytest`)

If any AGS API call in step 2 of §4.6 (item creation, campaign creation, or code generation) fails, the **DB transaction is rolled back** (removing the playtest row and any partially-inserted codes) and the admin sees an error. Additionally, any AGS resources created before the failure must be cleaned up.

1. **Item created but Campaign creation failed.** Cleanup: playtesthub attempts to **delete the orphaned AGS Item**. If the delete fails, the backend logs `{agsItemId, error}` at **WARN** level.
2. **Item + Campaign created but code generation failed.** Cleanup: playtesthub attempts to **delete the Campaign first**, then **delete the Item**. If either delete fails, the backend logs `{agsCampaignId, agsItemId, error}` at **WARN** level.

In both cases, the original error is returned to the admin. The admin retries playtest creation from scratch (new Item and Campaign will be created on retry).

## Partial fulfillment

The **rollback trigger is HTTP non-2xx OR an AGS error field being set on the response body**.

- **HTTP 2xx with codes AND no error field**: playtesthub **accepts and ingests whatever codes were returned — the transaction commits**. If `count < requested` (e.g. 4800 out of 5000 without an error), playtesthub emits a **warning** to the admin: `"Requested {requested} codes, received {actual}. You may need to top up."` The pool counter reflects the actual count. The admin can top up to close the gap.
- **HTTP non-2xx OR AGS error field populated**: the transaction rolls back per partial-failure cleanup above. Partial code receipt with an error signal is treated as a failure, not a partial fulfillment.

## Code generation batch size & pagination

- Named constant `agsCodeBatchSize = 1000` — playtesthub requests codes from the AGS CreateCodes API in batches of 1,000 per request.
- When generating more than 1,000 codes (initial or top-up), playtesthub issues **multiple sequential CreateCodes requests** and inserts each batch into the Code table within the open transaction.
- If the AGS CreateCodes API returns codes in paginated responses, playtesthub fetches **all pages** for each batch before inserting that batch.
- **Timeout**: 300s for the entire generation+fetch sequence on initial create; on timeout, the **DB transaction rolls back**. The error is surfaced to the admin. The admin can use **"Sync from AGS"** to recover any AGS-side codes that were generated but lost in the rollback, then retry or top up.

## M2 sub-capability failure matrix

The seven sub-capabilities validated in M2 (see PRD §10) and the partial-ship rules:

| Sub-cap | Capability                 | Failure → ship rule |
| ------- | -------------------------- | ------------------- |
| 1       | Item create                | Any of 1–3 fail → AGS_CAMPAIGN deferred in full to post-v1.0; v1.0 ships STEAM_KEYS-only. |
| 2       | Campaign create            | (as above) |
| 3       | CreateCodes 1000-batch     | (as above) |
| 4       | Code fetch                 | Treat same as 1–3 fail — defer AGS_CAMPAIGN entirely; CODE_UPLOAD remains. |
| 5       | Item / Campaign delete     | Ship initial-generate-only path AND log warning; cleanup hygiene is non-blocking. |
| 6       | TopUpCodes                 | Sub-caps 1–5 pass, 6/7 fail → ship with initial-generate only; top-up + sync deferred post-v1.0; admin UI hides those actions. |
| 7       | SyncFromAGS                | (as above) |
