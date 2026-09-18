-- Add unique indexes on upstream_id for upsert support.
--
-- The Laravel importers had no uniqueness constraint and ran concurrently, so
-- real data contains a handful of race-condition duplicates (same upstream_id
-- inserted twice within seconds — consecutive ids, identical created_at).
-- Creating the index without clearing those first fails with 23505 and, because
-- Migrate() aborts on error, stops the server booting at all.
--
-- Keep the highest id per upstream_id: it carries the latest updated_at, which
-- is the row the Go importer's upsert would converge on anyway.

DELETE FROM majorincidents a
 USING majorincidents b
 WHERE a.upstream_id IS NOT NULL
   AND a.upstream_id = b.upstream_id
   AND a.id < b.id;

DELETE FROM hazardreductions a
 USING hazardreductions b
 WHERE a.upstream_id IS NOT NULL
   AND a.upstream_id = b.upstream_id
   AND a.id < b.id;

CREATE UNIQUE INDEX IF NOT EXISTS idx_hazardreductions_upstream_id ON hazardreductions(upstream_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_majorincidents_upstream_id ON majorincidents(upstream_id);
