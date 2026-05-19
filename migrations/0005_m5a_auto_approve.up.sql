-- 0005_m5a_auto_approve — M5.A schema: auto-approve fields per
-- docs/PRD.md §5.1 / §5.4 "Auto-approve" and docs/STATUS_M5.md A2.
--
-- Adds:
--   playtest.auto_approve         — opt-in toggle (default false).
--   playtest.auto_approve_limit   — required iff auto_approve=true, 1..100,000.
--   applicant.auto_approved       — true iff the signup-time auto-approve
--                                   path admitted this applicant. Drives the
--                                   cap-count predicate in PRD §5.4.
--
-- The CHECK on (auto_approve, auto_approve_limit) enforces the
-- errors.md row "auto_approve_limit must be between 1 and 100000 when
-- auto_approve is true" at the DB level so it cannot be bypassed via
-- direct SQL.
--
-- Migrations are append-only (CLAUDE.md) — never edit 0001–0004;
-- fix forward.

ALTER TABLE playtest
    ADD COLUMN auto_approve       BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN auto_approve_limit INTEGER,
    -- NB: CHECK in Postgres passes on NULL (three-valued logic), so the
    -- predicate names IS NULL / IS NOT NULL explicitly. Without that,
    -- `auto_approve=true AND auto_approve_limit=NULL` evaluates to NULL
    -- and slips past the constraint.
    ADD CONSTRAINT playtest_auto_approve_limit_bounds
        CHECK (
            (auto_approve = false AND auto_approve_limit IS NULL) OR
            (auto_approve = true  AND auto_approve_limit IS NOT NULL
                                  AND auto_approve_limit BETWEEN 1 AND 100000)
        );

ALTER TABLE applicant
    ADD COLUMN auto_approved BOOLEAN NOT NULL DEFAULT FALSE;

-- Partial index keeps the cap-count predicate index-only:
--   SELECT count(*) FROM applicant
--    WHERE playtest_id = $1 AND auto_approved = true
-- (PRD §5.4 "Auto-approve" — cap semantics).
CREATE INDEX applicant_auto_approved_count_idx
    ON applicant (playtest_id)
    WHERE auto_approved = true;
