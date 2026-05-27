-- Tracks live websocket connections from receiver agents. One row per system
-- (only one receiver per system at a time wins; the latest upsert evicts the
-- previous pod). Used by the admin portal for online/offline status and
-- hardware visibility.

CREATE TABLE IF NOT EXISTS system_connections (
    system_id           bigint      PRIMARY KEY REFERENCES systems(id) ON DELETE CASCADE,
    pod_id              text        NOT NULL,
    connected_at        timestamptz NOT NULL DEFAULT NOW(),
    last_ping_at        timestamptz NOT NULL DEFAULT NOW(),
    receiver_version    text,
    receiver_os         text,
    receiver_arch       text,
    receiver_hostname   text,
    dongles             jsonb
);

CREATE INDEX IF NOT EXISTS idx_system_connections_last_ping_at
    ON system_connections (last_ping_at);

-- When a system row changes (admin edits config etc), notify all pods so the
-- pod holding that system's websocket can push the new config to its receiver.
CREATE OR REPLACE FUNCTION notify_receiver_config() RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('receiver_config', NEW.id::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_notify_receiver_config ON systems;
CREATE TRIGGER trg_notify_receiver_config
    AFTER UPDATE ON systems
    FOR EACH ROW EXECUTE FUNCTION notify_receiver_config();
