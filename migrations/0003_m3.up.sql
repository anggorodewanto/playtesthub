-- 0003_m3 — M3 schema: survey + survey_response per docs/schema.md
-- §"Survey entity spec" and PRD §5.6.
--
-- A survey row is per-version: EditSurvey writes a new survey row
-- (version = previous + 1) and Playtest.survey_id is repointed to it.
-- SurveyResponse.survey_id is the per-version FK — there is no
-- separate version column on SurveyResponse, version is recovered by
-- joining (schema.md L163).
--
-- Migrations are append-only (CLAUDE.md) — never edit 0001/0002; fix
-- forward.

-- survey -----------------------------------------------------------------
-- Per-version row. Question UUIDs are preserved across version bumps
-- (schema.md L156) — that's a service-layer responsibility; the DB
-- carries `questions` as opaque JSONB.

CREATE TABLE survey (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    playtest_id  UUID        NOT NULL REFERENCES playtest(id),
    version      INTEGER     NOT NULL,
    questions    JSONB       NOT NULL DEFAULT '[]'::jsonb,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- schema.md §"Survey entity spec" L152: version starts at 1 and
    -- monotonically advances per playtest. Uniqueness pins the
    -- "previous + 1" rule at the DB level so a buggy concurrent edit
    -- cannot create two v2 rows on the same playtest.
    CONSTRAINT survey_playtest_version_uniq UNIQUE (playtest_id, version),
    CONSTRAINT survey_version_positive CHECK (version >= 1)
);

-- GetCurrent path: latest version per playtest.
CREATE INDEX survey_playtest_version_idx ON survey (playtest_id, version DESC);

-- survey_response --------------------------------------------------------
-- One submission per (playtest_id, user_id) regardless of version
-- (schema.md L164). One-shot immutable per PRD §5.6 — no UPDATE/DELETE
-- surface.

CREATE TABLE survey_response (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    playtest_id   UUID        NOT NULL REFERENCES playtest(id),
    user_id       UUID        NOT NULL,
    survey_id     UUID        NOT NULL REFERENCES survey(id),
    answers       JSONB       NOT NULL DEFAULT '{}'::jsonb,
    submitted_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- PRD §5.6 / schema.md L164: one submission per player per
    -- playtest. Spans every survey version — a player who answered v1
    -- cannot resubmit against v2.
    CONSTRAINT survey_response_playtest_user_uniq UNIQUE (playtest_id, user_id)
);

-- ListResponses cursor pagination on (submitted_at, id) DESC,
-- optionally filtered by survey_id for the per-version aggregate split
-- (PRD §5.6 / §5.7 page 3).
CREATE INDEX survey_response_playtest_submitted_idx
    ON survey_response (playtest_id, submitted_at DESC, id DESC);

CREATE INDEX survey_response_survey_id_idx
    ON survey_response (survey_id);
