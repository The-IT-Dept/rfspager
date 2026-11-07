-- Supporters: people besides a receiver's owner who get the host comp while
-- that receiver qualifies (e.g. a brigade member who paid for half the gear).
-- Added and removed from the admin system page; the host comp job treats
-- them like the owner for that receiver.
--
-- conversation_uuid is the supporter's own Libredesk thread for their comp
-- emails. It is never the receiver's support_conversation_uuid: that thread
-- is the owner's application, which holds the install command and API key.
CREATE TABLE IF NOT EXISTS system_supporters (
    system_id         BIGINT NOT NULL REFERENCES systems(id) ON DELETE CASCADE,
    user_id           BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    note              TEXT NOT NULL,
    admin_user_id     BIGINT NULL REFERENCES admin_users(id) ON DELETE SET NULL,
    conversation_uuid TEXT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (system_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_system_supporters_user
    ON system_supporters (user_id);
