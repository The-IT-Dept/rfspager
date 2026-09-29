-- The Stripe integration reads and writes `users.stripe_id` and the
-- `stripe_subscription_*` columns (internal/stripe/handler.go), and the legacy
-- rows carry them, but no migration ever created them. Migration 000 only
-- reproduces the Laravel *migrations*, which do not include these columns, so a
-- database built from the Go migration set is missing all five. Every Stripe
-- code path then fails with `column "stripe_id" does not exist`: checkout,
-- portal, and both webhook handlers.
--
-- Add them additively so an existing Laravel database (that already has some or
-- all of them) is unaffected. `stripe_id` maps a Stripe customer to its user on
-- every inbound webhook, so index it for lookup.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS stripe_id TEXT,
    ADD COLUMN IF NOT EXISTS stripe_subscription_id TEXT,
    ADD COLUMN IF NOT EXISTS stripe_subscription_status TEXT,
    ADD COLUMN IF NOT EXISTS stripe_subscription_start_date TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS stripe_subscription_end_date TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_users_stripe_id
    ON users (stripe_id)
    WHERE stripe_id IS NOT NULL;