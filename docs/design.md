# LLM Gateway 设计规格

- 日期: 2026-07-13
- 状态: 已批准（方案 C）
- 技术栈: Go (Gin) + Postgres + Redis + Vue 3 + Naive UI

## 1. 目标与范围

构建一个多租户 LLM 网关，统一对接百练、火山方舟、百度千帆等国内供应商，对外同时提供 **OpenAI 兼容**与 **Anthropic 兼容**两套 API，支持文本与多模态（图像/音频/文件），具备预付余额计费、API Key 自助管理、用量仪表盘与多轮聊天台。

参考产品：OpenRouter（路由与定价）、OneAPI/NewAPI（渠道管理）、智谱开放平台（用户端 UI 风格）。

### 范围内
- 双协议 ingress（OpenAI `/v1/chat/completions`、Anthropic `/v1/messages`）及 embeddings、models 列表
- 多模态：OpenAI content-parts 为内部 canonical + 文件托管层（`/v1/files`）
- 供应商适配器：百练、火山方舟、千帆，开发期提供 **Mock Provider**
- 渠道（channel）管理：平台默认渠道池 + 租户 BYOK 覆盖
- 计费：预付余额 + 按 token 实时扣费（流式累计）
- 多租户 + 终端用户自助：注册、登录、聊天台、API Key、用量、充值
- 管理端：租户/用户/渠道/模型定价/计费/审计

### 范围外（YAGNI，后续再说）
- 集群高可用、消息队列异步计费（架构预留扩展点，但不实现）
- 后付费账单周期
- Prompt 模板库、A/B 测试、对话历史持久化产品化（仅聊天台内存级会话）
- 函数调用（tool use）的跨供应商深度归一——做基本透传，不保证全供应商语义一致

## 2. 架构总览

六边形/端口适配器架构。核心域以 OpenAI 格式为 canonical。

```
                    ┌─────────────────────────────────────────┐
  OpenAI client ──► │ Ingress: OpenAI Controller              │
                    │   (canonicalize request)                │
  Anthropic client► │ Ingress: Anthropic Controller           │
                    │   (canonicalize request)                 │
                    └───────────────┬─────────────────────────┘
                                    ▼
                    ┌─────────────────────────────────────────┐
                    │ Core: ChatService / EmbeddingService    │
                    │  - 鉴权(API Key→租户/用户)              │
                    │  - 模型路由(模型→渠道组)                │
                    │  - 限流/配额(Redis)                     │
                    │  - 预扣费/实扣费                        │
                    │  - 文件托管解析                         │
                    └───────────────┬─────────────────────────┘
                                    ▼
                    ┌─────────────────────────────────────────┐
                    │ Provider Port (interface)               │
                    │   Chat / Embed / Models / Multimodal    │
                    └──┬──────────┬──────────┬──────────┬─────┘
                       ▼          ▼          ▼          ▼
                   Bailian   VolcArk    Qianfan     MockProvider
                   adapter   adapter    adapter     (开发/测试)
```

### 关键设计原则
- **canonical = OpenAI 格式**：入口两侧都转成 OpenAI `ChatCompletionRequest`；出口 adapter 从 OpenAI 格式转供应商原生格式；响应原路转回。Anthropic 输出格式由 egress adapter 从 canonical 一次性生成。
- **Provider 接口最小且稳定**：只暴露 `Chat(ctx, *Req) (*Resp, error)` 与 `ChatStream`、`Embed`、`Models`。供应商差异封装在 adapter 内。
- **Channel 是调度单位**：一个供应商可配多个 channel（不同密钥、优先级、权重、价格覆盖）。模型→渠道组→负载均衡/故障转移。
- **密钥混合**：解析顺序 = 租户 BYOK channel（若配置且启用）> 平台默认 channel 池。
- **计费同步但幂等**：每请求生成 `request_id`，预扣（按估算上限）→ 实际 token 回来后差额回补，写 `billing_ledger`。流式按 chunk 累计 usage，结束时一次性结算。

## 3. 模块划分

后端 Go（`cmd/gateway` + `internal/`）：

