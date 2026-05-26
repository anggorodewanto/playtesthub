-- M5.C ADT build-health surfacing — make a gone/undownloadable ADT build
-- visible on the playtest detail page instead of only at ApproveApplicant
-- time. Two nullable columns record the last observed health of the
-- playtest's ADT build:
--   adt_build_status     — NULL (never checked) | 'OK' | 'UNAVAILABLE'.
--                          'UNAVAILABLE' = ADT returned build-not-found on
--                          the downloadUrls issue call (the same signal that
--                          fails ApproveApplicant); 'OK' = a URL was minted.
--   adt_build_checked_at — when the status above was last observed.
-- Written opportunistically by ApproveApplicant / RetryDM (resolveADTDownloadURL)
-- and on demand by CheckADTBuild. Non-ADT playtests leave both NULL.
ALTER TABLE playtest
    ADD COLUMN adt_build_status     TEXT,
    ADD COLUMN adt_build_checked_at TIMESTAMPTZ,
    ADD CONSTRAINT playtest_adt_build_status_enum
        CHECK (adt_build_status IS NULL OR adt_build_status IN ('OK', 'UNAVAILABLE'));
