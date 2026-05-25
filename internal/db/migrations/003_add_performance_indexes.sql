-- Partial index for non-deleted messages (fast count queries)
CREATE INDEX IF NOT EXISTS idx_messages_deleted_at_null ON messages(id DESC) WHERE deleted_at IS NULL;

-- Composite index for last message per system (fast MAX(created_at) lookups)
CREATE INDEX IF NOT EXISTS idx_messages_system_id_created_at ON messages(system_id, created_at DESC);

-- Widen message column to text (production has messages > 255 chars)
-- Must drop and recreate tsvector generated column first
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_name = 'messages' AND column_name = 'message' AND data_type = 'character varying'
  ) THEN
    ALTER TABLE messages DROP COLUMN IF EXISTS ts;
    ALTER TABLE messages ALTER COLUMN message TYPE text;
    ALTER TABLE messages ADD COLUMN ts tsvector GENERATED ALWAYS AS (to_tsvector('english', message)) STORED;
    CREATE INDEX IF NOT EXISTS messages_ts_idx ON messages USING GIN (ts);
  END IF;
END $$;
