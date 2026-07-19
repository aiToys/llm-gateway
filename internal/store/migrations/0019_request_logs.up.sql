-- 请求/响应原文日志: 生产排障、合规审计、bug 复现。
-- 与 usage_records(只记用量元信息)互补,这里存请求/响应原文(可采样/截断)。
-- request_body/response_body/error 可空(采样或 LogBodies=false 时),故无 NOT NULL。
CREATE TABLE IF NOT EXISTS request_logs (
  id             text PRIMARY KEY,
  request_id     text NOT NULL,
  tenant_id      text NOT NULL DEFAULT '',
  user_id        text NOT NULL DEFAULT '',
  api_key_id     text NOT NULL DEFAULT '',
  model          text,
  provider       text,
  channel_id     text,
  method         text,
  path           text,
  status         int,
  latency_ms     int,
  input_tokens   int,
  output_tokens  int,
  price_cents    bigint,
  request_body   text,
  response_body  text,
  error          text,
  created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_reqlog_tenant_time ON request_logs(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_reqlog_key_time   ON request_logs(api_key_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_reqlog_reqid      ON request_logs(request_id);
