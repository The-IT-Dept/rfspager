-- Two-stage notification pipeline using the existing Laravel `notifications`
-- table as the substrate. Inbound message handler does nothing extra; a
-- BEFORE-IDLE worker fans recipients into `notifications`, a dispatch worker
-- claims batches via SKIP LOCKED and pushes via APN/FCM. Wakeup is via
-- LISTEN/NOTIFY, with a slow reconciliation tick as the safety net.
--
-- Schema additions are nullable / default-false so existing Laravel writes
-- and reads continue to work unchanged.

ALTER TABLE notifications
    ADD COLUMN IF NOT EXISTS message_id        BIGINT REFERENCES messages(id),
    ADD COLUMN IF NOT EXISTS push_required     BOOLEAN     NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS push_attempts     INT         NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS push_locked_until TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS pushed_at         TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS push_failed_at    TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS push_last_error   TEXT;

-- Partial index makes the dispatch claim O(batch_size) regardless of table
-- size — only pending push rows are indexed.
CREATE INDEX IF NOT EXISTS idx_notifications_dispatch_pending
    ON notifications (created_at)
    WHERE push_required
      AND pushed_at IS NULL
      AND push_failed_at IS NULL;

-- Used by the fan-out reconciliation query (NOT EXISTS lookup per message).
CREATE INDEX IF NOT EXISTS idx_notifications_message_id
    ON notifications (message_id)
    WHERE message_id IS NOT NULL;

-- Wake-up triggers. Payloads are intentionally tiny — channel == intent,
-- payload is just message_id or empty string. Workers do the heavy lifting.
CREATE OR REPLACE FUNCTION notify_message_inserted() RETURNS TRIGGER AS $$
BEGIN
    PERFORM pg_notify('rfs_message', NEW.id::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_notify_message ON messages;
CREATE TRIGGER trg_notify_message
    AFTER INSERT ON messages
    FOR EACH ROW EXECUTE FUNCTION notify_message_inserted();

CREATE OR REPLACE FUNCTION notify_dispatch_pending() RETURNS TRIGGER AS $$
BEGIN
    IF NEW.push_required AND NEW.pushed_at IS NULL AND NEW.push_failed_at IS NULL THEN
        PERFORM pg_notify('rfs_dispatch', '');
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_notify_dispatch ON notifications;
CREATE TRIGGER trg_notify_dispatch
    AFTER INSERT ON notifications
    FOR EACH ROW EXECUTE FUNCTION notify_dispatch_pending();
