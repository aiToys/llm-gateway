# 更新日志

本项目遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 格式,
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]

继 v0.2.0 之后的 5 轮深度优化:逻辑缺陷修复 + 架构清理 + 运营面功能补齐。

### 修复
- **流式中途错误被静默吞掉**: relay 收到上游 StreamError 后直接 return 未投递给控制器,OpenAI 出口发截断的空帧+[DONE]、Anthropic 出口伪造 `end_turn` 成功事件——客户端基于不完整/虚假响应做决策。改为投递错误信号 chunk,OpenAI 发 `error` 事件、Anthropic 发 `event: error`。
- **Anthropic 入口完全绕过 TPM 限流**: `/v1/messages` 非流式未设置 `rl_tokens`,TPM 桶永不被计入;补齐(与 OpenAI 入口一致)。
- **Playground 流式绕过 TPM**: `recordTPM` 在 `APIKeyID==""`(JWT 主体)时早返回,聊天台流式不计 TPM。身份键改为 `APIKeyID || UserID`,与限流中间件一致。
- **流式 TPM 桶键用结束时间**: 跨分钟流式把 token 全计到结束分钟桶,起止分钟统计失真;改用请求开始时间。
- **熔断器半开探测死代码**: `Breaker.Allow`(Redis SetNX 原子抢占探测名额)实现完整但从未被调用,多副本冷却结束瞬间所有副本同时涌入上游反复震荡。在 Chat/Embeddings/ChatStream 调用上游前补 `Allow` 闸门。
- **API Key 缓存命中不复查过期**: 缓存 TTL(2min)内过期的 Key 仍可鉴权。Subject 携带 `ExpiresAt`,缓存命中路径补复查。
- **计费收入指标幂等冲突时重复计数**: `ChargeAtomic` 命中唯一冲突(已计费)时仍触发 `metrics.ObserveCharge`,重试致 revenue 虚高。改为返回 `newlyCharged` 标记,仅真记账才上报。
- **`OrderStatus` 二次查询失败返回 (nil,nil)**: 上层 `o.Status` nil deref 被 Recovery 兜底 500。失败时保留 settle 后的订单对象。
- **`EffectivePrice` 静默吞租户覆盖查询的 DB 错误**: 连接抖动被当作"无覆盖"回退全局价,用户可能按错误价计费。区分 `pgx.ErrNoRows`(回退)与 DB 错误(上抛)。
- **fallback 渠道无 API Key**: `DefaultProvider` 非 mock 时 fallback 到无 key 的真实上游会 401 且反复熔断不存在的渠道。改为仅 mock 才 fallback,否则返 `ErrNoChannel`。

### 变更
- **架构清理**: 删除死代码 `files.Service.Get/FilePath/ErrNotFound` + `store.GetFile` + `middleware.FromContext` 兼容垫片。
- **DRY**: 抽 `common.WriteRelayError` 消除 OpenAI/Anthropic 双 controller 的错误回写重复(~70 行);抽 `store.scanChannelModel` 复用两处 channel_models scan;删前端 `constants.js` 未用 export(`PROVIDER_OPTIONS`/`hasCapability`/`hasModality`)。
- **admin.go 按域拆分**: 759 行单文件拆为 `admin.go`(共享 helpers/模型/账本)+ `admin_tenants.go` + `admin_users.go` + `channel_admin.go`(渠道 CRUD 并入),零逻辑改动。

### 新增
- **充值卡密(兑换码)**: 平台 admin 批量生成卡密(`POST/GET /api/admin/redeem-codes`),用户凭码兑换(`POST /api/redeem`)。`RedeemCodeAtomic` 单事务原子完成"标记 used + 加余额 + 写 recharge 账目",保证账实一致。
- **API Key 过期时间**: `createKey` 收 `expires_at`(中间件复查已生效)。
- **登录/鉴权审计**: login 成功/失败、register、API Key 吊销入 `audit_logs`。
- **前端:兑换码充值 UI**: Recharge 页加"兑换码"输入+兑换(成功后余额局部刷新,无需重拉 /api/me)。
- **前端:暗色模式**: 主题切换按钮(公共页+控制台顶部),localStorage 持久化,默认跟随系统 `prefers-color-scheme`。
- **前端:API Key 过期时间 UI**: Keys 创建表单加 `NDatePicker`(留空=永不过期)。

