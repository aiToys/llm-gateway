-- 计费失败重试队列: relay 在响应完成"之后"才计费(usage 来自完整响应),若计费失败
-- 原先只记日志,会导致"上游已消费 token、平台已付成本、用户余额却没扣"的漏账。
-- 现把失败的应扣项落库,由后台 worker 幂等重试,保证最终一致。
CREATE TABLE IF NOT EXISTS pending_charges (
  id                 text PRIMARY KEY,
  tenant_id          text NOT NULL,
  user_id            text NOT NULL,
  request_id         text NOT NULL,          -- 幂等键(usage 请求级唯一,形如 req-<uuid>)
  model              text NOT NULL,
  input_tokens       int  NOT NULL DEFAULT 0,
  output_tokens      int  NOT NULL DEFAULT 0,
  cache_read_tokens  int  NOT NULL DEFAULT 0,
  cache_write_tokens int  NOT NULL DEFAULT 0,
  price_cents        bigint NOT NULL DEFAULT 0,
  cost_cents         bigint NOT NULL DEFAULT 0,
  attempts           int  NOT NULL DEFAULT 0,
  status             text NOT NULL DEFAULT 'pending',  -- pending | done | abandoned
  last_error         text,
  next_retry_at      timestamptz NOT NULL DEFAULT now(),
  created_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_pending_charges_retry ON pending_charges(status, next_retry_at);

-- 幂等: 同一 request_id 的 usage 账目至多一条。防"内联重试 / worker 重试 / 原路径重复提交"双扣余额。
-- usage 的 request_id 天然唯一(req-<uuid>);recharge/refund 的 request_id 非唯一,故仅对 usage 建部分唯一索引。
CREATE UNIQUE INDEX IF NOT EXISTS uniq_ledger_usage_request
  ON billing_ledger(request_id) WHERE type = 'usage';
