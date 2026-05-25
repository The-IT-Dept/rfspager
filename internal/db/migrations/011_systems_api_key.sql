-- Direct ingress takeover from Pagermon. Pagermon clients POST to
-- /api/messages with an `apikey` header — we now match that key to a system
-- row and run our standard ingestion. Once all clients are repointed and the
-- old uuid-in-URL form (/api/messages/{system}) is retired, this is the only
-- ingress path.

ALTER TABLE systems ADD COLUMN IF NOT EXISTS api_key VARCHAR(64);

CREATE UNIQUE INDEX IF NOT EXISTS idx_systems_api_key
    ON systems (api_key) WHERE api_key IS NOT NULL;

-- Seed the keys that Pagermon was already using. Matches by case-insensitive
-- name prefix so the seeding survives slightly different casing/suffixes
-- between Pagermon and our rows. Idempotent: only fills NULL slots.
UPDATE systems SET api_key = '35V3CA7DLNO063HC5X0D2X0'
 WHERE api_key IS NULL AND LOWER(name) LIKE 'rfspager-1%';

UPDATE systems SET api_key = '7Q030VBGGDG07MYFKNCERYC0'
 WHERE api_key IS NULL AND LOWER(name) LIKE 'rfspager-2%';

UPDATE systems SET api_key = '5IC65K95TDW07V9CW3ROF0K0'
 WHERE api_key IS NULL AND LOWER(name) LIKE 'rfspager-3%';

UPDATE systems SET api_key = '3II74R1MFF8014VR5ZHVPWLC'
 WHERE api_key IS NULL AND LOWER(name) LIKE 'raymond-terrace-1%';

UPDATE systems SET api_key = '84SAODIIRVW03JHOJTUSP720'
 WHERE api_key IS NULL AND LOWER(name) LIKE 'narrabeen-1%';
