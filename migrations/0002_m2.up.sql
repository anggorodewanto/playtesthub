-- 0002_m2 — M2 schema: nda_acceptance table (PRD §5.3, schema.md L192).
-- Audit action set extends with the M2 actions enumerated in
-- docs/schema.md §"AuditLog — `action` enum"; audit_log.action is a free
-- TEXT column (no CHECK constraint) so the enum extension is purely a
-- code/doc contract — see migration 0002 schema test for the round-trip
-- assertions that pin it down.
-- Migrations are append-only (CLAUDE.md) — never edit 0001; fix forward.

-- nda_acceptance ---------------------------------------------------------
-- Append-only ledger of click-accepts. Composite PK = natural key:
-- a second accept on the same (user_id, playtest_id, nda_version_hash)
-- is the idempotency case (PRD §5.3, §4.7).

CREATE TABLE nda_acceptance (
    user_id           UUID        NOT NULL,
    playtest_id       UUID        NOT NULL REFERENCES playtest(id),
    nda_version_hash  TEXT        NOT NULL,
    accepted_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT nda_acceptance_pk
        PRIMARY KEY (user_id, playtest_id, nda_version_hash)
);
