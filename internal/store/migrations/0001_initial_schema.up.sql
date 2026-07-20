-- 0001_initial_schema: 初始全量 schema(v0.2.0 合并版)。
--
-- 历史:本项目此前以 0001..0023 共 23 个增量迁移演进。鉴于尚无线上用户与历史数据,
-- v0.2.0 起将全部历史迁移合并为单一初始 schema,降低新用户认知负担(一条 migrate up 即建库)。
-- 合并方式: 对"全量 apply 后的真实库"执行 pg_dump --schema-only,清洗 psql 元命令后保留纯 DDL。
-- 未来版本从 0002 起继续增量迁移;schema_migrations 由 MigrateUp 运行器自建并登记版本。
--
-- 表(16): tenants/users/api_keys/channels/channel_models/models/tenant_model_overrides/
--         billing_ledger/pending_charges/recharges/payment_orders/usage_records/
--         request_logs/invite_tokens/audit_logs/files


CREATE TABLE public.api_keys (
    id text NOT NULL,
    tenant_id text NOT NULL,
    user_id text NOT NULL,
    key_prefix text NOT NULL,
    key_hash text NOT NULL,
    name text NOT NULL,
    scopes text[] DEFAULT '{}'::text[] NOT NULL,
    models text[] DEFAULT '{}'::text[] NOT NULL,
    rpm_limit integer DEFAULT 0 NOT NULL,
    tpm_limit integer DEFAULT 0 NOT NULL,
    expires_at timestamp with time zone,
    last_used_at timestamp with time zone,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    ip_whitelist text[] DEFAULT '{}'::text[] NOT NULL,
    daily_request_limit integer DEFAULT 0 NOT NULL,
    monthly_request_limit integer DEFAULT 0 NOT NULL,
    daily_token_limit integer DEFAULT 0 NOT NULL,
    monthly_token_limit integer DEFAULT 0 NOT NULL
);

