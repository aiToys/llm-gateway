-- 成本下沉到渠道(供应商):同一逻辑模型路由到不同供应商时,各家上游单价不同,
-- 故成本应跟"实际命中的渠道"绑定,而非模型。售价仍留在模型(面向用户统一价)。
ALTER TABLE channels ADD COLUMN IF NOT EXISTS input_cost_cents_per_m bigint NOT NULL DEFAULT 0;
ALTER TABLE channels ADD COLUMN IF NOT EXISTS output_cost_cents_per_m bigint NOT NULL DEFAULT 0;

-- 数据迁移:把模型表上的历史成本拷贝到挂载该模型的渠道(取首个匹配作为初始估值,
-- 管理员随后可在渠道页按真实供应商单价校准)。
UPDATE channels c SET
  input_cost_cents_per_m  = COALESCE((SELECT m.input_cost_cents_per_m  FROM models m WHERE m.model_name = ANY(c.models) ORDER BY array_position(c.models, m.model_name) LIMIT 1), 0),
  output_cost_cents_per_m = COALESCE((SELECT m.output_cost_cents_per_m FROM models m WHERE m.model_name = ANY(c.models) ORDER BY array_position(c.models, m.model_name) LIMIT 1), 0);
