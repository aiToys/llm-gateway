-- 余额非负约束: 数据库层兜底防止应用层 bug/并发把余额扣成负数。
-- 配合 ChargeAtomic 的 min(prev, price) 扣款策略,余额下界为 0。
ALTER TABLE users ADD CONSTRAINT users_balance_nonnegative CHECK (balance_cents >= 0);
