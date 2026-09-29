-- A store entitlement is 1:N, not 1:1.
--
-- Real users forget credentials and sign up again with a new address, or hold a
-- personal address, a work address, a Sign-in-with-Apple private relay and an
-- anonymous account at once, restoring the same subscription onto each. 023
-- enforced one owner per store identifier by releasing the losers' pointers,
-- which froze those accounts: no store webhook could reach them again and a
-- later /iap/restore returned a conflict.
--
-- Replace the 1:1 lock with a link table and rebuild the released links from the
-- audit trail 023 already wrote, so no Adapty re-run is required.
--
-- The "current subscription" for an account is still users.<store column>; the
-- ledger is now purely the set of accounts an identifier entitles. A webhook
-- updates only accounts whose current pointer is that identifier (or the Google
-- token being upgraded), so a historical event cannot clobber a newer purchase.

CREATE TABLE iap_subscription_ownership_new (
    platform TEXT NOT NULL CHECK (platform IN ('apple', 'google')),
    original_transaction_id TEXT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (platform, original_transaction_id, user_id)
);

-- Current pointers.
INSERT INTO iap_subscription_ownership_new (platform, original_transaction_id, user_id)
SELECT platform, original_transaction_id, user_id
FROM iap_subscription_ownership
WHERE user_id IS NOT NULL;

-- Accounts 023 released: recover the shared identifier from the audit rows.
INSERT INTO iap_subscription_ownership_new (platform, original_transaction_id, user_id)
SELECT DISTINCT t.platform, t.original_transaction_id, t.user_id
FROM iap_transactions t
WHERE t.event_type IN ('reconcile:ownership_released', 'backfill:adapty')
  AND t.platform IN ('apple', 'google')
  AND t.original_transaction_id IS NOT NULL
  AND t.original_transaction_id <> ''
  AND t.user_id IS NOT NULL
ON CONFLICT DO NOTHING;

-- Drop the 023 uniqueness invariant first: restoring the released pointers
-- below re-creates duplicates that the unique index would reject.
DROP INDEX IF EXISTS idx_users_apple_txn;
DROP INDEX IF EXISTS idx_users_google_token;

-- Give released accounts their pointer back so their row reflects the shared
-- entitlement immediately rather than waiting for the next restore.
UPDATE users u
SET apple_original_transaction_id = r.original_transaction_id
FROM (
    SELECT DISTINCT user_id, original_transaction_id
    FROM iap_transactions
    WHERE event_type = 'reconcile:ownership_released' AND platform = 'apple'
) r
WHERE u.id = r.user_id AND u.apple_original_transaction_id IS NULL;

UPDATE users u
SET google_purchase_token = r.original_transaction_id
FROM (
    SELECT DISTINCT user_id, original_transaction_id
    FROM iap_transactions
    WHERE event_type = 'reconcile:ownership_released' AND platform = 'google'
) r
WHERE u.id = r.user_id AND u.google_purchase_token IS NULL;

DROP TABLE iap_subscription_ownership;
ALTER TABLE iap_subscription_ownership_new RENAME TO iap_subscription_ownership;

CREATE INDEX iap_subscription_ownership_user_id_idx
    ON iap_subscription_ownership (user_id);

-- Multiple accounts may now share a store identifier.
CREATE INDEX idx_users_apple_txn
    ON users (apple_original_transaction_id)
    WHERE apple_original_transaction_id IS NOT NULL;

CREATE INDEX idx_users_google_token
    ON users (google_purchase_token)
    WHERE google_purchase_token IS NOT NULL;
