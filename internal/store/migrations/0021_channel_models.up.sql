-- 渠道模型独立实体化: 把内嵌在 channels 的 models[]/model_mappings/model_costs
-- 规范化为 channel_models 表(每个「渠道×模型」一行),支持单模型级成本/上游名/权重/启停。
-- 详见 docs/superpowers/specs/2026-07-18-channel-models-entity-design.md

CREATE TABLE channel_models (
    id            text        PRIMARY KEY,
    channel_id    text        NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
    model_name    text        NOT NULL,                    -- 逻辑模型名(对外暴露)
    upstream_model text       NOT NULL DEFAULT '',         -- 上游真实模型名(''=同名直通)
    input_cost_cents_per_m   bigint NOT NULL DEFAULT 0,
    output_cost_cents_per_m  bigint NOT NULL DEFAULT 0,
    cache_read_cost_cents_per_m  bigint NOT NULL DEFAULT 0,
    cache_write_cost_cents_per_m bigint NOT NULL DEFAULT 0,
    weight        int         NOT NULL DEFAULT 1,          -- 0=继承渠道 weight
    status        text        NOT NULL DEFAULT 'active',   -- active | disabled: 单模型级启停
    created_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE(channel_id, model_name)
);
CREATE INDEX idx_channel_models_lookup  ON channel_models(model_name, status);
CREATE INDEX idx_channel_models_channel ON channel_models(channel_id);

-- 数据搬迁: 展开 channels.models[],合并 model_mappings(上游名)与 model_costs(成本)。
-- NULLIF 把空串转 NULL→COALESCE 兜底 0,避免 ''::bigint 报错。
-- id 用 md5(channel_id+model_name) 确定性,便于排查与 down 回溯。
INSERT INTO channel_models(id, channel_id, model_name, upstream_model,
    input_cost_cents_per_m, output_cost_cents_per_m, cache_read_cost_cents_per_m, cache_write_cost_cents_per_m,
    weight, status, created_at)
SELECT
    md5(c.id || '/' || m.model_name),
    c.id,
    m.model_name,
    COALESCE(c.model_mappings->>m.model_name, ''),
    COALESCE(NULLIF(c.model_costs->m.model_name->>'input_cost_cents_per_m','')::bigint, 0),
    COALESCE(NULLIF(c.model_costs->m.model_name->>'output_cost_cents_per_m','')::bigint, 0),
    COALESCE(NULLIF(c.model_costs->m.model_name->>'cache_read_cost_cents_per_m','')::bigint, 0),
    COALESCE(NULLIF(c.model_costs->m.model_name->>'cache_write_cost_cents_per_m','')::bigint, 0),
    1, 'active', c.created_at
FROM channels c, unnest(c.models) AS m(model_name);

-- 搬迁完毕,移除 channels 的内嵌字段(一次性切换,不留双写包袱)。
ALTER TABLE channels
    DROP COLUMN models,
    DROP COLUMN model_mappings,
    DROP COLUMN model_costs;
