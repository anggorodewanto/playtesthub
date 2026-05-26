ALTER TABLE playtest
    DROP CONSTRAINT IF EXISTS playtest_adt_build_status_enum,
    DROP COLUMN IF EXISTS adt_build_checked_at,
    DROP COLUMN IF EXISTS adt_build_status;
