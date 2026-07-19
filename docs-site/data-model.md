---
title: 数据模型
---

# 数据模型

Postgres schema 由 `internal/store/migrations/0001~0022` 渐进式迁移定义（含若干次领域模型修正）。本文列出核心表与字段，并解释关键设计决策的**为什么**。

> 金额一律以**分（cents）**整数存储（`*_cents_per_m` = 每百万 token 的分），避免浮点误差。

## 核心表

### models — 模型（逻辑模型，面向用户）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `model_name` | text PK | 逻辑模型名，路由与定价的锚点 |
| `input_price_cents_per_m` | bigint | 售价：输入，每百万 token 分 |
| `output_price_cents_per_m` | bigint | 售价：输出 |
| `enabled` | boolean | 平台级启停 |
| `description` / `long_desc` | text | 模型广场展示文案 |
| `tags` | text[] | 展示标签 |
| `capabilities` | jsonb | **多标签**能力（见决策③） |
| `context_length` | int | 上下文长度 |
| `routing_strategy` | text | `weighted`(默认)/`round_robin`/`failover`/`pinned`/`random` |
| `pinned_channel_id` | text | `pinned` 策略下固定命中的渠道 |
| `providers` | text[] | **展示用**供应商标签（`0018_model_providers`，见决策②） |

**注意**：models 表**没有 `provider` 列，也没有成本列**（见决策①②）。

### channels — 渠道（供应商接入）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | text PK | |
| `tenant_id` | text FK NULL | **NULL = 平台默认渠道**；非 NULL = 租户 BYOK（见决策⑤） |
| `provider` | text | 供应商标识（`bailian`/`volcark`/`qianfan`/…） |
| `name` | text | 渠道名 |
| `base_url` | text | 上游基址 |
| `api_key_enc` | text | AES-GCM 加密的上游 key |
| `priority` | int | 优先级（高者优先） |
| `weight` | int | 同优先级组内加权随机的权重 |
| `input_cost_cents_per_m` | bigint | **渠道级成本回退：输入**（见决策①，模型级成本优先） |
| `output_cost_cents_per_m` | bigint | **渠道级成本回退：输出** |
| `status` | text | `active`/… |

> 渠道承载哪些逻辑模型、每个模型的上游映射与成本，均在 `channel_models` 子表逐行配置（见下）。渠道本身不再持有 `models`/`model_mappings`/`model_costs` 列（已于 `0021_channel_models` 拆出）。

### channel_models — 渠道模型（每个渠道 × 模型一行，承载成本与映射）

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | text PK | `md5(channel_id '/' model_name)`，确定性 ID 便于幂等 |
| `channel_id` | text FK | `ON DELETE CASCADE` 随渠道删除 |
| `model_name` | text | 逻辑模型名（与 `models.model_name` 对齐） |
| `upstream_model` | text | 上游真实模型名（空 = 与 `model_name` 相同） |
| `input_cost_cents_per_m` | bigint | **成本：输入**（0 = 回退到渠道级，见决策①） |
| `output_cost_cents_per_m` | bigint | **成本：输出** |
| `cache_read_cost_cents_per_m` | bigint | 缓存读取成本 |
| `cache_write_cost_cents_per_m` | bigint | 缓存写入成本 |
| `weight` | int | 该模型在渠道内的权重（默认 1，**0 = 继承渠道 `weight`**） |
| `status` | text | `active`/`disabled`，可单独停用某渠道的某模型 |
| | UNIQUE | `(channel_id, model_name)` |

`0021_channel_models` 把原先挤在 `channels` 的三列（`models` text[]、`model_mappings` jsonb、`model_costs` jsonb）标准化为一张子表，使“每个渠道的每个模型”都可独立配置成本/映射/启停/权重，并在管理端以表格逐行编辑（见决策④）。

### tenant_model_overrides — 租户模型覆盖

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `tenant_id` + `model_name` | 复合 PK | |
| `input_price_cents_per_m` | bigint | 覆盖售价 |
| `output_price_cents_per_m` | bigint | 覆盖售价 |
| `enabled` | boolean | **租户级启停**（见决策⑤） |

只管售价与启停，**无成本列**——成本归渠道。

### users — 用户

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | text PK | |
| `tenant_id` | text FK | |
| `email` | text | `(tenant_id, email)` 唯一 |
| `password_hash` | text | Argon2id |
| `role` | text | `admin` / `member` |
| `balance_cents` | bigint | 账户余额（分） |
| `status` | text | `active`/… |

