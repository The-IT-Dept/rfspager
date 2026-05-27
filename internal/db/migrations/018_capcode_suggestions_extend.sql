-- The capcode_suggestions table exists from the Laravel era with the basic
-- shape (id, user_id, address, agency, alias, icon, colour, timestamps). We
-- now want logged-in users to amend existing capcodes — not just submit new
-- ones — and to attach a justification note to each suggestion. The admin
-- approve flow will use `capcode_id IS NOT NULL` as the signal to UPDATE
-- the existing capcode instead of INSERTing a new one.

ALTER TABLE capcode_suggestions
    ADD COLUMN IF NOT EXISTS note TEXT;

ALTER TABLE capcode_suggestions
    ADD COLUMN IF NOT EXISTS capcode_id BIGINT REFERENCES capcodes(id) ON DELETE SET NULL;

-- One pending suggestion per (user, capcode) so a user doesn't spam the
-- queue with the same amendment. Pending = row exists; approved/rejected
-- = row deleted, so this constraint just prevents duplicate live entries.
CREATE INDEX IF NOT EXISTS idx_capcode_suggestions_capcode_id
    ON capcode_suggestions (capcode_id)
 WHERE capcode_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_capcode_suggestions_user_id_created_at
    ON capcode_suggestions (user_id, created_at DESC);
