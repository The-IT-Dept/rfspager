-- The Go importer read the RFS feed's pubDate (which is UTC) as Sydney local
-- time, so every major incident it inserted has created_at 10-11 hours early
-- (the app showed brand-new incidents as "11 hours ago"). Re-read each stored
-- instant as the Sydney wall-clock time it came from and take that as UTC:
-- the exact inverse of the bug, correct across daylight saving.
--
-- Rows up to id 172573 were inserted by Laravel before the 2026-10-06
-- cutover (max id in the cutover dump) and are already right; upserts never
-- touch created_at, so only rows the Go importer inserted are shifted. The
-- fixed importer starts after migrations run, so nothing is corrected twice.
-- On an installation that never ran the Laravel importer every row came from
-- the Go importer, and the bound is harmless.
UPDATE majorincidents
   SET created_at = (created_at AT TIME ZONE 'Australia/Sydney') AT TIME ZONE 'UTC'
 WHERE id > 172573
   AND created_at IS NOT NULL;
