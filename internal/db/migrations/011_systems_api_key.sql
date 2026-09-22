-- Runtime-provisioned receiver credentials. Never seed deployment keys in source.
ALTER TABLE systems ADD COLUMN IF NOT EXISTS api_key VARCHAR(64);
CREATE UNIQUE INDEX IF NOT EXISTS idx_systems_api_key
    ON systems (api_key) WHERE api_key IS NOT NULL;
