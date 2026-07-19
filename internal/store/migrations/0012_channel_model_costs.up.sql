-- 渠道按模型覆盖成本(model_costs):同一渠道挂多模型时,可为每个模型单独设定进货价。
-- 计费优先取 model_costs[逻辑模型名],回退渠道级 input/output_cost_cents_per_m。
-- 解决"同渠道多模型共用一组成本价"无法反映真实进货价差异(如 qwen-turbo vs qwen-max)。
ALTER TABLE channels ADD COLUMN IF NOT EXISTS model_costs jsonb NOT NULL DEFAULT '{}';
