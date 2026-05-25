-- Register-interest flow for new systems.
--
-- Users can submit a request to host a paging system. The request lives in
-- `systems` from day one with `approved_at = NULL` and `api_key = NULL`. An
-- admin reviews the queue and, on approval, generates the api_key and stamps
-- `approved_at`. A push notification then fires to the requester's
-- installations.
--
-- Existing rows are pre-approved at their original creation time so the
-- public Systems list doesn't suddenly empty.

ALTER TABLE systems
    ADD COLUMN IF NOT EXISTS approved_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS suburb VARCHAR(255) NULL,
    ADD COLUMN IF NOT EXISTS request_notes TEXT NULL;

UPDATE systems
   SET approved_at = COALESCE(approved_at, created_at)
 WHERE approved_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_systems_pending
    ON systems (created_at)
    WHERE approved_at IS NULL AND deleted_at IS NULL;
