-- 支付重放兜底: 同一渠道的同一笔交易号(transaction_id)在已支付订单中唯一。
-- 防同一笔微信/支付宝交易号被绑定到多个订单(重放/脏数据)。仅对已 paid 且有 transaction_id 的行生效。
CREATE UNIQUE INDEX IF NOT EXISTS uniq_payment_orders_provider_txn
  ON payment_orders(provider, transaction_id)
  WHERE transaction_id IS NOT NULL AND status = 'paid';
