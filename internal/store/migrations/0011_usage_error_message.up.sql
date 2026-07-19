-- 用量记录补充错误信息(status='error' 时记录脱敏后的错误类别,便于排障)。
ALTER TABLE usage_records ADD COLUMN error_message text NOT NULL DEFAULT '';
