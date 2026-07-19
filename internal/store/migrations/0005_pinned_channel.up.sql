-- pinned 路由策略: 固定到指定渠道(供应商),其余渠道仅作故障转移候选。
ALTER TABLE models ADD COLUMN IF NOT EXISTS pinned_channel_id text;
