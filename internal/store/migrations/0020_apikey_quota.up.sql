-- API Key 级用量配额:日/月请求数 + 日/月 token 上限。
-- 0 表示不限。配合既有 RPM/TPM(分钟级)形成"分钟→天→月"的递进限流体系,防滥用与成本失控。
--   daily_request_limit/monthly_request_limit: 单 key 每日/每月最大请求数。
--   daily_token_limit/monthly_token_limit:     单 key 每日/每月最大 token(输入+输出)总量。
ALTER TABLE api_keys
    ADD COLUMN IF NOT EXISTS daily_request_limit   int NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS monthly_request_limit int NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS daily_token_limit     int NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS monthly_token_limit   int NOT NULL DEFAULT 0;
