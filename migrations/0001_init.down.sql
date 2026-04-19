-- Rollback for 0001_init. Drop in reverse FK order. No IF EXISTS — a
-- clean rollback fails loudly on schema drift.

DROP TABLE audit_log;
DROP TABLE leader_lease;
DROP TABLE applicant;
DROP TABLE code;
DROP TABLE playtest;
