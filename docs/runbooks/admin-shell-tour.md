# Runbook — Admin shell tour (M5.C)

This runbook walks an operator through the per-playtest detail page that
ships in M5.C. Pairs with [`docs/PRD.md`](../PRD.md) §5.7 (admin pages),
[`docs/STATUS_M5.md`](../STATUS_M5.md) D5–D9, and the wider M5.C scope
in [`docs/CHANGELOG.md`](../CHANGELOG.md) v2.6.

## Why this changed

The pre-M5.C admin pages used a list + per-row modal pattern. Operators
running daily ADT-build playtests asked for permalinks, a single place
to compose Discord broadcasts, and a stable participants table that
covers every distribution model. M5.C replaces the modal-stack with a
detail page split into four tabs.

## Anatomy of the detail page

Open the **Playtests** list (the existing landing route) and click
**View** on a row — the admin UI navigates to `/playtest/<slug>`. The
page is laid out as:

1. **Breadcrumb** — `Playtests` ➜ `<title>` (the breadcrumb link routes
   back to the list).
2. **Header**:
   - Title (large) + status pill (`Draft` / `Published` / `Closed`).
   - Date range (`Start → End`) read directly off the playtest row.
   - **Publish** (visible in `DRAFT` only) — fires
     `TransitionPlaytestStatus(OPEN)` under the confirm modal.
   - **Stop Playtest** (visible in `OPEN` only) — fires
     `TransitionPlaytestStatus(CLOSED)` with the documented danger
     confirm copy.
   - **Copy share link** — writes `<origin>/#/playtest/<slug>` to the
     clipboard for sharing with co-operators.
3. **Tabs** with `?tab=<key>` query-param persistence so reload preserves
   the selection:
   - **Playtest Info** (default) — read-only field grid + `Edit` button
     that opens the existing edit form modal.
   - **Distribution** — per-model rendering. ADT shows the linkage
     state + namespace/game/build identifiers; STEAM_KEYS shows the
     code-pool stats with an `Upload Codes (CSV)` CTA when empty;
     AGS_CAMPAIGN shows the campaign / item summary + the Sync action.
   - **Participants** — 6-column table backed by
     `GetPlaytestParticipants`. `Code Sent Date` is derived
     server-side from `applicant.last_dm_attempt_at` when
     `last_dm_status='sent'`. ADT applicants leave it blank in M5.C —
     the analogue (Download Date) lands with the M6 telemetry surface.
     Inline **Approve** / **Reject** buttons on `PENDING` rows hit the
     M2 RPCs unchanged.
   - **Discord Bot Tools** — bulk DM broadcast form (Send To filter +
     Subject + Message) paired with the announcement history table.
     Closed playtests disable the form with a banner.

## Verbs over PRD §5.1

The header **Publish** / **Stop Playtest** copy is a UI rename of
M4's existing transitions:

| Header verb     | RPC                                        | State change   |
| --------------- | ------------------------------------------ | -------------- |
| Publish         | `TransitionPlaytestStatus(OPEN)`           | `DRAFT → OPEN` |
| Stop Playtest   | `TransitionPlaytestStatus(CLOSED)`         | `OPEN → CLOSED`|

No new transitions ship in M5.C; the M4 window-driven auto-transition
worker continues to drive `startsAt` / `endsAt` boundary flips.

## Migration 0007 — what changed under the hood

[`migrations/0007_m5c_ux_revamp.up.sql`](../../migrations/0007_m5c_ux_revamp.up.sql)
lands:

- Four nullable applicant ADT telemetry cache columns
  (`adt_download_at`, `adt_total_playtime_seconds`, `adt_hardware_specs`,
  `adt_crash_count`). All ship dormant in M5.C — M6 fills them.
- `announcement` + `announcement_recipient` tables backing the bulk
  broadcast surface.
- A dormant `applicant_adt_telemetry_stale_idx` partial index for M6's
  telemetry refresh worker.

The scoping doc previously framed a breaking `platforms` column
replacement; the M1 schema already shipped `platforms TEXT[]` so 0007
is fully additive — no operator-visible breakage.

## Where to look when something's off

- Tab missing entirely → check `admin/src/PlaytestDetailPage.tsx` for
  the `Tabs` items list; the React router maps `?tab=<key>` to one of
  `info` / `distribution` / `participants` / `bot-tools`.
- Publish / Stop button missing → confirm the playtest status in the
  pill; the buttons are status-gated.
- Participants tab shows `Code Sent Date: —` for every applicant →
  none of them have `last_dm_status='sent'` yet (initial-approve DM is
  still pending or has failed; check the Audit log viewer).
- Announcement history shows `Sending` indefinitely → the inline
  fan-out path uses the existing Discord bot client; check
  `DISCORD_BOT_TOKEN` plus the bot's guild membership against the
  recipient (see [`discord-login.md`](discord-login.md) §"Mutual guild
  requirement").

## What this does NOT ship

- **No telemetry surface** — Hardware Specs / Crash Reports / Download
  Date / Total Playtime are all M6 deliverables. The proto wire shape
  carries the fields so admin clients ignore them silently in M5.C.
- **No per-applicant ADT URL** — the 2026-05-20 ADT spec is per-build
  only; revocation goes through `RejectApplicant`.
