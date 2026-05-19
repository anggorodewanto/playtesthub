-- Rollback for 0005_m5a_auto_approve. No IF EXISTS — a clean rollback
-- fails loudly on schema drift.

DROP INDEX applicant_auto_approved_count_idx;

ALTER TABLE applicant
    DROP COLUMN auto_approved;

ALTER TABLE playtest
    DROP CONSTRAINT playtest_auto_approve_limit_bounds,
    DROP COLUMN    auto_approve_limit,
    DROP COLUMN    auto_approve;
