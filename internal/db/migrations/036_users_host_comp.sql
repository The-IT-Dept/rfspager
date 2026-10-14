-- Free Pro for receiver hosts ("host comp"). Kept apart from the store
-- entitlement columns (subscription_valid / _expiry / _product_id /
-- _platform): the comp job only writes these two, and the store, webhook,
-- Adapty and expiry paths never touch them. Effective access is
-- subscription_valid OR host_comp_until > NOW().
--
-- host_comp_until: access runs until then; extended to now + 7 days every
--   15 minutes while the user qualifies, so it lapses 7 days after they stop.
-- host_comp_since: start of the current comp period; NULL when not comped.
--   Set on the start transition, cleared on the lapse transition.
-- Design in docs/plans/2026-10-14-public-coverage-and-hosting.md, section 4.
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS host_comp_until TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS host_comp_since TIMESTAMPTZ NULL;
