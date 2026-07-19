-- API Key 级 IP 白名单:为空数组表示不限制;非空时仅允许列表内来源 IP 调用。
ALTER TABLE api_keys ADD COLUMN ip_whitelist text[] NOT NULL DEFAULT '{}';