| 包 | 职责 |
|---|---|
| `internal/domain` | 领域模型（Tenant/User/ApiKey/Channel/Model/Pricing/Billing） |
| `internal/store` | Postgres 仓储 + 迁移（golang-migrate） |
| `internal/auth` | JWT（Web）+ API Key 鉴权中间件 |
| `internal/api/openai` | OpenAI 兼容 ingress controller + egress |
| `internal/api/anthropic` | Anthropic 兼容 ingress controller + egress |
| `internal/api/web` | 用户端/管理端 REST API（管理实体、聊天、用量、充值） |
| `internal/provider` | Provider 接口 + bailian/volcark/qianfan/mock 实现 |
| `internal/relay` | Core ChatService/EmbeddingService：路由、限流、计费编排 |
| `internal/billing` | 余额、定价、ledger、实时扣费 |
| `internal/files` | 文件托管（本地存储，接口可换 S3/MinIO） |
| `internal/middleware` | 限流、日志、recover、CORS、租户上下文 |
| `internal/config` | 配置加载（env + yaml） |

前端 Vue 3（`web/user`、`web/admin`，独立 SPA 或 monorepo）：
- 用户端：登录/注册、聊天台（流式 SSE）、API Key 管理、用量仪表盘、充值
- 管理端：仪表盘、租户/用户、渠道、模型定价、计费审计、系统配置

## 4. 数据模型（Postgres）

核心表（简述，详细 schema 在迁移文件）：

- `tenants(id, name, slug, status, created_at)`
- `users(id, tenant_id, email, password_hash, role, status, balance_cents, created_at)` — role: admin/member；余额以分为单位整数，避免浮点
- `api_keys(id, tenant_id, user_id, key_prefix, key_hash, name, scopes, models, rpm_limit, tpm_limit, expires_at, last_used_at, created_at)` — 仅存 hash，明文只在创建时返回一次，前缀用于展示
- `channels(id, tenant_id NULL, provider, base_url, api_key_enc, models, priority, weight, status, created_at)` — tenant_id NULL = 平台默认
- `models(id, model_name, provider, channel_ids, input_price_cents_per_m, output_price_cents_per_m, cache_price..., enabled)` — 价格分/百万 token
- `tenant_model_overrides(tenant_id, model_name, input_price..., output_price..., enabled)` — 租户级定价覆盖
- `billing_ledger(id, tenant_id, user_id, request_id, model, input_tokens, output_tokens, cost_cents, price_cents, margin_cents, type[recharge|usage|refund], balance_after, created_at)`
- `usage_records(id, tenant_id, user_id, request_id, model, provider, channel_id, input_tokens, output_tokens, latency_ms, status, created_at)` — 用量明细
- `recharges(id, tenant_id, user_id, amount_cents, status, created_at)`
- `files(id, tenant_id, user_id, filename, mime, size, storage_url, purpose, created_at)`
- `audit_logs(id, actor_id, action, target, payload, ip, created_at)`

金额一律 `bigint` 分；token 计数 `int`；时间 `timestamptz`。

## 5. 关键流程

### 5.1 API 请求处理（以 OpenAI chat 为例）
1. 中间件解析 `Authorization: Bearer sk-...` → 查 `api_keys` 缓存（Redis）→ 注入 tenant/user 上下文。
2. OpenAI ingress 校验 + canonicalize 为内部 `ChatRequest`（OpenAI 格式，多模态 content-parts 解析 file_id→URL）。
3. `ChatService.Chat`：
   a. 模型路由：`model` → 启用渠道组（租户 BYOK 优先，否则平台默认池），按 priority+weight 选一个 channel。
   b. 配额检查：余额>0、RPM/TPM（Redis 滑动窗口）。
   c. 预扣：按 max_tokens 估算上限预扣到冻结额度（内存或 Redis 暂存）。
   d. 调 `provider.Chat/ChatStream`，传 channel 的 base_url + 解密 api_key。
   e. 拿到 usage（input/output tokens），按定价算实价，差额回补余额，写 ledger + usage_record。
4. Egress：canonical response → OpenAI 格式（直通）或 Anthropic 格式（按入口标记转换）。
5. 流式：SSE chunk 透传；累计 usage；结束帧结算。

### 5.2 多模态
- 入口接受 OpenAI `image_url`（http(s) URL 或 `data:` base64 或 `file_id:` 引用）与 `input_audio`。
- 文件托管：`POST /v1/files` 上传 → 返回 `file_id`；内部存本地 `data/files/<tenant>/<uuid>`，签名 URL 供 provider 拉取或转 base64 注入。
- Adapter 负责把 canonical 的图片/音频转成供应商原生字段（百练/方舟/千帆各自格式不同）。
- Anthropic 入口的 `image` content block 转 canonical `image_url`。

