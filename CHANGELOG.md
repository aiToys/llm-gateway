# 更新日志

本项目遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 格式,
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]

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
