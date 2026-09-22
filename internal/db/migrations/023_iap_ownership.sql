-- Store identifiers are durable ownership evidence, while the similarly named
-- columns on users point only at that account's current subscription. Keep a
-- ledger so an old receipt cannot be replayed after a user later buys a new
-- subscription or Google rotates a purchase token during an upgrade.
--
-- Reconcile duplicate bindings before enforcing the invariant.
--
-- Production arrives here with 274 Apple transactions and 75 Google tokens each
-- bound to more than one user (617 and ~160 accounts respectively). These are
-- not contested purchases: they are one person holding several accounts — a
-- personal address, a work address, a Sign-in-with-Apple private relay, an
-- anonymous account — who restored the same subscription onto each. The Adapty
-- backfill then wrote identical subscription state to every one of them, which
-- is why no duplicate group has a single valid member: either all of them look
-- valid or none do.
--
-- So we de-duplicate the *pointer* only, and never the entitlement. The winner
-- keeps `users.<store id>` and takes the ledger row; the others have their
-- pointer cleared but keep subscription_valid / subscription_expiry exactly as
-- they are. Nobody loses access today. The released accounts simply stop
-- receiving store updates and lapse at the expiry they already had, while the
-- owning account keeps renewing.
--
-- The winner is the account actually in use: most live installations first,
-- then most recent install activity, then a valid subscription, then the most
-- recent store sync, then the oldest account id so the choice is deterministic.
-- On production data the install count alone decides 202 of 274 Apple groups
-- and 50 of 75 Google ones.

DROP TABLE IF EXISTS iap_ownership_released;
CREATE TEMP TABLE iap_ownership_released (
    platform   TEXT   NOT NULL,
    user_id    BIGINT NOT NULL,
    identifier TEXT   NOT NULL
);

INSERT INTO iap_ownership_released (platform, user_id, identifier)
SELECT 'apple', id, identifier FROM (
    SELECT u.id,
           u.apple_original_transaction_id AS identifier,
           ROW_NUMBER() OVER (
               PARTITION BY u.apple_original_transaction_id
               ORDER BY (SELECT count(*) FROM installations i
                          WHERE i.user_id = u.id AND i.deleted_at IS NULL) DESC,
                        (SELECT max(i.updated_at) FROM installations i
                          WHERE i.user_id = u.id AND i.deleted_at IS NULL) DESC NULLS LAST,
                        u.subscription_valid DESC,
                        u.last_store_sync_at DESC NULLS LAST,
                        u.id ASC
           ) AS rn
      FROM users u
     WHERE u.apple_original_transaction_id IS NOT NULL
) ranked WHERE rn > 1;

INSERT INTO iap_ownership_released (platform, user_id, identifier)
SELECT 'google', id, identifier FROM (
    SELECT u.id,
           u.google_purchase_token AS identifier,
           ROW_NUMBER() OVER (
               PARTITION BY u.google_purchase_token
               ORDER BY (SELECT count(*) FROM installations i
                          WHERE i.user_id = u.id AND i.deleted_at IS NULL) DESC,
                        (SELECT max(i.updated_at) FROM installations i
                          WHERE i.user_id = u.id AND i.deleted_at IS NULL) DESC NULLS LAST,
                        u.subscription_valid DESC,
                        u.last_store_sync_at DESC NULLS LAST,
                        u.id ASC
           ) AS rn
      FROM users u
     WHERE u.google_purchase_token IS NOT NULL
) ranked WHERE rn > 1;

-- Leave support an audit trail: which account held which identifier, and what
-- access it had at the moment the pointer was released.
INSERT INTO iap_transactions
    (user_id, platform, original_transaction_id, product_id, event_type,
     is_valid, expires_at, raw_payload, created_at)
SELECT r.user_id, r.platform, r.identifier,
       COALESCE(u.subscription_product_id, 'unknown'),
       'reconcile:ownership_released',
       COALESCE(u.subscription_valid, false),
       u.subscription_expiry,
       'released duplicate ' || r.platform || ' binding during migration 023; '
         || 'subscription_valid and subscription_expiry left untouched',
       NOW()
  FROM iap_ownership_released r
  JOIN users u ON u.id = r.user_id;

-- Clear the duplicate pointers. Entitlement columns are deliberately not touched.
UPDATE users u
   SET apple_original_transaction_id = NULL, updated_at = NOW()
  FROM iap_ownership_released r
 WHERE r.platform = 'apple' AND r.user_id = u.id;

UPDATE users u
   SET google_purchase_token = NULL, updated_at = NOW()
  FROM iap_ownership_released r
 WHERE r.platform = 'google' AND r.user_id = u.id;

DROP TABLE iap_ownership_released;

-- Belt and braces: the unique indexes below would fail anyway, but a clear
-- message beats a raw constraint violation if reconciliation ever misses a shape.
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
            'cannot enforce IAP ownership: duplicate Apple subscription ownership survived reconciliation'
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
            'cannot enforce IAP ownership: duplicate Google subscription ownership survived reconciliation'
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
