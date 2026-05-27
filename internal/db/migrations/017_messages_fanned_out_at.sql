-- Track when fanout has completed for a message, independent of whether any
-- recipients were found. Previously the fanout reconcile loop used
-- "messages with no notifications rows" as the orphan predicate, which meant
-- any message with zero eligible recipients (no Pro subscriber to that capcode
-- and no overlapping alert zone) would be re-reconciled every minute forever,
-- logging "recipients: 0" on every boot.
--
-- After this migration the worker sets `fanned_out_at` for every message it
-- processes — recipients found or not — and the reconcile query filters on
-- `fanned_out_at IS NULL` only.
--
-- Deliberately NO backfill UPDATE here: on a table with millions of rows the
-- 2M+ row UPDATE blocks server boot for minutes. Instead the reconcile query
-- is bounded to `created_at > NOW() - 1 hour`, so old messages with NULL
-- fanned_out_at are never selected and never get reprocessed. New messages
-- get the column set as they fan out via the updated fanoutSQL.
--
-- Deliberately NO partial index either: a non-concurrent CREATE INDEX on
-- 2M+ rows takes a write lock and is slow. The reconcile query is bounded
-- to one hour of data which the existing `idx_messages_pager_feed` index
-- covers cheaply. Add a partial index later via CREATE INDEX CONCURRENTLY
-- in a maintenance script if reconcile shows up in pg_stat_statements.

ALTER TABLE messages
    ADD COLUMN IF NOT EXISTS fanned_out_at timestamptz;

-- Defensive cleanup: an earlier draft of this migration created a partial
-- index that ended up holding 2M+ rows when the backfill didn't run. If a
-- previous run got that far before being interrupted, drop it.
DROP INDEX IF EXISTS idx_messages_fanout_pending;
