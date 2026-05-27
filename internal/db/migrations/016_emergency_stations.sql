-- Emergency service facility locations (fire, police, ambulance, SES) sourced
-- primarily from Geoscience Australia's Emergency Management Facilities
-- Database (CC-BY 4.0, national coverage, 2018 bulk snapshot, refreshed
-- "as needed") and supplemented by Fire and Rescue NSW's live station GeoRSS
-- feed (~514 urban stations, refreshed daily by FRNSW).
--
-- One row per facility. `source` + `source_id` are unique to allow re-running
-- the importer idempotently. The combined `category` field distinguishes the
-- agency at query time so the public API can serve a single endpoint and the
-- frontend filters client-side.

CREATE TABLE IF NOT EXISTS emergency_stations (
    id          BIGSERIAL    PRIMARY KEY,
    source      TEXT         NOT NULL,
    source_id   TEXT         NOT NULL,
    category    TEXT         NOT NULL,
    name        TEXT         NOT NULL,
    operator    TEXT,
    address     TEXT,
    suburb      TEXT,
    postcode    TEXT,
    state       TEXT         NOT NULL DEFAULT 'NSW',
    status      TEXT,
    lat         NUMERIC(10,7) NOT NULL,
    lng         NUMERIC(10,7) NOT NULL,
    revised_at  DATE,
    raw         JSONB        NOT NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (source, source_id)
);

CREATE INDEX IF NOT EXISTS idx_emergency_stations_category
    ON emergency_stations (category);

CREATE INDEX IF NOT EXISTS idx_emergency_stations_state
    ON emergency_stations (state);

-- Approximate bbox filter — Postgres' B-tree on (lat, lng) is fine for the
-- ~2.8k NSW rows we're working with. If we ever grow to multi-state we can
-- swap in PostGIS.
CREATE INDEX IF NOT EXISTS idx_emergency_stations_lat_lng
    ON emergency_stations (lat, lng);
