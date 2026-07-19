-- 支付订单表: 承载"下单 → 支付 → 回调入账"全生命周期,与 recharges/ledger 解耦。
--   out_trade_no 为商户订单号(全局唯一),作为幂等键抵御支付平台回调重入。
--   status 状态机: pending(待支付) → paid(已支付,已入账) / closed(超时关闭)。
--   回调成功后由 service 调 billing.Recharge 入账(写 recharges + ledger + 加余额)。
CREATE TABLE IF NOT EXISTS payment_orders (
  id             text PRIMARY KEY,
  tenant_id      text NOT NULL,
  user_id        text NOT NULL,
  out_trade_no   text NOT NULL UNIQUE,
  provider       text NOT NULL,                       -- wechat | alipay | mock
  amount_cents   bigint NOT NULL,
  status         text NOT NULL DEFAULT 'pending',     -- pending | paid | closed
  prepay_data    text,                                -- 微信 code_url / 支付宝跳转 URL / mock 占位
  transaction_id text,                                -- 支付平台流水号
  paid_at        timestamptz,
  expires_at     timestamptz NOT NULL,                -- 下单时间 + 有效期(默认 15min),超时关单
  created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_payment_orders_user   ON payment_orders(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_payment_orders_expire ON payment_orders(status, expires_at);
