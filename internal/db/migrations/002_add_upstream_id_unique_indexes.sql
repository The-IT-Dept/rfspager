-- Add unique indexes on upstream_id for upsert support
CREATE UNIQUE INDEX IF NOT EXISTS idx_hazardreductions_upstream_id ON hazardreductions(upstream_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_majorincidents_upstream_id ON majorincidents(upstream_id);
