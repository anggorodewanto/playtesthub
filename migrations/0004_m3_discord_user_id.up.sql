-- 0004_m3_discord_user_id — add applicant.discord_user_id (the Discord
-- snowflake) so the DM worker can deliver to a user-id endpoint instead
-- of the human display name in `discord_handle`. Per STATUS M3 phase 7:
-- DM delivery to discord_handle (display name) fails; the snowflake is
-- the only routable identifier.
--
-- Nullable + no backfill: rows persisted before this migration land as
-- NULL. The DM queue treats NULL as `lastDmError='missing_recipient'`
-- (errors.md) without invoking the Discord client.
--
-- Migrations are append-only (CLAUDE.md) — never edit 0001/0002/0003;
-- fix forward.

ALTER TABLE applicant
    ADD COLUMN discord_user_id TEXT;
