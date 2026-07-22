-- 回滚至仅 request_id 的旧索引(含免单漏洞,仅用于开发回退)。
DROP INDEX IF EXISTS uniq_ledger_usage_request;
CREATE UNIQUE INDEX uniq_ledger_usage_request
    ON public.billing_ledger (request_id) WHERE (type = 'usage'::text);
