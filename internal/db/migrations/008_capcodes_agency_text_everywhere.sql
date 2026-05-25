-- Make the legacy capcodes.agency text column exist on every environment so
-- the PHP/Go dual-write trigger is exercised in dev the same way it runs in
-- prod. Idempotent: ADD COLUMN IF NOT EXISTS is a no-op on prod, the backfill
-- is a no-op when text already matches, and CREATE OR REPLACE / DROP-then-
-- CREATE on the trigger overwrites with the same definition.

ALTER TABLE capcodes ADD COLUMN IF NOT EXISTS agency VARCHAR(255);

-- Backfill text from the agencies table for any rows that have agency_id set
-- but no text (Go-only dev environments).
UPDATE capcodes c
   SET agency = a.agency
  FROM agencies a
 WHERE c.agency_id = a.id
   AND (c.agency IS NULL OR TRIM(c.agency) = '');

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
