-- Message quality and incident keys.
--
-- quality: the receiver's decode assessment for the stored copy of a page
-- (codewords, corrected, uncorrectable, truncated, damaged ranges). Ingest
-- keeps the best copy heard by any receiver and records how many worse
-- copies it replaced.
--
-- incident_key: identifies "the same dispatch" across the capcodes it was
-- sent to, so a user subscribed to several brigades or groups is pushed
-- once per incident message rather than once per capcode. Computed at
-- ingest (see internal/handler incident_key.go); NULL for rows written
-- before this migration, which simply fall outside the suppression.

ALTER TABLE messages
    ADD COLUMN IF NOT EXISTS quality        JSONB,
    ADD COLUMN IF NOT EXISTS incident_key   TEXT,
    ADD COLUMN IF NOT EXISTS replaced_count INT NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_messages_incident_key_created
    ON messages (incident_key, created_at DESC)
    WHERE incident_key IS NOT NULL;
