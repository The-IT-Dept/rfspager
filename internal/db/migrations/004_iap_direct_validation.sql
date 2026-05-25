-- Add columns for direct Apple/Google store validation (replacing Adapty)
ALTER TABLE users ADD COLUMN IF NOT EXISTS apple_original_transaction_id VARCHAR(255);
ALTER TABLE users ADD COLUMN IF NOT EXISTS google_purchase_token TEXT;
ALTER TABLE users ADD COLUMN IF NOT EXISTS subscription_platform VARCHAR(20);
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_store_sync_at TIMESTAMP;

CREATE INDEX IF NOT EXISTS idx_users_apple_txn ON users(apple_original_transaction_id) WHERE apple_original_transaction_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_users_google_token ON users(google_purchase_token) WHERE google_purchase_token IS NOT NULL;
