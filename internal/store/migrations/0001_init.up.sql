-- 初始 schema
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE tenants (
    id          text PRIMARY KEY,
    name        text NOT NULL,
    slug        text NOT NULL UNIQUE,
    status      text NOT NULL DEFAULT 'active',
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE users (
    id            text PRIMARY KEY,
    tenant_id     text NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    email         text NOT NULL,
    password_hash text NOT NULL,
    role          text NOT NULL DEFAULT 'member',
    status        text NOT NULL DEFAULT 'active',
    balance_cents bigint NOT NULL DEFAULT 0,
    created_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, email)
);
CREATE INDEX idx_users_tenant ON users(tenant_id);

CREATE TABLE api_keys (
    id          text PRIMARY KEY,
    tenant_id   text NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id     text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key_prefix  text NOT NULL,
    key_hash    text NOT NULL UNIQUE,
    name        text NOT NULL,
    scopes      text[] NOT NULL DEFAULT '{}',
    models      text[] NOT NULL DEFAULT '{}',
    rpm_limit   int NOT NULL DEFAULT 0,
    tpm_limit   int NOT NULL DEFAULT 0,
    expires_at  timestamptz,
    last_used_at timestamptz,
    status      text NOT NULL DEFAULT 'active',
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_api_keys_tenant ON api_keys(tenant_id);
CREATE INDEX idx_api_keys_hash ON api_keys(key_hash);

CREATE TABLE channels (
    id          text PRIMARY KEY,
    tenant_id   text REFERENCES tenants(id) ON DELETE CASCADE,
    provider    text NOT NULL,
    name        text NOT NULL,
    base_url    text NOT NULL DEFAULT '',
    api_key_enc text NOT NULL DEFAULT '',
    models      text[] NOT NULL DEFAULT '{}',
    priority    int NOT NULL DEFAULT 0,
    weight      int NOT NULL DEFAULT 1,
    status      text NOT NULL DEFAULT 'active',
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_channels_tenant ON channels(tenant_id);
CREATE INDEX idx_channels_provider ON channels(provider);

CREATE TABLE models (
    model_name                 text PRIMARY KEY,
    provider                   text NOT NULL,
    input_price_cents_per_m    bigint NOT NULL DEFAULT 0,
    output_price_cents_per_m   bigint NOT NULL DEFAULT 0,
    input_cost_cents_per_m     bigint NOT NULL DEFAULT 0,
    output_cost_cents_per_m    bigint NOT NULL DEFAULT 0,
    enabled                    boolean NOT NULL DEFAULT true
);

CREATE TABLE tenant_model_overrides (
    tenant_id                  text NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    model_name                 text NOT NULL,
    input_price_cents_per_m    bigint NOT NULL,
    output_price_cents_per_m   bigint NOT NULL,
    input_cost_cents_per_m     bigint NOT NULL DEFAULT 0,
    output_cost_cents_per_m    bigint NOT NULL DEFAULT 0,
    enabled                    boolean NOT NULL DEFAULT true,
    PRIMARY KEY (tenant_id, model_name)
);

CREATE TABLE billing_ledger (
    id            text PRIMARY KEY,
    tenant_id     text NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id       text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    request_id    text NOT NULL,
    model         text NOT NULL,
    input_tokens  int NOT NULL DEFAULT 0,
    output_tokens int NOT NULL DEFAULT 0,
    cost_cents    bigint NOT NULL DEFAULT 0,
    price_cents   bigint NOT NULL DEFAULT 0,
    margin_cents  bigint NOT NULL DEFAULT 0,
    type          text NOT NULL,
    balance_after bigint NOT NULL DEFAULT 0,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_ledger_tenant_time ON billing_ledger(tenant_id, created_at DESC);
CREATE INDEX idx_ledger_user_time ON billing_ledger(user_id, created_at DESC);

CREATE TABLE usage_records (
    id            text PRIMARY KEY,
    tenant_id     text NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id       text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    request_id    text NOT NULL,
    model         text NOT NULL,
    provider      text NOT NULL,
    channel_id    text NOT NULL,
    input_tokens  int NOT NULL DEFAULT 0,
    output_tokens int NOT NULL DEFAULT 0,
    latency_ms    int NOT NULL DEFAULT 0,
    status        text NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_usage_tenant_time ON usage_records(tenant_id, created_at DESC);
CREATE INDEX idx_usage_user_time ON usage_records(user_id, created_at DESC);

CREATE TABLE recharges (
    id           text PRIMARY KEY,
    tenant_id    text NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id      text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    amount_cents bigint NOT NULL,
    status       text NOT NULL DEFAULT 'pending',
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE files (
    id          text PRIMARY KEY,
    tenant_id   text NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id     text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    filename    text NOT NULL,
    mime_type   text NOT NULL,
    size        bigint NOT NULL,
    storage_url text NOT NULL,
    purpose     text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_files_tenant ON files(tenant_id);

CREATE TABLE audit_logs (
    id         text PRIMARY KEY,
    actor_id   text,
    action     text NOT NULL,
    target     text,
    payload    jsonb,
    ip         text,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_time ON audit_logs(created_at DESC);
