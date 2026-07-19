-- 每模型可配置路由策略。
--   weighted    加权随机(默认,兼容历史)
--   round_robin 严格轮询(Redis 游标)
--   failover    主备(只用最高优先级组第一个,其余兜底)
--   random      纯随机(忽略权重)
ALTER TABLE models ADD COLUMN IF NOT EXISTS routing_strategy text NOT NULL DEFAULT 'weighted';
