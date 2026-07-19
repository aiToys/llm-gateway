-- 0003: 多供应商路由 + 用量多维聚合
-- 渠道模型映射: logical_model -> 上游真实模型名(一个逻辑模型可由多个渠道/供应商承载,且上游名可不同)
ALTER TABLE channels
    ADD COLUMN IF NOT EXISTS model_mappings jsonb NOT NULL DEFAULT '{}'::jsonb;

-- 用量记录补充维度与费用,使一张表即可完成按 key/模型/供应商 的 TPM/RPM/费用聚合
ALTER TABLE usage_records
    ADD COLUMN IF NOT EXISTS api_key_id   text   NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS api_key_name text   NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS price_cents  bigint NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS cost_cents   bigint NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_usage_key_time      ON usage_records(api_key_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_usage_provider_time ON usage_records(provider, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_usage_model_time    ON usage_records(model, created_at DESC);
