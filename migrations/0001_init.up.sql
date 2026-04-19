-- 0001_init — M1 schema: playtest, code, applicant, leader_lease, audit_log.
-- Shape reference: docs/schema.md. Behavior reference: docs/PRD.md §5.1, §5.2.
-- Migrations are append-only (CLAUDE.md) — never edit this file; fix forward.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Playtest ---------------------------------------------------------------

CREATE TABLE playtest (
    id                          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    namespace                   TEXT        NOT NULL,
    slug                        TEXT        NOT NULL,
    title                       TEXT        NOT NULL,
    description                 TEXT        NOT NULL DEFAULT '',
    banner_image_url            TEXT        NOT NULL DEFAULT '',
    platforms                   TEXT[]      NOT NULL DEFAULT '{}',
    starts_at                   TIMESTAMPTZ,
    ends_at                     TIMESTAMPTZ,
    status                      TEXT        NOT NULL DEFAULT 'DRAFT',
    nda_required                BOOLEAN     NOT NULL DEFAULT FALSE,
    nda_text                    TEXT        NOT NULL DEFAULT '',
    current_nda_version_hash    TEXT        NOT NULL DEFAULT '',
    survey_id                   UUID,
    distribution_model          TEXT        NOT NULL,
    ags_item_id                 TEXT,
    ags_campaign_id             TEXT,
    initial_code_quantity       INTEGER,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at                  TIMESTAMPTZ,

    -- PRD §5.1 L144: slug regex enforced server-side; DB carries the same
    -- constraint so malformed data cannot exist even via direct SQL.
    CONSTRAINT playtest_slug_format
        CHECK (slug ~ '^[a-z0-9][a-z0-9-]{2,63}$'),
    CONSTRAINT playtest_title_len       CHECK (char_length(title) <= 200),
    CONSTRAINT playtest_description_len CHECK (char_length(description) <= 10000),
    CONSTRAINT playtest_banner_len      CHECK (char_length(banner_image_url) <= 2048),
    CONSTRAINT playtest_status_enum
        CHECK (status IN ('DRAFT', 'OPEN', 'CLOSED')),
    CONSTRAINT playtest_distribution_model_enum
        CHECK (distribution_model IN ('STEAM_KEYS', 'AGS_CAMPAIGN')),
    -- PRD §4.6 / §5.1: initial_code_quantity is required (1–50000) for
    -- AGS_CAMPAIGN and must be NULL for STEAM_KEYS.
    CONSTRAINT playtest_initial_code_quantity_model
        CHECK (
            (distribution_model = 'STEAM_KEYS'   AND initial_code_quantity IS NULL) OR
            (distribution_model = 'AGS_CAMPAIGN' AND initial_code_quantity BETWEEN 1 AND 50000)
        )
);

-- PRD §5.1 L144: slug uniqueness spans live AND soft-deleted rows so a
-- soft-deleted slug cannot be reused.
CREATE UNIQUE INDEX playtest_namespace_slug_uniq ON playtest (namespace, slug);

-- Code -------------------------------------------------------------------

CREATE TABLE code (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    playtest_id  UUID        NOT NULL REFERENCES playtest(id),
    value        TEXT        NOT NULL,
    state        TEXT        NOT NULL DEFAULT 'UNUSED',
    reserved_by  UUID,
    reserved_at  TIMESTAMPTZ,
    granted_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT code_state_enum
        CHECK (state IN ('UNUSED', 'RESERVED', 'GRANTED')),
    -- schema.md §"Code table" L125: explicit per-playtest uniqueness.
    CONSTRAINT code_playtest_value_uniq UNIQUE (playtest_id, value)
);

-- Serves the pool-stats + reserve-by-state path.
CREATE INDEX code_playtest_state_idx ON code (playtest_id, state);

-- Applicant --------------------------------------------------------------

CREATE TABLE applicant (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    playtest_id         UUID        NOT NULL REFERENCES playtest(id),
    user_id             UUID        NOT NULL,
    discord_handle      TEXT        NOT NULL,
    platforms           TEXT[]      NOT NULL DEFAULT '{}',
    nda_version_hash    TEXT,
    status              TEXT        NOT NULL DEFAULT 'PENDING',
    granted_code_id     UUID        REFERENCES code(id),
    approved_at         TIMESTAMPTZ,
    rejection_reason    TEXT,
    last_dm_status      TEXT,
    last_dm_attempt_at  TIMESTAMPTZ,
    last_dm_error       TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT applicant_status_enum
        CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED')),
    CONSTRAINT applicant_last_dm_status_enum
        CHECK (last_dm_status IS NULL OR last_dm_status IN ('sent', 'failed')),
    CONSTRAINT applicant_rejection_reason_len
        CHECK (rejection_reason IS NULL OR char_length(rejection_reason) <= 500),
    CONSTRAINT applicant_last_dm_error_len
        CHECK (last_dm_error IS NULL OR char_length(last_dm_error) <= 500),
    -- PRD §5.2 L186: signup idempotency natural key.
    CONSTRAINT applicant_playtest_user_uniq UNIQUE (playtest_id, user_id)
);

-- Applicant queue listing (PRD §5.4): filter by status, order by created_at.
CREATE INDEX applicant_queue_idx ON applicant (playtest_id, status, created_at DESC);

-- leader_lease -----------------------------------------------------------
-- Used by the reclaim-job leader election (PRD §5.5; reclaim lands in M2).

CREATE TABLE leader_lease (
    name         TEXT        PRIMARY KEY,
    holder       TEXT        NOT NULL,
    acquired_at  TIMESTAMPTZ NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL
);

-- AuditLog ---------------------------------------------------------------

CREATE TABLE audit_log (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    namespace       TEXT        NOT NULL,
    playtest_id     UUID,
    actor_user_id   UUID,
    action          TEXT        NOT NULL,
    before          JSONB       NOT NULL DEFAULT '{}'::jsonb,
    after           JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Powers the per-playtest audit viewer (PRD §5.7 page 5) with cursor
-- pagination on (created_at, id) — schema.md §"AuditLog table" L32–34.
CREATE INDEX audit_log_playtest_created_at_idx
    ON audit_log (playtest_id, created_at DESC);

CREATE INDEX audit_log_actor_created_at_idx
    ON audit_log (actor_user_id, created_at DESC);
