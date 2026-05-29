-- Tracks every notable state change for a receiver: WS connect / disconnect,
-- hardware changes (dongle plugged/unplugged), software changes (version
-- upgrade, restart, crash), and admin-driven lifecycle (approval, ownership).
--
-- Public clients see a redacted projection (just transition events + uptime
-- %). Owner + admin see everything including metadata.

CREATE TABLE IF NOT EXISTS system_events (
    id          BIGSERIAL    PRIMARY KEY,
    system_id   BIGINT       NOT NULL REFERENCES systems(id) ON DELETE CASCADE,
    type        TEXT         NOT NULL,
    severity    TEXT         NOT NULL DEFAULT 'info', -- info | warning | error
    -- Short human-readable summary. The mobile + web timelines use this as
    -- the row label; admin sees both this and the full metadata blob.
    message     TEXT         NOT NULL,
    metadata    JSONB        NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_system_events_system_id_created_at
    ON system_events (system_id, created_at DESC);

-- The watchdog uses this partial index to find systems that have gone offline
-- but for which we haven't yet emitted a `receiver_offline` event. Saves a
-- full scan every minute. See internal/recvhub/watchdog.go.
CREATE INDEX IF NOT EXISTS idx_system_events_type_recent
    ON system_events (system_id, type, created_at DESC);


-- Receiver log lines shipped over the WS. Capped at 7-day retention by the
-- hub's daily rotation job (see internal/recvhub/log_rotate.go). Owner and
-- admin can stream live; everyone else just sees uptime, not log content.

CREATE TABLE IF NOT EXISTS system_logs (
    id          BIGSERIAL    PRIMARY KEY,
    system_id   BIGINT       NOT NULL REFERENCES systems(id) ON DELETE CASCADE,
    -- timestamp the receiver assigned to the log line (its own clock).
    -- Stored separately from received_at so we can show real receiver-clock
    -- timestamps even if the WS buffered for a few seconds.
    ts          TIMESTAMPTZ  NOT NULL,
    received_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    level       TEXT         NOT NULL DEFAULT 'info', -- debug | info | warn | error
    source      TEXT,                                   -- e.g. "pocsag", "sdr", "wsclient"
    message     TEXT         NOT NULL,
    -- Anything structured the receiver wants to ship alongside the message.
    raw         JSONB        NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_system_logs_system_id_ts
    ON system_logs (system_id, ts DESC);

-- Bounds the daily DELETE so it doesn't scan the whole table to find the
-- old rows. Predicates exactly what the rotation job needs.
CREATE INDEX IF NOT EXISTS idx_system_logs_received_at
    ON system_logs (received_at);
