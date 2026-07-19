-- 模型缓存命中定价: cache_read(命中读取,通常折扣)、cache_write(写入缓存,通常加价)。
-- 0 表示该模型不启用缓存分段计价,缓存 token 按普通输入价核算(向后兼容)。
ALTER TABLE models ADD COLUMN IF NOT EXISTS cache_read_price_cents_per_m bigint NOT NULL DEFAULT 0;
ALTER TABLE models ADD COLUMN IF NOT EXISTS cache_write_price_cents_per_m bigint NOT NULL DEFAULT 0;
