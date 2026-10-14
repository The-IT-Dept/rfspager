-- Coverage map: where receivers are, which paging transmitters they hear,
-- and which pages every receiver heard. Design in
-- docs/plans/2026-10-14-coverage-map.md.
--
-- Everything here is idempotent: migrations run on every boot and are only
-- recorded in schema_migrations after they succeed, so a partial first run
-- must be re-runnable.

-- Receptions before located_since are not attributed to the receiver's
-- current location (system 3 was the Narrabeen PagerMon feed before it
-- became Newcastle). NULL = always been here.
ALTER TABLE systems
    ADD COLUMN IF NOT EXISTS located_since TIMESTAMPTZ NULL;

-- Only RFS district agencies label transmitters and appear in the district
-- table. The known non-district agencies are switched off when the column is
-- first added; after that it is admin-owned and a re-run must not undo edits.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'agencies' AND column_name = 'coverage_district'
    ) THEN
        ALTER TABLE agencies ADD COLUMN coverage_district BOOLEAN NOT NULL DEFAULT true;
        UPDATE agencies SET coverage_district = false
         WHERE upper(btrim(agency)) IN (
            'FRNSW', 'SES', 'VOLUNTEER RESCUE ASSOCIATION', 'MARINE RESCUE',
            'TEST', 'RFSPAGER.COM', 'RUBISH', 'NSWRFS', 'RFS');
    END IF;
END $$;

