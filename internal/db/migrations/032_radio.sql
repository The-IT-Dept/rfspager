-- P25 radio monitoring (NSW Public Safety Network). A P25 receiver decodes a
-- site's trunking control channel and sends site, call and unit frames over
-- the receiver websocket (internal/recvhub); internal/radio writes them here.
--
-- Identity follows the network, not our receivers: a site is
-- (sysid, rfss, site), a talkgroup (sysid, tgid) and a radio (sysid, rid).
-- Rows are created the first time anything is heard and named later by an
-- admin, so every descriptive column is nullable.
--
-- receiver_system_id records which receiver heard a row. It is SET NULL on
-- delete so history outlives a receiver being removed.

CREATE TABLE IF NOT EXISTS radio_sites (
    id                      BIGSERIAL        PRIMARY KEY,
    sysid                   INT              NOT NULL,
    rfss                    INT              NOT NULL,
    site                    INT              NOT NULL,
    wacn                    INT              NOT NULL DEFAULT 0,
    nac                     INT              NOT NULL DEFAULT 0,
    name                    TEXT,
    control_hz              BIGINT           NOT NULL DEFAULT 0,
    -- As advertised by the site; see p25.SitePayload.
    neighbors               JSONB            NOT NULL DEFAULT '[]'::jsonb,
    plan                    JSONB            NOT NULL DEFAULT '[]'::jsonb,
    lat                     DOUBLE PRECISION,
    lng                     DOUBLE PRECISION,
    first_seen_at           TIMESTAMPTZ      NOT NULL DEFAULT NOW(),
    last_seen_at            TIMESTAMPTZ      NOT NULL DEFAULT NOW(),
    last_receiver_system_id BIGINT           REFERENCES systems(id) ON DELETE SET NULL,
    UNIQUE (sysid, rfss, site)
);

CREATE TABLE IF NOT EXISTS radio_talkgroups (
    id              BIGSERIAL    PRIMARY KEY,
    sysid           INT          NOT NULL,
    tgid            INT          NOT NULL,
    alpha_tag       TEXT,
    description     TEXT,
    agency          TEXT,
    category        TEXT,
    -- Hidden talkgroups are left out of everything subscribers see.
    hidden          BOOLEAN      NOT NULL DEFAULT false,
    -- NULL until heard: talkgroups can be imported by name before any traffic.
    first_seen_at   TIMESTAMPTZ,
    last_seen_at    TIMESTAMPTZ,
    -- Completed calls, counted once when a call ends (radio.Store.EndCall).
    calls_clear     INT          NOT NULL DEFAULT 0,
    calls_encrypted INT          NOT NULL DEFAULT 0,
    UNIQUE (sysid, tgid)
);

CREATE TABLE IF NOT EXISTS radio_units (
    id            BIGSERIAL    PRIMARY KEY,
    sysid         INT          NOT NULL,
    rid           INT          NOT NULL,
    alias         TEXT,
    agency        TEXT,
    first_seen_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    last_seen_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    -- Talkgroup of the unit's most recent accepted affiliation.
    last_tgid     INT,
    UNIQUE (sysid, rid)
);

-- One row per voice grant. call_uid is the receiver's call_id, which ties a
-- call_start to its call_end; tgid is NULL for unit-to-unit calls, which use
-- target_rid instead. Times are the receiver's clock.
CREATE TABLE IF NOT EXISTS radio_calls (
    id                 BIGSERIAL    PRIMARY KEY,
    receiver_system_id BIGINT       REFERENCES systems(id) ON DELETE SET NULL,
    call_uid           TEXT         NOT NULL UNIQUE,
    sysid              INT          NOT NULL,
    rfss               INT          NOT NULL,
    site               INT          NOT NULL,
    tgid               INT,
    target_rid         INT,
    source_rid         INT,
    freq_hz            BIGINT,
    slot               SMALLINT     NOT NULL DEFAULT 0,
    encrypted          BOOLEAN      NOT NULL DEFAULT false,
    emergency          BOOLEAN      NOT NULL DEFAULT false,
    patch              BOOLEAN      NOT NULL DEFAULT false,
    started_at         TIMESTAMPTZ  NOT NULL,
    ended_at           TIMESTAMPTZ,
    -- Filled in by the voice stage, which does not exist yet.
    audio_url          TEXT,
    transcript         TEXT,
    transcribed_at     TIMESTAMPTZ
);

-- The feed pages on (started_at, id), so id is in each index as the tiebreak.
CREATE INDEX IF NOT EXISTS idx_radio_calls_started_at
    ON radio_calls (started_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_radio_calls_talkgroup
    ON radio_calls (sysid, tgid, started_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_radio_calls_source
    ON radio_calls (source_rid, started_at DESC);

-- Affiliations, registrations and deregistrations, as heard.
CREATE TABLE IF NOT EXISTS radio_unit_events (
    id                 BIGSERIAL    PRIMARY KEY,
    receiver_system_id BIGINT       REFERENCES systems(id) ON DELETE SET NULL,
    sysid              INT          NOT NULL,
    rfss               INT          NOT NULL,
    site               INT          NOT NULL,
    kind               TEXT         NOT NULL,
    rid                INT          NOT NULL,
    tgid               INT,
    result             SMALLINT     NOT NULL DEFAULT 0, -- 0 = accepted
    at                 TIMESTAMPTZ  NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_radio_unit_events_rid_at
    ON radio_unit_events (rid, at DESC);
CREATE INDEX IF NOT EXISTS idx_radio_unit_events_at
    ON radio_unit_events (at DESC);

-- Wakes the radio SSE broker when a call starts or ends. Payload is the call
-- id. "UPDATE OF ended_at" fires whenever ended_at is in the SET list, so the
-- function also checks the value actually changed.
CREATE OR REPLACE FUNCTION notify_radio_call() RETURNS TRIGGER AS $$
BEGIN
    IF TG_OP = 'INSERT' OR OLD.ended_at IS DISTINCT FROM NEW.ended_at THEN
        PERFORM pg_notify('radio_call', NEW.id::text);
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_notify_radio_call ON radio_calls;
CREATE TRIGGER trg_notify_radio_call
    AFTER INSERT OR UPDATE OF ended_at ON radio_calls
    FOR EACH ROW EXECUTE FUNCTION notify_radio_call();