## [0.2.0] - 2026-07-20

首个正式可用版本(在 v0.1.0 预览版基础上完成深度审计与架构优化)。本项目尚无线上用户,v0.2.0 作为干净的起点。

### 变更(破坏性,仅影响 v0.1.0 部署)

- **迁移合并为单一初始 schema**:历史增量迁移 `0001`~`0023` 合并为 `0001_initial_schema`(由全量库 `pg_dump --schema-only` 生成)。新用户一条 `migrate up` 即建库;未来从 `0002` 起继续增量演进。**v0.1.0 部署无法直接升级**(需重置数据库或保留旧迁移历史)
- **JWT 密钥阈值**:`auth.jwt_secret` 生产要求 ≥32 字节(原 16 字符)。存量部署需更新密钥

### 安全

- **熔断器查询/探测分离**:新增只读 `IsOpen`,`route` 过滤与管理端健康展示改用之;原 `Allow` 在 Redis 实现有半开探测名额副作用,遍历候选渠道会占满探测窗口,多副本下冷却恢复极慢
- **JWT 限流绕过修复**:聊天台(JWT 鉴权、无 API Key)此前完全不受限流,登录用户可无限冲击上游;新增 `Web.Playground` 每用户默认 RPM/TPM,身份键改为 `APIKeyID || UserID`
- **JWT 状态复查**:`JWTAuth` 经 1min Redis 缓存复查用户/租户状态,管理员禁用操作及时生效(平台超管豁免)
- **SSRF 防护**:渠道 `base_url` 入库前校验 scheme(http/https)并拒绝解析到内网/回环/链路本地地址(dev 模式放宽)
- **`ipAllowed` 空指针**:`net.ParseIP` 对异常/污染头返回 nil 会导致 `network.Contains(nil)` panic,鉴权中间件崩溃;补 nil 判断 + IPv6 用 `Equal` 归一

### 修复

- **流式计费兜底**:流转发 goroutine panic 时 `finalize` 不触发,导致计费与 Inflight 双漏;加 `defer finish()`(sync.Once 防双扣)
- **流式 usage 缺失**:OpenAI 兼容上游默认不带 usage 帧,流式请求 0 token 计费(静默漏计);canon 新增 `StreamOptions`,relay 在流式路径强制 `include_usage=true`
- **流式中途错误**:上游中途断流误记 `status=ok`,污染 SLA 与对账;改记 `partial`,OpenAI 出口改为发 `error` 事件而非截断的空帧+[DONE]
- **流式超时硬截断**:`http.Client.Timeout=5min` 涵盖 body 读取阶段,长对话/reasoning 流被默默截断;流式改用专用 client(无整体 Timeout,仅 `ResponseHeaderTimeout`)
- **Anthropic 流式输入 token 丢失**:`message_start` 未解析 `message.usage.input_tokens`,`message_delta.usage` 又不含之,流式 `PromptTokens` 恒为 0 → 漏算输入计费;跨事件缓存合并
- **Anthropic 出口多模态丢失**:`canonContentToAnthropic` 不处理 `[]interface{}` 形态(JSON 绑定产物),图片/音频全丢;`canon.AsParts` 补 `[]interface{}` 归一,出口改用之
- **Anthropic 出口协议补全**:首帧 `role:assistant` 占位;`tool_choice` 字符串简写(`auto`/`none`/`required`);`stop_sequence` → `stop` 映射;`developer`/`function` 角色归一;`max_tokens<1` 防御
- **Anthropic 入口响应缓存 token**:`response.Usage` 补 `cache_read_input_tokens`/`cache_creation_input_tokens`(SDK 计费依赖);入口前置校验 `messages` 非空、`max_tokens>0`
- **缓存 token 双计**:`parseCacheTokens` 三字段相加在代理层合并多源时会重复计数,改取首个非零值
- **关单竞态**:`SettlePaymentAtomic` 原 `WHERE status='pending'`,用户在关单窗口期付款后回调无法入账;允许 `closed→paid`(回调验签/查单为可信付款证据)
- **Embeddings 配额绕过**:`preflight` 收到空 request,估算 token=1,余额/配额预检失效;改为按 input 文本估算
- **错误伪装**:`EffectivePrice` 的 DB 错误原一律映射为 `ErrModelNotFound`,PG 抖动时用户看到"模型不存在";区分 `ErrNotFound`(404)与底层错误(500)

