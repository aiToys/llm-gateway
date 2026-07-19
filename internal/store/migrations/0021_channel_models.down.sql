-- 回滚: 重建 channels 的 models[]/model_mappings/model_costs,从 channel_models 反向聚合。
ALTER TABLE channels
    ADD COLUMN models         text[] NOT NULL DEFAULT '{}',
    ADD COLUMN model_mappings jsonb  NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN model_costs    jsonb  NOT NULL DEFAULT '{}'::jsonb;

-- models[]: 聚合 channel_models.model_name。
UPDATE channels c SET models = agg.models
FROM (SELECT channel_id, array_agg(model_name) AS models FROM channel_models GROUP BY channel_id) agg
WHERE agg.channel_id = c.id;

-- model_mappings: {"model":"upstream"},仅含 upstream_model 非空的(空=同名直通,不记)。
UPDATE channels c SET model_mappings = agg.m
FROM (
    SELECT channel_id,
        COALESCE(jsonb_object_agg(model_name, upstream_model) FILTER (WHERE upstream_model <> ''), '{}'::jsonb) AS m
    FROM channel_models GROUP BY channel_id
) agg
WHERE agg.channel_id = c.id;

-- model_costs: {"model":{"input_cost_cents_per_m":..,"output_cost_cents_per_m":..,...}}。
UPDATE channels c SET model_costs = agg.c
FROM (
    SELECT channel_id,
        COALESCE(jsonb_object_agg(model_name, jsonb_build_object(
            'input_cost_cents_per_m', input_cost_cents_per_m,
            'output_cost_cents_per_m', output_cost_cents_per_m,
            'cache_read_cost_cents_per_m', cache_read_cost_cents_per_m,
            'cache_write_cost_cents_per_m', cache_write_cost_cents_per_m
        )), '{}'::jsonb) AS c
    FROM channel_models GROUP BY channel_id
) agg
WHERE agg.channel_id = c.id;

DROP TABLE IF EXISTS channel_models;
