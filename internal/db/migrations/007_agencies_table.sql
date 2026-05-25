-- Move from capcodes.agency (text) to capcodes.agency_id (FK -> agencies).
-- Keep the old text column populated via a trigger so the Laravel app continues
-- to work during the rollout. Once PHP is fully retired, drop the trigger and
-- the agency column in a separate migration.

CREATE TABLE IF NOT EXISTS agencies (
    id BIGSERIAL PRIMARY KEY,
    agency VARCHAR(255) NOT NULL UNIQUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

ALTER TABLE capcodes ADD COLUMN IF NOT EXISTS agency_id BIGINT REFERENCES agencies(id);

CREATE INDEX IF NOT EXISTS idx_capcodes_agency_id ON capcodes(agency_id);

-- Seed agencies from existing distinct values, then backfill agency_id.
-- Skipped on databases (e.g. fresh local dev) that never had capcodes.agency.
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_name = 'capcodes' AND column_name = 'agency'
  ) THEN
    INSERT INTO agencies (agency)
        SELECT DISTINCT TRIM(agency)
        FROM capcodes
        WHERE agency IS NOT NULL AND TRIM(agency) <> ''
    ON CONFLICT (agency) DO NOTHING;

    UPDATE capcodes c
       SET agency_id = a.id
      FROM agencies a
     WHERE c.agency_id IS NULL
       AND c.agency IS NOT NULL
       AND TRIM(c.agency) = a.agency;
  END IF;
END $$;

-- Trigger to keep capcodes.agency and capcodes.agency_id in sync during the
-- PHP/Go coexistence window. Only installed when the legacy text column still
-- exists — dev environments that never had it skip the trigger entirely.
--   * If Go writes only agency_id, fill agency from agencies.agency
--   * If PHP writes only agency, upsert into agencies and fill agency_id
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_name = 'capcodes' AND column_name = 'agency'
  ) THEN
    CREATE OR REPLACE FUNCTION sync_capcode_agency() RETURNS TRIGGER AS $fn$
    DECLARE
        a_text VARCHAR(255);
        a_id   BIGINT;
    BEGIN
        IF NEW.agency_id IS NOT NULL THEN
            SELECT agency INTO a_text FROM agencies WHERE id = NEW.agency_id;
            IF a_text IS NOT NULL AND (NEW.agency IS NULL OR TRIM(NEW.agency) <> a_text) THEN
                NEW.agency := a_text;
            END IF;
        ELSIF NEW.agency IS NOT NULL AND TRIM(NEW.agency) <> '' THEN
            INSERT INTO agencies (agency) VALUES (TRIM(NEW.agency))
                ON CONFLICT (agency) DO UPDATE SET updated_at = NOW()
                RETURNING id INTO a_id;
            NEW.agency_id := a_id;
        END IF;
        RETURN NEW;
    END;
    $fn$ LANGUAGE plpgsql;

    DROP TRIGGER IF EXISTS trg_sync_capcode_agency ON capcodes;
    CREATE TRIGGER trg_sync_capcode_agency
        BEFORE INSERT OR UPDATE ON capcodes
        FOR EACH ROW EXECUTE FUNCTION sync_capcode_agency();
  END IF;
END $$;
