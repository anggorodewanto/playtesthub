-- Rollback for 0003_m3. No IF EXISTS — a clean rollback fails loudly
-- on schema drift. survey_response references survey, so it drops first.

DROP TABLE survey_response;
DROP TABLE survey;
