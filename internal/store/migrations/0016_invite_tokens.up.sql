-- 团队邀请令牌: 租户管理员生成签名链接,同事凭链接注册即加入该租户。
-- 存 hash(明文 token 只在创建时返回一次),支持列表/吊销/审计。复用 API Key 的 hash 范式。
CREATE TABLE IF NOT EXISTS invite_tokens (
  id          text PRIMARY KEY,
  token_hash  text NOT NULL UNIQUE,
  tenant_id   text NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  role        text NOT NULL DEFAULT 'member',  -- 被邀请者角色: member | admin
  created_by  text NOT NULL,                    -- 邀请人 user_id
  expires_at  timestamptz NOT NULL,
  used_at     timestamptz,                      -- 可空: 已被接受
  used_by     text,                             -- 可空: 接受者 user_id
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_invite_tokens_tenant ON invite_tokens(tenant_id, created_at DESC);