### 5.3 计费
- 价格查找：`tenant_model_overrides` > `models` 表。input/output 分别计价。
- 流式：每个 chunk 若带 usage 则累加；若无则结束时 provider 给的最终 usage；极端无 usage 时按字符估算兜底。
- 余额不足：非流式直接 402；流式中途 402 中断并退款已生成部分（按已计 token）。
- 退款/补偿：`billing_ledger.type=refund`，带原 `request_id`。

### 5.4 双协议差异处理
- Anthropic `messages` 入口 → canonical：system 抽出、messages 内容块转 OpenAI content-parts、`max_tokens` 必填。
- 响应：若入口是 Anthropic，egress 把 OpenAI `choices[0].message` 转 Anthropic `content` blocks、`stop_reason` 映射、`usage` 字段对齐。流式 event 类型对齐（`message_start`/`content_block_delta`/`message_delta`/`message_stop`）。
- 工具调用：基本透传 canonical 的 `tools`/`tool_calls`，adapter 尽力映射，不保证全供应商一致（文档标注限制）。

## 6. 鉴权与安全
- Web：邮箱+密码（bcrypt），JWT（access 15m + refresh 7d，Redis 存 refresh）。
- API：`sk-` 前缀明文 Key 仅创建时返回；存 Argon2id hash；前 8 位明文存 `key_prefix` 供识别。Key 限模型、限速、可吊销。
- 渠道密钥：AES-GCM 加密存库，主密钥从环境变量。
- 多租户隔离：所有查询带 `tenant_id` 范围；管理端跨租户需 admin 角色。
- 速率限制：Redis 令牌桶/滑动窗口，按 API Key + 模型。

## 7. Mock 供应商
`MockProvider` 实现 Provider 接口：
- 返回固定/回显式响应，支持流式 SSE。
- 可配置延迟、错误率、usage 量，用于压测与计费验证。
- 支持多模态回显（返回对输入图片的描述占位）。
- 开发期作为默认渠道，`make dev` 一键起服务 + Mock。

## 8. 测试策略
- 单元：domain/billing/pricing/adapter 转换函数，表驱动。
- 适配器：用 `httptest` mock 供应商上游，验证 canonical↔native 转换与流式。
- 集成：起 Postgres+Redis（docker-compose），端到端跑 OpenAI/Anthropic ingress → MockProvider → 计费 → ledger 校验。
- 双协议一致性：同一 canonical 请求分别走两入口，断言语义等价。
- 计费正确性：流式/非流式、余额不足中断、退款、租户定价覆盖的金额断言。
- 前端：Vitest 组件测 + Playwright 关键路径（登录、聊天、发 Key）。

## 9. 前端

技术：Vue 3 + Vite + TypeScript + Pinia + Vue Router + Naive UI + axios。SSE 用原生 `fetch`+ReadableStream。

### 用户端（参考智谱开放平台）
- 顶栏：Logo、模型选择器、余额、用户菜单。
- 聊天台：左侧会话列表（内存级）、中间对话区（流式渲染、Markdown、代码高亮、多模态预览）、底部输入框（支持上传图片/文件、模型切换、参数调节）。
- API Keys：列表、创建（仅展示一次明文）、删除、限速配置。
- 用量仪表盘：折线图（调用量/token/费用）、按模型/按日聚合、明细表。
- 充值：模拟支付（开发期直接加余额）。

### 管理端
- 仪表盘：总调用、总收入、活跃租户、渠道健康。
- 租户与用户管理、渠道管理（增删改、启停、测试连通性）、模型与定价、计费审计、审计日志、系统配置。

## 10. 部署与开发
- `docker-compose.yml`：postgres、redis、gateway、user-web、admin-web。
- `Makefile`：`dev`/`build`/`test`/`lint`/`migrate`/`seed`（mock 数据）。
- 配置：`config.yaml` + 环境变量覆盖。
- 国内源：Go `GOPROXY=https://goproxy.cn,direct`；npm `--registry=https://registry.npmmirror.com`。
- README、LICENSE（MIT）、CONTRIBUTING、Dockerfile。

## 11. 分阶段交付（实现规划锚点）
1. 骨架：项目结构、配置、Postgres 迁移、Mock Provider、OpenAI ingress 非流式 + 计费闭环 + 集成测试。
2. 流式 + Anthropic ingress/egress + 双协议一致性测试。
3. 多模态 + 文件托管。
4. 真实 adapter：百练、火山方舟、千帆。
5. 渠道管理 + 租户 BYOK + 负载均衡/故障转移。
6. 用户端 Vue（聊天台、Key、用量、充值）。
7. 管理端 Vue。
8. 文档、Docker、开源化打磨。
