-- Receiver remote management: server-held tuning config, periodic health
-- stats, a typed command queue and on-demand captures. Design and rollout in
-- docs/plans/2026-10-13-receiver-remote-management.md.
--
-- Everything here is idempotent: migrations run on every boot and are only
-- recorded in schema_migrations after they succeed, so a partial first run
-- must be re-runnable.

-- One row per revision of a system's RuntimeConfig (internal/rxproto). The
-- newest version is the config the receiver should be running; the server is
-- authoritative and the receiver reports back what it managed to apply.
CREATE TABLE IF NOT EXISTS receiver_config_revisions (
    id          BIGSERIAL    PRIMARY KEY,
    system_id   BIGINT       NOT NULL REFERENCES systems(id) ON DELETE CASCADE,
    version     INT          NOT NULL,
    config      JSONB        NOT NULL,
    -- Who produced it: "admin:<email>" or "seeded-from-receiver".
    source      TEXT         NOT NULL,
    note        TEXT,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (system_id, version)
);

-- Same channel as trg_notify_receiver_config on systems (kept): the pod
-- holding the receiver's socket pushes the newest revision down.
CREATE OR REPLACE FUNCTION notify_receiver_config_revision() RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('receiver_config', NEW.system_id::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_notify_receiver_config_revision ON receiver_config_revisions;
CREATE TRIGGER trg_notify_receiver_config_revision
    AFTER INSERT ON receiver_config_revisions
    FOR EACH ROW EXECUTE FUNCTION notify_receiver_config_revision();

-- Periodic health reports (rxproto.Stats), one row per report. Sampled on
-- read with date_bin, pruned after 30 days by the hub sweeper.
CREATE TABLE IF NOT EXISTS system_stats (
    system_id   BIGINT       NOT NULL REFERENCES systems(id) ON DELETE CASCADE,
    ts          TIMESTAMPTZ  NOT NULL,
    stats       JSONB        NOT NULL,
    PRIMARY KEY (system_id, ts)
);

-- The retention DELETE predicates on ts alone; the primary key leads with
-- system_id so it cannot serve that scan.
CREATE INDEX IF NOT EXISTS idx_system_stats_ts
    ON system_stats (ts);

-- Typed commands for a receiver: restart, dongle_reset, update, capture.
-- status: queued | sent | progress | ok | error | expired.
CREATE TABLE IF NOT EXISTS system_commands (
    id            UUID         PRIMARY KEY,
    system_id     BIGINT       NOT NULL REFERENCES systems(id) ON DELETE CASCADE,
    type          TEXT         NOT NULL,
    params        JSONB        NOT NULL DEFAULT '{}'::jsonb,
    requested_by  TEXT         NOT NULL,
    status        TEXT         NOT NULL DEFAULT 'queued',
    stage         TEXT,
    message       TEXT,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_system_commands_system_id_created_at
    ON system_commands (system_id, created_at DESC);

-- Wake every pod; the one holding a live proto-2 socket for the command's
-- system sends it and marks the row sent. Delivered at commit, so a capture
-- row inserted in the same transaction is visible by the time a pod reacts.
CREATE OR REPLACE FUNCTION notify_receiver_command() RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('receiver_command', NEW.id::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_notify_receiver_command ON system_commands;
CREATE TRIGGER trg_notify_receiver_command
    AFTER INSERT ON system_commands
    FOR EACH ROW EXECUTE FUNCTION notify_receiver_command();

-- On-demand spectrum / audio / IQ captures. The receiver uploads the file
-- with PUT /api/receiver/captures/{id}; the server stores it under
-- CAPTURE_DIR/{system_id}/{id}.{json|bin}. status: requested | recording |
-- ready | error.
CREATE TABLE IF NOT EXISTS system_captures (
    id            UUID         PRIMARY KEY,
    system_id     BIGINT       NOT NULL REFERENCES systems(id) ON DELETE CASCADE,
    command_id    UUID         REFERENCES system_commands(id) ON DELETE SET NULL,
    kind          TEXT         NOT NULL,
    channel       TEXT,
    seconds       INT,
    sample_rate   INT,
    status        TEXT         NOT NULL DEFAULT 'requested',
    path          TEXT,
    size_bytes    BIGINT,
    error         TEXT,
    requested_by  TEXT         NOT NULL,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    ready_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_system_captures_system_id_created_at
    ON system_captures (system_id, created_at DESC);

-- What the connected receiver told us in hello and reported since: protocol
-- version (1 for 5.2.x/5.3.x receivers), the config revision it is running
-- and whether it applied or rejected it, host facts, and the latest stats
-- report for the admin overview tiles.
ALTER TABLE system_connections
    ADD COLUMN IF NOT EXISTS proto          INT         NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS config_version INT,
    ADD COLUMN IF NOT EXISTS config_status  TEXT,
    ADD COLUMN IF NOT EXISTS host           JSONB,
    ADD COLUMN IF NOT EXISTS last_stats     JSONB,
    ADD COLUMN IF NOT EXISTS last_stats_at  TIMESTAMPTZ;
