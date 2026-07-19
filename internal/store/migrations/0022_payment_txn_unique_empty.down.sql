-- 回滚到 0017 的原始定义(含空串漏洞,仅用于回滚)。
DROP INDEX IF EXISTS uniq_payment_orders_provider_txn;
CREATE UNIQUE INDEX uniq_payment_orders_provider_txn
  ON payment_orders(provider, transaction_id)
  WHERE transaction_id IS NOT NULL AND status = 'paid';