### api_keys — API Key

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | text PK | |
| `tenant_id` / `user_id` | text FK | |
| `key_prefix` | text | 展示用前缀（如 `sk-abc…`） |
| `key_hash` | text UNIQUE | **Argon2id** 哈希（不可逆） |
| `name` | text | 标签 |
| `scopes` | text[] | 权限范围 |
| `models` | text[] | 该 key 允许调用的模型白名单 |
| `rpm_limit` / `tpm_limit` | int | 分钟级限流，0 = 不限 |
| `daily_request_limit` / `monthly_request_limit` | int | 日/月请求数配额，0 = 不限（`0020_apikey_quota`） |
| `daily_token_limit` / `monthly_token_limit` | int | 日/月 token 上限，0 = 不限（字段存在；中间件尚未消费，预留） |
| `ip_whitelist` | text[] | IP 白名单（`0010_apikey_ip_whitelist`，空数组=不限，非空时仅允许列表内 IP/CIDR） |
| `status` | text | `active`/… |

### request_logs — 请求/响应原文日志

`0019_request_logs` 引入，与 `usage_records`（只记元信息）互补，这里存可采样/截断的请求与响应原文，用于排障与合规审计：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | text PK | |
| `request_id` | text | 与 `usage_records`/`billing_ledger` 共享同一 ID |
| `tenant_id` / `user_id` / `api_key_id` | text | 维度，默认 `''` |
| `model` / `provider` / `channel_id` | text | 路由命中信息，可空 |
| `method` / `path` | text | HTTP 方法与路径 |
| `status` | int | HTTP 状态码 |
| `latency_ms` | int | 延迟 |
| `input_tokens` / `output_tokens` | int | 用量 |
| `price_cents` | bigint | 计费金额 |
| `request_body` / `response_body` / `error` | text | 原文/错误（采样或 `log_bodies=false` 时为空） |
| `created_at` | timestamptz | |

### payment_orders — 支付订单

`0014_payment_orders` 引入，承载「下单 → 支付 → 回调入账」全生命周期，与 `recharges`/`billing_ledger` 解耦：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | text PK | |
| `tenant_id` / `user_id` | text | |
| `out_trade_no` | text UNIQUE | 商户订单号（全局唯一），作为幂等键抵御回调重入 |
| `provider` | text | `wechat` / `alipay` / `mock` |
| `amount_cents` | bigint | 订单金额（分） |
| `status` | text | `pending` / `paid` / `closed` |
| `prepay_data` | text | 微信 `code_url` / 支付宝跳转 URL / mock 占位 |
| `transaction_id` | text | 支付平台流水号 |
| `paid_at` / `expires_at` | timestamptz | 入账时间 / 超时关单时间 |
| `created_at` | timestamptz | |

> `0017_payment_txn_unique` + `0022_payment_txn_unique_empty` 在 `(provider, transaction_id)` 上建立部分唯一索引，条件为 `transaction_id IS NOT NULL AND transaction_id <> '' AND status='paid'`，避免空串交易号互相阻塞入账。

### pending_charges — 计费重试队列

`0015_billing_outbox` 引入。relay 在响应完成「之后」才计费（usage 来自完整响应），若计费失败原先只记日志会导致「上游已消费 token、平台已付成本、用户余额却没扣」的漏账。失败的应扣项落库由后台 worker 幂等重试：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | text PK | |
| `tenant_id` / `user_id` | text | |
| `request_id` | text | 幂等键（usage 请求级唯一，形如 `req-<uuid>`） |
| `model` | text | |
| `input_tokens` / `output_tokens` / `cache_read_tokens` / `cache_write_tokens` | int | 用量明细 |
| `price_cents` / `cost_cents` | bigint | 应收/成本 |
| `attempts` | int | 已重试次数 |
| `status` | text | `pending` / `done` / `abandoned` |
| `last_error` | text | 最近一次失败原因 |
| `next_retry_at` / `created_at` | timestamptz | 下次重试 / 入队时间 |

### invite_tokens — 团队邀请令牌

`0016_invite_tokens` 引入。租户管理员生成签名链接，同事凭链接注册即加入该租户；存 hash（明文 token 仅创建时返回一次），复用 API Key 的 hash 范式：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | text PK | |
| `token_hash` | text UNIQUE | token 的哈希值 |
| `tenant_id` | text FK | `ON DELETE CASCADE` |
| `role` | text | 被邀请者角色：`member` / `admin` |
| `created_by` | text | 邀请人 `user_id` |
| `expires_at` | timestamptz | 过期时间 |
| `used_at` / `used_by` | timestamptz / text | 可空：已接受时记录接受时间与接受者 |
| `created_at` | timestamptz | |

