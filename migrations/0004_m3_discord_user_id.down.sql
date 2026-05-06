-- Rollback for 0004_m3_discord_user_id. No IF EXISTS — a clean
-- rollback fails loudly on schema drift.

ALTER TABLE applicant
    DROP COLUMN discord_user_id;
