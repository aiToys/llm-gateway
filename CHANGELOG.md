# 更新日志

本项目遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 格式,
版本号遵循 [语义化版本](https://semver.org/lang/zh-CN/)。

## [Unreleased]

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

[Unreleased]: https://github.com/aitoys/llm-gateway/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/aitoys/llm-gateway/releases/tag/v0.1.0
