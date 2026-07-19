-- 模型可显式声明其归属供应商(展示用),与"由哪些渠道挂载"解耦。
-- 便于按真实供应商在模型广场/筛选展示,即便统一由 mock 渠道兜底路由。
ALTER TABLE models ADD COLUMN IF NOT EXISTS providers text[] NOT NULL DEFAULT '{}';
