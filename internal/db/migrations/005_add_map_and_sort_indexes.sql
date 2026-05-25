-- The Go message ingester extracts lat/lng from message text and stores them
-- here. Prod (Laravel) never had these columns; add them as nullable strings
-- so the index below is valid. Existing 2.1M rows stay NULL and are filtered
-- out by the map endpoint.
ALTER TABLE messages ADD COLUMN IF NOT EXISTS lat VARCHAR(50);
ALTER TABLE messages ADD COLUMN IF NOT EXISTS lng VARCHAR(50);

-- Index for messages/map endpoint: filters deleted_at IS NULL + lat/lng NOT NULL, orders by created_at
CREATE INDEX IF NOT EXISTS idx_messages_map
  ON messages(created_at DESC)
  WHERE deleted_at IS NULL AND lat IS NOT NULL AND lng IS NOT NULL;

-- Index for major incidents listing (ORDER BY created_at DESC)
CREATE INDEX IF NOT EXISTS idx_majorincidents_created_at
  ON majorincidents(created_at DESC);

-- Index for hazard reductions listing (ORDER BY start_at DESC)
CREATE INDEX IF NOT EXISTS idx_hazardreductions_start_at
  ON hazardreductions(start_at DESC);
