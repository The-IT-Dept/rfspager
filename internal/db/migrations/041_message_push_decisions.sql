-- Why a page was or wasn't pushed, for the admin message page and for
-- checking the replay and damaged-copy rules in prod.
--
--   push_skip_reason  'replay': stored but nobody woken, because the paging
--                     network was re-sending old backlog (see
--                     internal/pagetext ReplayWatch).
--   push_lag_s        how far behind its own header the page was heard,
--                     when that decided anything.
--   push_hold_ms      how long fan-out held a damaged copy (or a stale page
--                     awaiting corroboration) before deciding.
--
-- Nullable with no default, so adding them doesn't rewrite the table.
ALTER TABLE messages
    ADD COLUMN IF NOT EXISTS push_skip_reason TEXT,
    ADD COLUMN IF NOT EXISTS push_lag_s       INTEGER,
    ADD COLUMN IF NOT EXISTS push_hold_ms     INTEGER;
