-- Convert naive `timestamp` columns to `timestamptz`.
--
-- Background: Postgres columns were created as `timestamp without time zone`
-- and the DB session runs in Australia/Sydney. Inserts using NOW() therefore
-- stored Sydney local time as a naive value. Go's database/sql then read those
-- values back as UTC and the JSON layer stamped them with `Z`, leaving every
-- timestamp surfaced by the API 10–11 hours off (visible on the new Systems
-- page as "in 10 hours").
--
-- Fix: convert every naive timestamp column to `timestamptz`, interpreting the
-- existing values as Sydney local. Postgres then stores the correct UTC instant
-- and serialises it to JSON with the proper offset. Once this lands, all future
-- inserts using NOW() or `CURRENT_TIMESTAMP` are timezone-aware and correct.
--
-- Safe to run on a running DB (each ALTER takes a brief AccessExclusiveLock on
-- the table only). Application code uses `time.Time` which scans equally well
-- from both column types, so no Go changes are needed.

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
            'ALTER TABLE %I.%I ALTER COLUMN %I TYPE timestamptz USING %I AT TIME ZONE ''Australia/Sydney''',
            r.table_schema, r.table_name, r.column_name, r.column_name
        );
    END LOOP;
END $$;
