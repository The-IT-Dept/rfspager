-- Indexes for the message feed.
--
-- The mobile feed reads `deleted_at IS NULL AND capcode_id IS NOT NULL ORDER BY
-- created_at DESC`, and nothing indexed that shape. idx_messages_deleted_at_null
-- is keyed on (id DESC), so Postgres was reading all 2.25M rows, discarding 1.5M
-- by filter, top-N sorting the rest and returning 25. Every page paid for it:
-- 2,144 ms for page one on a copy of production, before pagination depth even
-- entered into it. With these indexes the same page costs ~14 ms, and a cursor
-- read deep in the archive ~2 ms.
--
-- The trailing `id DESC` matters: the feed paginates on the (created_at, id)
-- tuple because created_at is not unique, and the index has to match that
-- ordering exactly or the sort comes back.
--
-- Note these are built non-concurrently, because the migration runner wraps
-- every migration in a transaction and CREATE INDEX CONCURRENTLY cannot run
-- inside one. Each takes a ShareLock: reads are unaffected, writes to
-- `messages` block while it builds (~10s across all of them on a 16-core box,
-- longer on smaller hardware). To avoid that window entirely, build them by
-- hand with CREATE INDEX CONCURRENTLY before deploying — these are all
-- IF NOT EXISTS, so the migration then finds them and does nothing.

CREATE INDEX IF NOT EXISTS idx_messages_feed_known
    ON messages (created_at DESC, id DESC)
    WHERE deleted_at IS NULL AND capcode_id IS NOT NULL;

-- Paid users can opt into the unknown-capcode feed, which is the exact inverse
-- predicate and needs its own index.
CREATE INDEX IF NOT EXISTS idx_messages_feed_unknown
    ON messages (created_at DESC, id DESC)
    WHERE deleted_at IS NULL AND capcode_id IS NULL;

-- Search by capcode address. text_pattern_ops so a `LIKE 'prefix%'` can use the
-- index regardless of the database collation.
CREATE INDEX IF NOT EXISTS idx_messages_address_prefix
    ON messages (address text_pattern_ops)
    WHERE deleted_at IS NULL;

-- Every feed row asks `responding` for its responder count and whether the
-- caller is among them. responding_pkey is on (id) alone, so both subqueries
-- were sequentially scanning the table once per row returned.
CREATE INDEX IF NOT EXISTS idx_responding_message_user
    ON responding (message_id, user_id);
