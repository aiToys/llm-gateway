ALTER TABLE tenant_model_overrides ADD COLUMN IF NOT EXISTS output_cost_cents_per_m bigint NOT NULL DEFAULT 0;
ALTER TABLE tenant_model_overrides ADD COLUMN IF NOT EXISTS input_cost_cents_per_m bigint NOT NULL DEFAULT 0;
ALTER TABLE models ADD COLUMN IF NOT EXISTS output_cost_cents_per_m bigint NOT NULL DEFAULT 0;
ALTER TABLE models ADD COLUMN IF NOT EXISTS input_cost_cents_per_m bigint NOT NULL DEFAULT 0;
ALTER TABLE models ADD COLUMN IF NOT EXISTS provider text NOT NULL DEFAULT '';
