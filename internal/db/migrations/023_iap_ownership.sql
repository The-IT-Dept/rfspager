-- Store identifiers are durable ownership evidence, while the similarly named
-- columns on users point only at that account's current subscription. Keep a
-- ledger so an old receipt cannot be replayed after a user later buys a new
-- subscription or Google rotates a purchase token during an upgrade.
--
-- Fail closed if historical user columns already bind one entitlement to more
-- than one user. This migration never picks a winner or rewrites paid access.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM users
        WHERE apple_original_transaction_id IS NOT NULL
        GROUP BY apple_original_transaction_id
        HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION
            'cannot enforce IAP ownership: duplicate Apple subscription ownership exists; reconcile duplicate users before migrating'
            USING ERRCODE = '23505';
    END IF;

    IF EXISTS (
        SELECT 1
        FROM users
        WHERE google_purchase_token IS NOT NULL
        GROUP BY google_purchase_token
        HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION
            'cannot enforce IAP ownership: duplicate Google subscription ownership exists; reconcile duplicate users before migrating'
            USING ERRCODE = '23505';
    END IF;
END $$;

CREATE TABLE iap_subscription_ownership (
    platform TEXT NOT NULL CHECK (platform IN ('apple', 'google')),
    original_transaction_id TEXT NOT NULL,
    -- Keep a historical claim if an account is deleted. A null owner can never
    -- be reclaimed automatically; support must resolve it from store evidence.
    user_id BIGINT REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (platform, original_transaction_id)
);

-- Snapshot every existing active pointer into the durable ledger.
INSERT INTO iap_subscription_ownership (platform, original_transaction_id, user_id)
SELECT 'apple', apple_original_transaction_id, id
FROM users
WHERE apple_original_transaction_id IS NOT NULL;

INSERT INTO iap_subscription_ownership (platform, original_transaction_id, user_id)
SELECT 'google', google_purchase_token, id
FROM users
WHERE google_purchase_token IS NOT NULL;

-- 004 created non-unique lookup indexes. Keep current user pointers unique as
-- an additional invariant; the ledger is authoritative for historical IDs.
DROP INDEX IF EXISTS idx_users_apple_txn;
CREATE UNIQUE INDEX idx_users_apple_txn
    ON users (apple_original_transaction_id)
    WHERE apple_original_transaction_id IS NOT NULL;

DROP INDEX IF EXISTS idx_users_google_token;
CREATE UNIQUE INDEX idx_users_google_token
    ON users (google_purchase_token)
    WHERE google_purchase_token IS NOT NULL;
