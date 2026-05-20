# Runbook — Bulk Discord announcement broadcast (M5.C)

This runbook documents the **Discord Bot Tools** tab on the M5.C
playtest detail page. Pairs with [`docs/PRD.md`](../PRD.md) §5.4
"Bulk announcements" + §6 Observability and the entity shapes in
[`docs/schema.md`](../schema.md) §"announcement +
announcement_recipient tables".

## When to use it

Use bulk announcements when you need to broadcast a single Discord DM
to every applicant of a playtest (or a status-bucketed subset). The
most common scenarios:

- A new build is live and approved players should re-download.
- Pending applicants need an NDA reminder before approval ramps.
- Closing-soon notice to every signup.

## Quick start

1. Open the detail page (`/playtest/<slug>`) and switch to **Discord Bot
   Tools**.
2. Pick a recipient filter (`All` / `Approved only` / `Pending only`).
   The filter resolves **at submit time** against the current applicant
   set — adding an applicant later does NOT auto-include them.
3. Fill in **Subject** (1–200 chars) + **Message** (1–4,000 chars).
   The form surfaces the byte-exact `errors.md` strings if either bound
   is violated.
4. Click **Send via DM**. The form clears on success and the history
   table refreshes.

## Aggregate status semantics

`announcement.status` is recomputed from the per-recipient
`announcement_recipient.dm_status` spread:

| Aggregate | Predicate over recipient rows                   |
| --------- | ----------------------------------------------- |
| `SENT`    | Every recipient row is `SENT`.                  |
| `SENDING` | At least one row is still `QUEUED`.             |
| `PARTIAL` | No `QUEUED` rows; mix of `SENT` and `FAILED`.   |
| `FAILED`  | Every row is `FAILED`.                          |

The history table renders the aggregate as a colour-coded tag.

## PII guarantee

`subject` and `message` are treated as PII per PRD §6 Observability:

- They are **never written** to structured logs, metrics, or audit
  JSONB. The `announcement.create` audit row records IDs + counts
  only (admin-attributed via `actorUserId`).
- They live exclusively on the `announcement` table. Per-recipient
  delivery state lives on `announcement_recipient`, which carries
  `dm_status` + `dm_error_code` (a short machine-readable tag like
  `missing_recipient` / `no_mutual_guild` / `timeout` /
  `send_error`) — never the body text.
- Operators may include NDA prompts, project codenames, or unreleased
  build URLs in the message; the PII rule keeps the body out of every
  log surface so accidental disclosure during incident review is not
  possible.

## Discord rate-limit awareness

The form surfaces the documented notice: "Delivery may take a few
minutes depending on Discord rate limits." The fan-out path goes
through the existing Discord bot client; per-recipient retries on
transient errors follow the same path as approve DMs.

## Closed-playtest write block

`CreateAnnouncement` on a `CLOSED` playtest returns `FailedPrecondition`
with the byte-exact `errors.md` string
`playtest is closed; announcements can no longer be sent`. The form
disables itself with the matching banner once the status pill reads
`Closed`. Reading the announcement history is always allowed
regardless of status.

## Retention

Announcements are kept forever. Rows are tiny (≤4,200 chars per row)
and announcements are forensically valuable for compliance reviews.
There is no cleanup worker; revisit only if a studio's `announcement`
table grows past ~1M rows.

## CLI mirror

The full surface is also available via `pth`:

```bash
pth --namespace mygame --profile admin announcement create \
    --playtest-id <UUID> \
    --send-to APPROVED_ONLY \
    --subject "Build update" \
    --message "New patch is live; please download from the playtest page."

pth --namespace mygame --profile admin announcement list \
    --playtest-id <UUID>
```

`--dry-run` echoes the `CreateAnnouncementRequest` body without
dialling the server — useful for piping into AI/code review without
actually firing the broadcast.

## When something goes wrong

| Symptom                                            | Likely cause                                                                                          |
| -------------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| Aggregate stuck at `SENDING`                       | The fan-out is still draining; refresh the history table.                                            |
| Every row landed `FAILED` with `missing_recipient` | The applicants signed up before `migration 0004`; no Discord snowflake on the row.                   |
| Mixed `SENT` + `FAILED` with `no_mutual_guild`     | The bot doesn't share a guild with those recipients (see [`setup-ags-discord.md`](setup-ags-discord.md) §7 "Discord bot + server"). |
| `400 announcement subject must not be empty`       | The server-side validator agrees with the client-side rule; surface in `docs/errors.md` M5.C row.    |
| `400 announcement message must be at most 4000 characters` | The textarea allows typing past the cap because antd `showCount` only warns; the validator rejects on submit. |
