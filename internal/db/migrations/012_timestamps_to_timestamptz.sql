-- Convert naive `timestamp` columns to `timestamptz`.
--
-- Background: Postgres columns were created as `timestamp without time zone`.
-- Production stores UTC in them: the database runs with TimeZone=Etc/UTC and
-- Laravel's config/app.php sets 'timezone' => 'UTC', so NOW() and Carbon both
-- produce UTC and it is written to the column naively.
--
-- Verified against production before writing this: the newest row in
-- `messages` trailed `now() AT TIME ZONE 'UTC'` by about five minutes on a feed
-- that receives pages continuously. If the stored values were Sydney local they
-- would have run ten hours ahead of UTC instead.
--
-- An earlier revision of this migration interpreted the existing values as
-- 'Australia/Sydney'. That assumption appears to have come from a developer
-- database running a Sydney session; against production data it shifts every
-- timestamp in the schema back by ten hours (eleven during AEDT). Any database
-- that ran that revision needs the shift undone, not this migration re-run.
--
-- Locking: each ALTER rewrites the whole table under an AccessExclusiveLock,
-- and because Migrate() executes this file as a single statement batch, all of
-- those locks are held together until the transaction commits. On a database
-- with a large `notifications` table this is a full outage for the duration,
-- not a brief pause — run it in a planned window via cmd/migrate-spike rather
-- than letting it fire implicitly on first boot.

DO $$
DECLARE
    r record;
BEGIN
    FOR r IN
        SELECT table_schema, table_name, column_name
        FROM information_schema.columns
        WHERE table_schema = 'public'
          AND data_type = 'timestamp without time zone'
    LOOP
        EXECUTE format(
            'ALTER TABLE %I.%I ALTER COLUMN %I TYPE timestamptz USING %I AT TIME ZONE ''UTC''',
            r.table_schema, r.table_name, r.column_name, r.column_name
        );
    END LOOP;
END $$;
