-- 修复支付订单交易号唯一索引的空串漏洞。
--
-- 0017 的部分唯一索引条件为 `transaction_id IS NOT NULL AND status='paid'`,
-- 但空字符串 '' 不是 NULL,仍参与唯一性。当某渠道(微信回调延迟 / 支付宝异步通知阶段 /
-- mock)偶发返回空 transaction_id 时,首个空 txn 的 paid 订单会阻塞同 provider 下所有
-- 后续空 txn 订单入账——SettlePaymentAtomic 的 UPDATE 触发唯一冲突,回调/查单重试永久失败,
-- 用户已付款却余额未加、订单卡 pending。
--
-- 加 `transaction_id <> ''` 排除空串:仅对有意义的交易号去重,空 txn 不再互相阻塞。
DROP INDEX IF EXISTS uniq_payment_orders_provider_txn;
CREATE UNIQUE INDEX uniq_payment_orders_provider_txn
  ON payment_orders(provider, transaction_id)
  WHERE transaction_id IS NOT NULL AND transaction_id <> '' AND status = 'paid';
