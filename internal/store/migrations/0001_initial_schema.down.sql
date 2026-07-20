-- 回滚初始 schema: 整体删除并重建空 public schema。
-- 仅用于完全重置(会删除所有数据);增量回滚请用各版本的 .down.sql(v0.2.0 起新增)。
DROP SCHEMA IF EXISTS public CASCADE;
CREATE SCHEMA public;
