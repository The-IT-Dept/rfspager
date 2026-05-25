-- Append-only log of all IAP transactions for audit/reconciliation
CREATE TABLE IF NOT EXISTS iap_transactions (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    platform VARCHAR(20) NOT NULL, -- 'apple' or 'google'
    transaction_id VARCHAR(255), -- Apple transactionId or Google orderId
    original_transaction_id VARCHAR(255), -- Apple originalTransactionId or Google purchaseToken
    product_id VARCHAR(255) NOT NULL,
    event_type VARCHAR(50) NOT NULL, -- 'purchase', 'renewal', 'restore', 'cancel', 'refund', 'expire', 'webhook'
    is_valid BOOLEAN NOT NULL,
    expires_at TIMESTAMP,
    will_renew BOOLEAN,
    environment VARCHAR(20), -- 'Production' or 'Sandbox'
    raw_payload TEXT, -- JWS or webhook payload for debugging
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_iap_transactions_user_id ON iap_transactions(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_iap_transactions_original_txn ON iap_transactions(original_transaction_id) WHERE original_transaction_id IS NOT NULL;
