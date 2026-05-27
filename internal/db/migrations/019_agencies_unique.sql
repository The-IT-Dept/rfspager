-- The Laravel-era schema declared `agencies.agency` as NOT NULL but didn't
-- add the UNIQUE constraint that the Go migration 007 assumed. Approving a
-- capcode suggestion blew up with "no unique or exclusion constraint matching
-- the ON CONFLICT specification" because the upsert on agency name couldn't
-- find anything to conflict on.
--
-- This is idempotent — the index name is fixed and CREATE UNIQUE INDEX IF
-- NOT EXISTS skips when the index already exists. We use a named index so
-- the upsert can reference it explicitly via `ON CONFLICT (agency)`.

CREATE UNIQUE INDEX IF NOT EXISTS agencies_agency_unique
    ON agencies (agency);