-- One row per licensed paging transmitter device from the ACMA Register of
-- Radiocommunications Licences (internal/importer/acma.go). agency_id is the
-- RFS district the transmitter serves: inferred nightly when district_source
-- is 'auto' (internal/coverage), set by an admin when 'manual'.
CREATE TABLE IF NOT EXISTS paging_transmitters (
    id                   BIGSERIAL        PRIMARY KEY,
    sdd_id               BIGINT           NOT NULL UNIQUE,
    channel              TEXT             NOT NULL CHECK (channel IN ('RFS', 'FRNSW')),
    freq_hz              BIGINT           NOT NULL,
    licence_no           TEXT             NOT NULL,
    licensee             TEXT             NOT NULL,
    acma_site_id         TEXT             NOT NULL,
    name                 TEXT             NOT NULL,
    lat                  DOUBLE PRECISION NOT NULL,
    lng                  DOUBLE PRECISION NOT NULL,
    elevation_m          REAL,
    height_m             REAL,
    eirp_w               REAL,
    authorised_on        DATE,
    agency_id            BIGINT           REFERENCES agencies(id) ON DELETE SET NULL,
    district_source      TEXT             NOT NULL DEFAULT 'auto' CHECK (district_source IN ('auto', 'manual')),
    district_confidence  REAL,
    imported_at          TIMESTAMPTZ      NOT NULL DEFAULT NOW(),
    created_at           TIMESTAMPTZ      NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ      NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_paging_transmitters_agency_id
    ON paging_transmitters (agency_id);

-- One row per receiver per posted page, recorded at ingest before dedupe so
-- every receiver that heard a page is counted, not just the one whose copy
-- was stored. District and channel are resolved at query time from the
-- address, so capcode edits apply retroactively. Pruned after 400 days.
-- source: live (ingest) | logs (backfilled from system_logs) | messages
-- (backfilled from the stored copy only).
CREATE TABLE IF NOT EXISTS page_receptions (
    id            BIGSERIAL        PRIMARY KEY,
    system_id     BIGINT           NOT NULL REFERENCES systems(id) ON DELETE CASCADE,
    heard_at      TIMESTAMPTZ      NOT NULL,
    address       TEXT             NOT NULL,
    capcode_id    BIGINT,
    lat           DOUBLE PRECISION,
    lng           DOUBLE PRECISION,
    incident_key  TEXT,
    damaged       BOOLEAN          NOT NULL DEFAULT false,
    source        TEXT             NOT NULL DEFAULT 'live' CHECK (source IN ('live', 'logs', 'messages')),
    UNIQUE (system_id, address, heard_at)
);

CREATE INDEX IF NOT EXISTS idx_page_receptions_heard_at
    ON page_receptions (heard_at);

CREATE INDEX IF NOT EXISTS idx_page_receptions_system_id_heard_at
    ON page_receptions (system_id, heard_at);

-- Backfill 90 days, once (only while the table is empty).
--
-- 1. Receiver log lines: every Go receiver ships each decoded page to
--    system_logs (7-day retention) with raw->>'posted', so these give every
--    receiver's copy, not just the deduped winner. ts is the receiver clock;
--    it is truncated to the second to line up with the ingest timestamp.
-- 2. messages for each system before its earliest such log line (all 90
--    days for systems that never shipped logs, e.g. PagerMon feeds): one row
--    per stored page, credited to the receiver whose copy won the dedupe.
--
-- Numeric-only pages (beacons like FRNSW's 0180111 clock tick) are skipped,
-- as at ingest (handler.isDispatchText).
--
-- Coordinates come from messages.lat/lng or the "[lng,lat]" tail of the body.
-- Rows written before migration 033 have no incident_key; for those the RFS
-- incident number or FRNSW INC number stands in (the coverage heat map only
-- needs a key shared by every copy of one incident).
DO $$
DECLARE
    since TIMESTAMPTZ := NOW() - INTERVAL '90 days';
BEGIN
    IF EXISTS (SELECT 1 FROM page_receptions) THEN
        RETURN;
    END IF;

    INSERT INTO page_receptions
        (system_id, heard_at, address, capcode_id, lat, lng, incident_key, damaged, source)
    SELECT l.system_id,
           date_trunc('second', l.ts),
           l.raw->>'address',
           cc.id,
           COALESCE((regexp_match(l.raw->>'msg', '\[(1[0-9]{2}\.[0-9]+),(-[0-9]{2}\.[0-9]+)\]'))[2]::float8, m.lat),
           COALESCE((regexp_match(l.raw->>'msg', '\[(1[0-9]{2}\.[0-9]+),(-[0-9]{2}\.[0-9]+)\]'))[1]::float8, m.lng),
           COALESCE(m.incident_key,
                    'frinc:' || (regexp_match(l.raw->>'msg', 'INC:\s*([0-9]+-[0-9]{8})'))[1],
                    'rfs:' || (regexp_match(l.raw->>'msg', '\m([0-9]{2}-[0-9]{6})\M'))[1]),
           COALESCE(l.raw->>'uncorrectable', '0') !~ '^0*$'
               OR COALESCE(l.raw->>'truncated', 'false') = 'true',
           'logs'
      FROM system_logs l
      LEFT JOIN LATERAL (
            SELECT c.id, c."ignore"
              FROM capcodes c
             WHERE c.address = l.raw->>'address' AND c.deleted_at IS NULL
             ORDER BY c.id
             LIMIT 1
      ) cc ON true
      LEFT JOIN LATERAL (
            -- The stored copy of the same page, for its incident key and
            -- coordinates when the logged text lost them. md5 is indexed.
            SELECT mm.incident_key,
                   CASE WHEN mm.lat ~ '^-?[0-9]+(\.[0-9]+)?$' AND mm.lng ~ '^-?[0-9]+(\.[0-9]+)?$'
                        THEN mm.lat::float8 END AS lat,
                   CASE WHEN mm.lat ~ '^-?[0-9]+(\.[0-9]+)?$' AND mm.lng ~ '^-?[0-9]+(\.[0-9]+)?$'
                        THEN mm.lng::float8 END AS lng
              FROM messages mm
             WHERE mm.md5 = md5(l.raw->>'msg')
               AND mm.address = l.raw->>'address'
               AND mm.deleted_at IS NULL
               AND mm.created_at BETWEEN l.ts - INTERVAL '10 minutes' AND l.ts + INTERVAL '10 minutes'
             LIMIT 1
      ) m ON true
     WHERE l.message = 'page'
       AND l.raw->>'posted' = 'true'
       AND COALESCE(l.raw->>'address', '') <> ''
       AND l.raw->>'msg' ~ '[A-Za-z]'
       AND l.ts >= since
       AND l.ts <= NOW() + INTERVAL '1 hour'
       AND (cc.id IS NULL OR NOT cc."ignore")
    ON CONFLICT (system_id, address, heard_at) DO NOTHING;

    INSERT INTO page_receptions
        (system_id, heard_at, address, capcode_id, lat, lng, incident_key, damaged, source)
    SELECT m.system_id,
           m.created_at,
           m.address,
           m.capcode_id,
           CASE WHEN m.lat ~ '^-?[0-9]+(\.[0-9]+)?$' AND m.lng ~ '^-?[0-9]+(\.[0-9]+)?$'
                THEN m.lat::float8
                ELSE (regexp_match(m.message, '\[(1[0-9]{2}\.[0-9]+),(-[0-9]{2}\.[0-9]+)\]'))[2]::float8 END,
           CASE WHEN m.lat ~ '^-?[0-9]+(\.[0-9]+)?$' AND m.lng ~ '^-?[0-9]+(\.[0-9]+)?$'
                THEN m.lng::float8
                ELSE (regexp_match(m.message, '\[(1[0-9]{2}\.[0-9]+),(-[0-9]{2}\.[0-9]+)\]'))[1]::float8 END,
           COALESCE(m.incident_key,
                    'frinc:' || (regexp_match(m.message, 'INC:\s*([0-9]+-[0-9]{8})'))[1],
                    'rfs:' || (regexp_match(m.message, '\m([0-9]{2}-[0-9]{6})\M'))[1]),
           COALESCE(m.quality->>'uncorrectable', '0') !~ '^0*$'
               OR COALESCE(m.quality->>'truncated', 'false') = 'true',
           'messages'
      FROM messages m
      LEFT JOIN (
            SELECT system_id, MIN(heard_at) AS first_logged
              FROM page_receptions
             WHERE source = 'logs'
             GROUP BY system_id
      ) f ON f.system_id = m.system_id
     WHERE m.deleted_at IS NULL
       AND m.message ~ '[A-Za-z]'
       AND m.created_at >= since
       AND m.created_at <= NOW() + INTERVAL '1 hour'
       AND (f.first_logged IS NULL OR m.created_at < f.first_logged)
    ON CONFLICT (system_id, address, heard_at) DO NOTHING;
END $$;
