DROP INDEX IF EXISTS idx_usage_model_time;
DROP INDEX IF EXISTS idx_usage_provider_time;
DROP INDEX IF EXISTS idx_usage_key_time;
ALTER TABLE usage_records
    DROP COLUMN IF EXISTS cost_cents,
    DROP COLUMN IF EXISTS price_cents,
    DROP COLUMN IF EXISTS api_key_name,
    DROP COLUMN IF EXISTS api_key_id;
ALTER TABLE channels
    DROP COLUMN IF EXISTS model_mappings;
