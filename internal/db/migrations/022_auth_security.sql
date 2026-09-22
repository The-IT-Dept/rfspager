-- Versioned sessions revoke all previously-issued tokens after logout or a
-- credential change. Defaults retain existing sessions until the Go cutover,
-- whose strict claim contract invalidates legacy tokens separately.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS token_version BIGINT NOT NULL DEFAULT 0;

ALTER TABLE admin_users
    ADD COLUMN IF NOT EXISTS token_version BIGINT NOT NULL DEFAULT 0;

-- The raw recovery secret is sent only by the configured mail transport. The
-- database stores a SHA-256 digest, one token is active per user, and reset
-- consumption marks it used inside the password-change transaction.
CREATE TABLE IF NOT EXISTS password_reset_tokens (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash BYTEA NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_password_reset_tokens_active
    ON password_reset_tokens (token_hash)
    WHERE used_at IS NULL;
