-- 修复免单漏洞: uniq_ledger_usage_request 原仅含 request_id,
-- 客户端复用 X-Request-Id 头可跨用户触发 ON CONFLICT DO NOTHING 不扣款(上游 token 已消费)。
-- 改为 (user_id, request_id) 复合索引: 同一 request_id 仅对同一用户幂等(重试语义),
-- 跨用户即使复用相同 request_id 也各自计费,堵住外部可触发的免单路径。
DROP INDEX IF EXISTS uniq_ledger_usage_request;
CREATE UNIQUE INDEX uniq_ledger_usage_request
    ON public.billing_ledger (user_id, request_id) WHERE (type = 'usage'::text);
