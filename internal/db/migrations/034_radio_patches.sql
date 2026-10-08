-- One keyup can be granted on several talkgroups at once (a dispatcher patch
-- or multi-select): the same radio, the same moment, the same voice. Record
-- it once. Later copies point at the first call (patch_of) and are left out
-- of the subscriber feed; they get no recording or transcript of their own.

ALTER TABLE radio_calls
    ADD COLUMN IF NOT EXISTS patch_of BIGINT REFERENCES radio_calls(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_radio_calls_patch_of
    ON radio_calls (patch_of) WHERE patch_of IS NOT NULL;

-- Finding the twin: same system and radio, starts within two seconds.
CREATE INDEX IF NOT EXISTS idx_radio_calls_sys_source_started
    ON radio_calls (sysid, source_rid, started_at) WHERE source_rid IS NOT NULL;

-- Link calls recorded before this migration. Each copy points at the
-- earliest call in its two-second window.
UPDATE radio_calls c
   SET patch_of = (
       SELECT p.id FROM radio_calls p
        WHERE p.sysid = c.sysid AND p.source_rid = c.source_rid
          AND p.started_at BETWEEN c.started_at - INTERVAL '2 seconds' AND c.started_at + INTERVAL '2 seconds'
          AND (p.started_at, p.id) < (c.started_at, c.id)
        ORDER BY p.started_at, p.id
        LIMIT 1)
 WHERE c.patch_of IS NULL AND c.source_rid IS NOT NULL
   AND EXISTS (
       SELECT 1 FROM radio_calls p
        WHERE p.sysid = c.sysid AND p.source_rid = c.source_rid
          AND p.started_at BETWEEN c.started_at - INTERVAL '2 seconds' AND c.started_at + INTERVAL '2 seconds'
          AND (p.started_at, p.id) < (c.started_at, c.id));