### 变更

- **架构:依赖倒置**:提取 `internal/requestid` 包,`relay` 不再依赖 `middleware`(业务核心不应反向依赖传输层)
- **架构:request_id 统一**:此前存在三套 request_id 机制(logging/middleware/gin key 互不一致),且主引擎漏挂 `RequestID` 中间件导致日志与落库链路 ID 不一致;统一为单一来源
- **`/healthz` 含版本**:返回 `{"ok":true,"version":...}` 便于运维确认部署版本
- **移除死代码**:删除无调用方的 `billing.Refund`(YAGNI)

### 新增

- **Anthropic 出口适配器单测**:覆盖 `canonToAnthropicReq`(developer 归 system / 多模态归一 / tool 结果合并 / max_tokens 防御)、`canonToolChoice`、`stopToFinish`;`canon.AsParts` 补 `[]interface{}` 用例

## [0.1.0] - 2026-07-19

首个开源版本。

### 新增

- **双协议接入**:OpenAI(`/v1/chat/completions`、`/v1/embeddings`)+ Anthropic(`/v1/messages`)兼容,存量 SDK 零改动接入
- **多供应商负载均衡**:加权随机 / 轮询 / 主备 / 随机 / 固定渠道五种策略,自动故障转移,Redis 熔断器(含半开探测)
- **channel_models 独立实体**:每个「渠道 × 模型」可独立配置成本、上游名、权重、启停(取代内嵌 JSON)
- **预付计费**:整数分精度、按 token 实时扣费;`ChargeAtomic` 幂等闸(`INSERT ... ON CONFLICT DO NOTHING`)防并发双扣;五类账本(usage/recharge/refund/transfer/adjust);`pending_charges` 重试队列防漏账
- **多租户 + BYOK**:租户隔离的用量与账单;租户自带密钥(BYOK)整体优先于平台默认渠道;租户可自助启停模型
- **用量配额**:API Key 维度 RPM/TPM + 日/月请求数 + token 上限 + IP 白名单
- **请求/响应原文日志**:可采样、可截断、按保留天数自动清理
- **可观测性**:Prometheus 8 项指标(`/metrics`)+ `slog` 结构化日志 + 审计日志
- **支付集成**:微信支付 Native + 支付宝电脑网站支付 + mock(开发用)
- **控制面 / 数据面分离**:同进程内嵌,或拆分为独立 `edge` 二进制横向扩展
- **Vue 控制台**:管理端 + 用户端开箱即用
- **CI**:golangci-lint v2 + Go 测试矩阵 + 前端构建 + Playwright 端到端测试
- **文档站**:VitePress,14 篇覆盖快速开始 / 核心概念 / 使用 / 运维 / 深入

### 安全

- 资金扣费并发幂等(防 X-Request-Id 客户端可控导致的双扣)
- 5xx 错误脱敏(客户端只收 `internal_error`,详情记服务端日志)
- 请求绑定错误中文化(不暴露 gin validator 内部结构)
- 渠道上游错误透传脱敏

[Unreleased]: https://github.com/aitoys/llm-gateway/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/aitoys/llm-gateway/releases/tag/v0.2.0
[0.1.0]: https://github.com/aitoys/llm-gateway/releases/tag/v0.1.0