CREATE TABLE public.audit_logs (
    id text NOT NULL,
    actor_id text,
    action text NOT NULL,
    target text,
    payload jsonb,
    ip text,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.billing_ledger (
    id text NOT NULL,
    tenant_id text NOT NULL,
    user_id text NOT NULL,
    request_id text NOT NULL,
    model text NOT NULL,
    input_tokens integer DEFAULT 0 NOT NULL,
    output_tokens integer DEFAULT 0 NOT NULL,
    cost_cents bigint DEFAULT 0 NOT NULL,
    price_cents bigint DEFAULT 0 NOT NULL,
    margin_cents bigint DEFAULT 0 NOT NULL,
    type text NOT NULL,
    balance_after bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.channel_models (
    id text NOT NULL,
    channel_id text NOT NULL,
    model_name text NOT NULL,
    upstream_model text DEFAULT ''::text NOT NULL,
    input_cost_cents_per_m bigint DEFAULT 0 NOT NULL,
    output_cost_cents_per_m bigint DEFAULT 0 NOT NULL,
    cache_read_cost_cents_per_m bigint DEFAULT 0 NOT NULL,
    cache_write_cost_cents_per_m bigint DEFAULT 0 NOT NULL,
    weight integer DEFAULT 1 NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.channels (
    id text NOT NULL,
    tenant_id text,
    provider text NOT NULL,
    name text NOT NULL,
    base_url text DEFAULT ''::text NOT NULL,
    api_key_enc text DEFAULT ''::text NOT NULL,
    priority integer DEFAULT 0 NOT NULL,
    weight integer DEFAULT 1 NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    input_cost_cents_per_m bigint DEFAULT 0 NOT NULL,
    output_cost_cents_per_m bigint DEFAULT 0 NOT NULL
);

CREATE TABLE public.files (
    id text NOT NULL,
    tenant_id text NOT NULL,
    user_id text NOT NULL,
    filename text NOT NULL,
    mime_type text NOT NULL,
    size bigint NOT NULL,
    storage_url text NOT NULL,
    purpose text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.invite_tokens (
    id text NOT NULL,
    token_hash text NOT NULL,
    tenant_id text NOT NULL,
    role text DEFAULT 'member'::text NOT NULL,
    created_by text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    used_at timestamp with time zone,
    used_by text,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.models (
    model_name text NOT NULL,
    input_price_cents_per_m bigint DEFAULT 0 NOT NULL,
    output_price_cents_per_m bigint DEFAULT 0 NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    description text DEFAULT ''::text NOT NULL,
    long_desc text DEFAULT ''::text NOT NULL,
    tags text[] DEFAULT '{}'::text[] NOT NULL,
    context_length integer DEFAULT 0 NOT NULL,
    routing_strategy text DEFAULT 'weighted'::text NOT NULL,
    pinned_channel_id text,
    capabilities jsonb DEFAULT '["text"]'::jsonb NOT NULL,
    cache_read_price_cents_per_m bigint DEFAULT 0 NOT NULL,
    cache_write_price_cents_per_m bigint DEFAULT 0 NOT NULL,
    providers text[] DEFAULT '{}'::text[] NOT NULL
);

CREATE TABLE public.payment_orders (
    id text NOT NULL,
    tenant_id text NOT NULL,
    user_id text NOT NULL,
    out_trade_no text NOT NULL,
    provider text NOT NULL,
    amount_cents bigint NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    prepay_data text,
    transaction_id text,
    paid_at timestamp with time zone,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.pending_charges (
    id text NOT NULL,
    tenant_id text NOT NULL,
    user_id text NOT NULL,
    request_id text NOT NULL,
    model text NOT NULL,
    input_tokens integer DEFAULT 0 NOT NULL,
    output_tokens integer DEFAULT 0 NOT NULL,
    cache_read_tokens integer DEFAULT 0 NOT NULL,
    cache_write_tokens integer DEFAULT 0 NOT NULL,
    price_cents bigint DEFAULT 0 NOT NULL,
    cost_cents bigint DEFAULT 0 NOT NULL,
    attempts integer DEFAULT 0 NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    last_error text,
    next_retry_at timestamp with time zone DEFAULT now() NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.recharges (
    id text NOT NULL,
    tenant_id text NOT NULL,
    user_id text NOT NULL,
    amount_cents bigint NOT NULL,
    status text DEFAULT 'pending'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.request_logs (
    id text NOT NULL,
    request_id text NOT NULL,
    tenant_id text DEFAULT ''::text NOT NULL,
    user_id text DEFAULT ''::text NOT NULL,
    api_key_id text DEFAULT ''::text NOT NULL,
    model text,
    provider text,
    channel_id text,
    method text,
    path text,
    status integer,
    latency_ms integer,
    input_tokens integer,
    output_tokens integer,
    price_cents bigint,
    request_body text,
    response_body text,
    error text,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.tenant_model_overrides (
    tenant_id text NOT NULL,
    model_name text NOT NULL,
    input_price_cents_per_m bigint NOT NULL,
    output_price_cents_per_m bigint NOT NULL,
    enabled boolean DEFAULT true NOT NULL
);

CREATE TABLE public.tenants (
    id text NOT NULL,
    name text NOT NULL,
    slug text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE public.usage_records (
    id text NOT NULL,
    tenant_id text NOT NULL,
    user_id text NOT NULL,
    request_id text NOT NULL,
    model text NOT NULL,
    provider text NOT NULL,
    channel_id text NOT NULL,
    input_tokens integer DEFAULT 0 NOT NULL,
    output_tokens integer DEFAULT 0 NOT NULL,
    latency_ms integer DEFAULT 0 NOT NULL,
    status text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    api_key_id text DEFAULT ''::text NOT NULL,
    api_key_name text DEFAULT ''::text NOT NULL,
    price_cents bigint DEFAULT 0 NOT NULL,
    cost_cents bigint DEFAULT 0 NOT NULL,
    error_message text DEFAULT ''::text NOT NULL
);

CREATE TABLE public.users (
    id text NOT NULL,
    tenant_id text NOT NULL,
    email text NOT NULL,
    password_hash text NOT NULL,
    role text DEFAULT 'member'::text NOT NULL,
    status text DEFAULT 'active'::text NOT NULL,
    balance_cents bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT users_balance_nonnegative CHECK ((balance_cents >= 0))
);

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_key_hash_key UNIQUE (key_hash);

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.audit_logs
    ADD CONSTRAINT audit_logs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.billing_ledger
    ADD CONSTRAINT billing_ledger_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.channel_models
    ADD CONSTRAINT channel_models_channel_id_model_name_key UNIQUE (channel_id, model_name);

ALTER TABLE ONLY public.channel_models
    ADD CONSTRAINT channel_models_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.channels
    ADD CONSTRAINT channels_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.files
    ADD CONSTRAINT files_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.invite_tokens
    ADD CONSTRAINT invite_tokens_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.invite_tokens
    ADD CONSTRAINT invite_tokens_token_hash_key UNIQUE (token_hash);

ALTER TABLE ONLY public.models
    ADD CONSTRAINT models_pkey PRIMARY KEY (model_name);

ALTER TABLE ONLY public.payment_orders
    ADD CONSTRAINT payment_orders_out_trade_no_key UNIQUE (out_trade_no);

ALTER TABLE ONLY public.payment_orders
    ADD CONSTRAINT payment_orders_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.pending_charges
    ADD CONSTRAINT pending_charges_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.recharges
    ADD CONSTRAINT recharges_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.request_logs
    ADD CONSTRAINT request_logs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.tenant_model_overrides
    ADD CONSTRAINT tenant_model_overrides_pkey PRIMARY KEY (tenant_id, model_name);

ALTER TABLE ONLY public.tenants
    ADD CONSTRAINT tenants_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.tenants
    ADD CONSTRAINT tenants_slug_key UNIQUE (slug);

ALTER TABLE ONLY public.usage_records
    ADD CONSTRAINT usage_records_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_tenant_id_email_key UNIQUE (tenant_id, email);

CREATE INDEX idx_api_keys_hash ON public.api_keys USING btree (key_hash);

CREATE INDEX idx_api_keys_tenant ON public.api_keys USING btree (tenant_id);

CREATE INDEX idx_audit_time ON public.audit_logs USING btree (created_at DESC);

CREATE INDEX idx_channel_models_channel ON public.channel_models USING btree (channel_id);

CREATE INDEX idx_channel_models_lookup ON public.channel_models USING btree (model_name, status);

CREATE INDEX idx_channels_provider ON public.channels USING btree (provider);

CREATE INDEX idx_channels_tenant ON public.channels USING btree (tenant_id);

CREATE INDEX idx_files_tenant ON public.files USING btree (tenant_id);

CREATE INDEX idx_invite_tokens_tenant ON public.invite_tokens USING btree (tenant_id, created_at DESC);

CREATE INDEX idx_ledger_tenant_time ON public.billing_ledger USING btree (tenant_id, created_at DESC);

CREATE INDEX idx_ledger_user_time ON public.billing_ledger USING btree (user_id, created_at DESC);

CREATE INDEX idx_payment_orders_expire ON public.payment_orders USING btree (status, expires_at);

CREATE INDEX idx_payment_orders_user ON public.payment_orders USING btree (user_id, created_at DESC);

CREATE INDEX idx_pending_charges_retry ON public.pending_charges USING btree (status, next_retry_at);

CREATE INDEX idx_reqlog_key_time ON public.request_logs USING btree (api_key_id, created_at DESC);

CREATE INDEX idx_reqlog_reqid ON public.request_logs USING btree (request_id);

CREATE INDEX idx_reqlog_tenant_time ON public.request_logs USING btree (tenant_id, created_at DESC);

CREATE INDEX idx_usage_key_time ON public.usage_records USING btree (api_key_id, created_at DESC);

CREATE INDEX idx_usage_model_time ON public.usage_records USING btree (model, created_at DESC);

CREATE INDEX idx_usage_provider_time ON public.usage_records USING btree (provider, created_at DESC);

CREATE INDEX idx_usage_tenant_time ON public.usage_records USING btree (tenant_id, created_at DESC);

CREATE INDEX idx_usage_user_time ON public.usage_records USING btree (user_id, created_at DESC);

CREATE INDEX idx_users_tenant ON public.users USING btree (tenant_id);

CREATE UNIQUE INDEX uniq_ledger_usage_request ON public.billing_ledger USING btree (request_id) WHERE (type = 'usage'::text);

CREATE UNIQUE INDEX uniq_payment_orders_provider_txn ON public.payment_orders USING btree (provider, transaction_id) WHERE ((transaction_id IS NOT NULL) AND (transaction_id <> ''::text) AND (status = 'paid'::text));

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.api_keys
    ADD CONSTRAINT api_keys_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.billing_ledger
    ADD CONSTRAINT billing_ledger_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.billing_ledger
    ADD CONSTRAINT billing_ledger_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.channel_models
    ADD CONSTRAINT channel_models_channel_id_fkey FOREIGN KEY (channel_id) REFERENCES public.channels(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.channels
    ADD CONSTRAINT channels_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.files
    ADD CONSTRAINT files_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.files
    ADD CONSTRAINT files_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.invite_tokens
    ADD CONSTRAINT invite_tokens_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.recharges
    ADD CONSTRAINT recharges_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.recharges
    ADD CONSTRAINT recharges_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.tenant_model_overrides
    ADD CONSTRAINT tenant_model_overrides_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.usage_records
    ADD CONSTRAINT usage_records_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.usage_records
    ADD CONSTRAINT usage_records_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_tenant_id_fkey FOREIGN KEY (tenant_id) REFERENCES public.tenants(id) ON DELETE CASCADE;

-- redeem_codes: 充值兑换码(卡密)。无外键依赖(used_by_user_id 仅记录,不加 FK 以免影响用户删除)。
-- 原为增量迁移 0002,因项目尚无线上用户,合并入初始 schema 保持单一基线(v0.2.0 决策)。
CREATE TABLE public.redeem_codes (
    id text NOT NULL,
    code text NOT NULL,
    amount_cents bigint NOT NULL,
    status text NOT NULL DEFAULT 'active'::text,
    note text NOT NULL DEFAULT ''::text,
    used_by_user_id text,
    used_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    expires_at timestamp with time zone
);
CREATE UNIQUE INDEX uniq_redeem_codes_code ON public.redeem_codes USING btree (code);

