-- Rollback for 0007_m5c_ux_revamp. No IF EXISTS — clean rollback fails loudly
-- on drift.

DROP INDEX applicant_adt_telemetry_stale_idx;

DROP INDEX announcement_recipient_status_idx;
DROP TABLE announcement_recipient;

DROP INDEX announcement_playtest_created_idx;
DROP TABLE announcement;

ALTER TABLE applicant
    DROP COLUMN adt_crash_count,
    DROP COLUMN adt_hardware_specs,
    DROP COLUMN adt_total_playtime_seconds,
    DROP COLUMN adt_download_at;
