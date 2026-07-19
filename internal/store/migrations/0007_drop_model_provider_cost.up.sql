-- 领域模型修正:成本下沉到渠道(0006)后,模型层不再保留成本;
-- provider 字段在"一模型多供应商"下具误导性,移除——供应商纯粹是渠道属性。
ALTER TABLE models DROP COLUMN IF EXISTS provider;
ALTER TABLE models DROP COLUMN IF EXISTS input_cost_cents_per_m;
ALTER TABLE models DROP COLUMN IF EXISTS output_cost_cents_per_m;

-- 租户覆盖只管售价与启停,成本归渠道,移除 overrides 的成本列。
ALTER TABLE tenant_model_overrides DROP COLUMN IF EXISTS input_cost_cents_per_m;
ALTER TABLE tenant_model_overrides DROP COLUMN IF EXISTS output_cost_cents_per_m;
