-- 0007_m5c_ux_revamp — M5.C schema additions per docs/PRD.md §5.4
-- "Bulk announcements" + docs/schema.md §"announcement + announcement_recipient
-- tables" + docs/STATUS_M5.md C2.
--
-- Adds:
--   applicant.adt_download_at, adt_total_playtime_seconds, adt_hardware_specs,
--     adt_crash_count    — M6-targeted ADT telemetry cache columns. They ship
--                          dormant in M5.C (always NULL / 0) so M6's worker +
--                          endpoint hookup lands with zero schema churn.
--
--   announcement              — admin-authored bulk DM broadcast per playtest
--                               (PRD §5.4 "Bulk announcements"). subject +
--                               message are PII-sensitive — see schema.md
--                               "PII guarantee".
--   announcement_recipient    — per-applicant fan-out row paired with a
--                               dm_outbox entry through the existing M2
--                               RetryDM machinery.
--
--   applicant_adt_telemetry_stale_idx  — partial index, dormant in M5.C;
--     turns on when M6's refresh worker queries APPROVED applicants with no
--     telemetry yet. Cheap (small partial), harmless until M6.
--
-- The platforms column shipped in 0001 as TEXT[] — no column drop / rename
-- needed despite earlier scoping prose; STATUS_M5.md C2 note reflects this.
--
-- Migrations are append-only (CLAUDE.md) — never edit 0001–0006; fix forward.

ALTER TABLE applicant
    ADD COLUMN adt_download_at            TIMESTAMPTZ,
    ADD COLUMN adt_total_playtime_seconds INTEGER,
    ADD COLUMN adt_hardware_specs         JSONB,
    ADD COLUMN adt_crash_count            INTEGER NOT NULL DEFAULT 0;

CREATE TABLE announcement (
    id                    UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    playtest_id           UUID        NOT NULL REFERENCES playtest(id),
    send_to_filter        TEXT        NOT NULL,
    subject               TEXT        NOT NULL,
    message               TEXT        NOT NULL,
    status                TEXT        NOT NULL DEFAULT 'SENDING',
    recipients_total      INTEGER     NOT NULL,
    recipients_sent       INTEGER     NOT NULL DEFAULT 0,
    created_by_user_id    UUID        NOT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT announcement_send_to_filter_enum
        CHECK (send_to_filter IN ('ALL', 'APPROVED_ONLY', 'PENDING_ONLY')),
    CONSTRAINT announcement_status_enum
        CHECK (status IN ('SENDING', 'SENT', 'PARTIAL', 'FAILED')),
    CONSTRAINT announcement_subject_len
        CHECK (length(subject) BETWEEN 1 AND 200),
    CONSTRAINT announcement_message_len
        CHECK (length(message) BETWEEN 1 AND 4000),
    CONSTRAINT announcement_recipients_total_non_negative
        CHECK (recipients_total >= 0),
    CONSTRAINT announcement_recipients_sent_bounded
        CHECK (recipients_sent BETWEEN 0 AND recipients_total)
);

CREATE INDEX announcement_playtest_created_idx
    ON announcement (playtest_id, created_at DESC);

CREATE TABLE announcement_recipient (
    announcement_id   UUID        NOT NULL REFERENCES announcement(id) ON DELETE CASCADE,
    applicant_id      UUID        NOT NULL REFERENCES applicant(id),
    dm_status         TEXT        NOT NULL DEFAULT 'QUEUED',
    dm_sent_at        TIMESTAMPTZ,
    dm_failed_at      TIMESTAMPTZ,
    dm_error_code     TEXT,
    PRIMARY KEY (announcement_id, applicant_id),
    CONSTRAINT announcement_recipient_dm_status_enum
        CHECK (dm_status IN ('QUEUED', 'SENT', 'FAILED'))
);

CREATE INDEX announcement_recipient_status_idx
    ON announcement_recipient (announcement_id, dm_status);

-- Dormant in M5.C. The predicate is the M6 refresh-worker query shape:
-- "APPROVED applicants with no telemetry yet for a given playtest". Cheap to
-- maintain at the M5.C cardinality (always empty until M6 starts writing).
CREATE INDEX applicant_adt_telemetry_stale_idx
    ON applicant (playtest_id, status)
    WHERE adt_download_at IS NULL AND status = 'APPROVED';
