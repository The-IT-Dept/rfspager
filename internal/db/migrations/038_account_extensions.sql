-- Free time given from the admin user page ("Give free time"). The method
-- follows the account: an Apple renewal date extension or a Google Play
-- deferral for an active store subscription (so the store doesn't charge
-- until the free time runs out), otherwise a gift in our own columns
-- (io.devlan.rfspager.gifted). One row per grant, failed ones included, so
-- the page shows what was tried and why.
--
-- days_applied: less than days_requested when a two-call Apple grant
--   (over 90 days) had its second call refused (status 'partial').
-- store_requests: Apple request identifiers of the calls that succeeded;
--   their count over 365 days is the subscriber's Apple allowance used
--   (Apple allows two extensions a year). Empty for Google and gifts.
-- expiry_before/after: the subscription expiry around the grant; for stores,
--   after is what the store reported (the webhook then writes it to users).
--
-- The expiry sweep lapses a gifted account at its expiry only when it has an
-- applied gift here; older gifts (no row) keep never expiring.
CREATE TABLE IF NOT EXISTS account_extensions (
    id             BIGSERIAL PRIMARY KEY,
    user_id        BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    admin_user_id  BIGINT NULL REFERENCES admin_users(id) ON DELETE SET NULL,
    method         TEXT NOT NULL CHECK (method IN ('apple', 'google', 'gift')),
    status         TEXT NOT NULL CHECK (status IN ('applied', 'partial', 'failed')),
    days_requested INT NOT NULL CHECK (days_requested > 0),
    days_applied   INT NOT NULL DEFAULT 0,
    reason_code    INT NULL,
    note           TEXT NOT NULL,
    expiry_before  TIMESTAMPTZ NULL,
    expiry_after   TIMESTAMPTZ NULL,
    store_requests TEXT[] NOT NULL DEFAULT '{}',
    error          TEXT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_account_extensions_user
    ON account_extensions (user_id, created_at DESC);
