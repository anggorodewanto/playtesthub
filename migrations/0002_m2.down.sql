-- Rollback for 0002_m2. No IF EXISTS — a clean rollback fails loudly
-- on schema drift.

DROP TABLE nda_acceptance;
