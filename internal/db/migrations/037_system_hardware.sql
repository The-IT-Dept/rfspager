-- Whether someone applying to host a receiver has the hardware ('own') or
-- would host one if we help with the gear ('needs'). Shown to admins so the
-- queue can be scanned at a glance. NULL for receivers that predate the
-- question.
ALTER TABLE systems
    ADD COLUMN IF NOT EXISTS hardware TEXT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'systems_hardware_check'
    ) THEN
        ALTER TABLE systems
            ADD CONSTRAINT systems_hardware_check CHECK (hardware IN ('own', 'needs'));
    END IF;
END $$;
