-- Never silently discard distinct legacy records to create an index.
-- Reconcile duplicate upstream IDs deliberately on a reviewed database copy
-- before cutover, then retry; the migration transaction preserves every row.
DO $$
BEGIN
    IF EXISTS (SELECT upstream_id FROM majorincidents WHERE upstream_id IS NOT NULL GROUP BY upstream_id HAVING COUNT(*) > 1) THEN
        RAISE EXCEPTION 'duplicate majorincidents.upstream_id values: reconcile legacy records before migrating';
    END IF;
    IF EXISTS (SELECT upstream_id FROM hazardreductions WHERE upstream_id IS NOT NULL GROUP BY upstream_id HAVING COUNT(*) > 1) THEN
        RAISE EXCEPTION 'duplicate hazardreductions.upstream_id values: reconcile legacy records before migrating';
    END IF;
END $$;
CREATE UNIQUE INDEX IF NOT EXISTS idx_hazardreductions_upstream_id ON hazardreductions(upstream_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_majorincidents_upstream_id ON majorincidents(upstream_id);