### billing_ledger — 计费账本（append-only）

每笔资金流转一条不可变记录。`type` 区分五类账目（见 [计费与账本](./billing)）：

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | text PK | |
| `tenant_id` / `user_id` | text FK | |
| `request_id` | text | 请求 ID（usage 类幂等键） |
| `model` | text | 模型名 |
| `input_tokens` / `output_tokens` | int | 用量 |
| `type` | text | `usage` / `recharge` / `refund` / `transfer` / `adjust` |
| `cost_cents` / `price_cents` / `margin_cents` | bigint | 成本 / 售价 / 毛利（分） |
| `balance_after` | bigint | 本次流转后的余额快照 |
| `created_at` | timestamptz | |

> 在 `billing_ledger(request_id) WHERE type='usage'` 上建有部分唯一索引（`0015_billing_outbox`），保证同一请求的 usage 账目至多一条，是并发幂等计费的闸门。

其余表：`tenants`、`usage_records`（含 `api_key_id`/`provider`/`channel_id`/`price_cents`/`cost_cents`，单表完成按 key/模型/供应商 的多维聚合）、`recharges`、`files`、`audit_logs`（`actor_id`/`action`/`target`/`payload(jsonb)`/`ip`/`created_at`）。

## 设计决策与理由

### ① 售价归模型，成本归渠道

**售价是面向终端用户的统一报价**：一个逻辑模型（如 `gpt-4o`）对用户只有一个价，与上游路由到哪家无关。售价放 `models`，并由 `tenant_model_overrides` 做租户级差价。

**成本随实际命中的上游而变**：同一逻辑模型可由多个供应商渠道承载，各家上游单价不同。若成本挂在模型上，则无法表达“这次走百炼、那次走火山方舟”的成本差异，毛利算不准。`0006_channel_cost` 把成本下沉到渠道侧，`0007` 随即从 `models` 删除成本列；`0021_channel_models` 进一步把成本下沉到 `channel_models`（每个渠道×模型一行），使同一渠道的不同模型可有不同成本。路由命中 `channel_models` 某行时，取该行成本；该行成本为 0 则回退到 `channels` 渠道级成本。

一次请求的毛利 = 售价（来自模型/覆盖）− 成本（来自实际命中的 channel_models 行），三者同记入 `billing_ledger` 与 `usage_records`。

### ② 模型无 `provider` 字段

`0007` 同步删除了 `models.provider`。理由：**一个逻辑模型可由多个供应商渠道承载**，`provider` 是渠道（接入路径）的属性，而非模型的固有属性。把 `provider` 放在模型上会误导（仿佛一个模型只属于一家）。路由层先按 `model_name` 找出所有可承载渠道，再依策略挑一条。

### ③ `capabilities` 多标签替代单值 `modality`

`0008` 把单值 `modality`(text/vision/audio/…) 改为 jsonb 数组 `capabilities`（如 `["text","vision","tool","code","reasoning"]`）。真实世界里**一个模型可同时具备多种能力**（既能看图又能用工具又能推理），单值字段无法表达组合。多标签与智谱/OpenAI 的能力标注一致，也便于模型广场按能力筛选。

### ④ 一个模型 ↔ N 个渠道（经 channel_models）

`models` 与 `channels` 是**多对多**，关联表为 `channel_models`：一个渠道可挂多个逻辑模型（多行），同一逻辑模型也可被多个渠道（不同供应商、或同供应商多 key 池）承载。`channel_models.upstream_model` 允许“逻辑模型名 → 上游真实模型名”的映射，使上游命名差异（如各家对同一基模的不同叫法）对用户无感。

路由时：按 `model_name` 查所有 `channel_models.status=active` 且所属 `channels.status=active` 的渠道 → 按 `priority` 分组 → 熔断过滤 → 按 `routing_strategy` 排出主备序列 → 逐个故障转移。

### ⑤ 租户 BYOK 与租户模型启停

两条独立的租户化路径：

- **BYOK（Bring Your Own Key）**：`channels.tenant_id` 为 NULL 表示**平台默认渠道**（所有租户可用）；非 NULL 表示**该租户自有上游 key 的渠道**，仅本租户路由可见。租户可在管理面录入自己的供应商 key，平台对其按成本计费。
- **租户模型启停与差价**：`tenant_model_overrides.enabled` 可对单个租户关闭某个模型（即便平台级 `models.enabled=true`）；同时可覆盖售价。`relay` 调用 `EffectivePrice(tenantID, modelName)` 时，租户覆盖优先于平台默认。
