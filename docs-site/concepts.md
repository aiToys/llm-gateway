---
title: 核心概念
---

# 核心概念

理解 LLM Gateway 只需抓住三个对象：**模型**、**渠道**、**供应商**，外加把它们串起来的 **channel_models**（每个渠道 × 模型一行，承载上游映射、成本、启停与权重）。本页把它们讲透。

## 一张图的关系

```
                    逻辑模型 (Model)                       对外暴露给 SDK 的名字
                    gpt-4o / glm-4-plus ...
                           │
                           │ 一个模型挂多个渠道
                           ▼
              ┌────────────┴────────────┐
              ▼                         ▼
        渠道 (Channel)              渠道 (Channel)
        provider=bailian            provider=volces
        upstream: glm-4-plus        upstream: doubao-pro
        成本: 1.0/3.0 分/百万        成本: 0.9/2.8 分/百万
        priority=1, weight=5        priority=1, weight=5
              │                         │
              ▼                         ▼
        供应商 (Provider)          供应商 (Provider)
        百练 · 火山方舟 · 千帆 · mock
```

一句话：**模型是「卖什么」，渠道是「从谁那进货」，供应商是「上游是谁」。**

## 模型 vs 渠道 vs 供应商

| 维度 | 模型 (Model) | 渠道 (Channel) | 供应商 (Provider) |
| --- | --- | --- | --- |
| 是什么 | 对外的逻辑名 | 供应商的一个接入实例 | 上游协议适配器 |
| 谁面向 | 调用方 SDK | 运营/管理员 | 网关内部 |
| 关键字段 | `name`、`input/output_price_cents_per_m`、`capabilities`、`context_length`、`routing_strategy`、`enabled` | `provider`、`key`、`priority`、`weight`、`tenant_id` + `channel_models`（每模型的上游映射/成本/启停/权重） | `bailian` / `volces` / `qianfan` / `mock` |
| 数量关系 | 一个模型 | 一个模型下挂多个渠道 | 一个渠道属于一个供应商 |

### 为什么模型没有 `provider` 字段？

因为同一个逻辑模型可以由**多个供应商**供给。例如 `gpt-4o` 这个名字，可能同时通过百练的 `gpt-4o`、火山方舟的代理、千帆的兼容端点接入。把 `provider` 钉死在模型上就丧失了多供应商负载均衡的能力。所以归属关系是反过来的：**渠道引用模型，并声明自己属于哪个供应商**——模型本身对供应商无感。

### 为什么成本（cost）在渠道×模型上，而不在模型上？

不同供应商给我们的**进价**不同：百练的 `glm-4-plus` 可能 1.0 分/百万 input，火山代理的同款可能 0.9 分。进价是渠道属性，而且**同一渠道的不同模型进价也不同**。所以我们把成本挂在 `channel_models`（每个渠道×模型一行），而非模型本身。而我们面向用户的**售价**则希望统一（用户体验：调 `glm-4-plus` 就是这个价），所以售价绑定在模型上。

> 售价归模型，成本归 channel_models。毛利 = 售价（模型） − 成本（实际命中的 channel_models 行）。系统按真实命中的渠道×模型核算成本，因此能精确到分地告诉你每笔请求赚了多少。

## 售价 vs 成本

| | 售价 (Price) | 成本 (Cost) |
| --- | --- | --- |
| 绑定对象 | **模型** | **channel_models（渠道 × 模型）** |
| 字段 | `input_price_cents_per_m` / `output_price_cents_per_m` | `channel_models.input/output_cost_cents_per_m`（0 回退到渠道级） |
| 单位 | 分（人民币）/ 百万 token | 分 / 百万 token |
| 面向 | 调用方（统一价） | 运营（真实进价） |
| 用途 | 从用户余额扣费 | 核算毛利、渠道选型 |

调用一次扣费 = 命中渠道的实际 token 用量 × 模型售价；同时记录成本 = 同样用量 × 命中 channel_models 行的成本。

## channel_models：把逻辑名翻译成上游真实名（并承载成本）

这是网关的核心翻译层。每个渠道对其承载的每个逻辑模型有一行 `channel_models`，记录上游映射、成本、启停与权重：

```yaml
# 一个百练渠道的 channel_models 示例（每行 = 一个逻辑模型）
- model_name: glm-4-plus            # 逻辑名
  upstream_model: glm-4-plus        # 上游真实名（空 = 与逻辑名相同）
  input_cost_cents_per_m: 100       # 该渠道该模型的进价
  output_cost_cents_per_m: 100
  status: active
- model_name: gpt-4o
  upstream_model: gpt-4o
  input_cost_cents_per_m: 60
  output_cost_cents_per_m: 60
- model_name: embedding-large
  upstream_model: text-embedding-v3
```

一次请求的生命周期：

1. SDK 发 `POST /v1/chat/completions`，body 里 `model: "glm-4-plus"`。
2. 网关在 `channel_models` 中查所有 `model_name="glm-4-plus"` 且 `status=active` 的行，按所属渠道的 `routing_strategy`（加权随机 / 轮询 / 主备 / 随机 / 固定）挑一个**可用**渠道，例如选中了百练渠道。
3. 取该行的 `upstream_model`，拿到上游真实名（这里也是 `glm-4-plus`，也可能是 `doubao-pro-32k` 之类）。
4. 用**该渠道的 provider + key + 上游真实名**向上游发请求。
5. 回包后按模型售价扣费、按该 `channel_models` 行的成本记账。

这样做的好处：

- **上游名变动不波及调用方**：供应商把 `doubao-pro-32k` 改名成 `doubao-1-5-pro-32k`，只需在管理端改该行的 `upstream_model`，SDK 侧零改动。
- **同价同能力模型可互换**：把 `gpt-4o` 同时配到百练 / 火山 / 千帆三家渠道，由路由策略决定实际走谁，自动故障转移。
- **逐模型成本与启停**：同一渠道的不同模型可有不同进价，也可单独停用某个模型而不影响该渠道的其他模型。
- **BYOK（租户自带密钥）天然支持**：租户自建一条同模型渠道、`tenant_id` 指向自己，路由时优先命中。

## 租户与用户

| | 租户 (Tenant) | 用户 (User) |
| --- | --- | --- |
| 是什么 | 计费与隔离的主体 | 登录控制台的账号 |
| 关系 | 1 个租户 : N 个用户 | 属于一个租户 |
| 余额 / 用量 / 账单 | 按租户聚合 | 用户身份驱动操作 |
| 模型可见性 | 租户级开关 | 继承所属租户 |

控制台登录拿的是 **JWT**（Web 端鉴权，账号 → 租户上下文）；推理请求用的是 **API Key**（`sk-` 开头，绑定到具体租户，见下）。

## API Key

- 形如 `sk-demo-key-1234567890`，由网关签发，**只用于推理端点**（`/v1/chat/completions`、`/v1/messages`）。
- 每把 Key 绑定一个**租户**；网关用 Key 解析出租户上下文，再据此做计费、限流、模型可见性判断。
- Web 端登录用的是 JWT，不是 API Key——两套鉴权互不混用。

请求时：

```http
# OpenAI 协议
POST /v1/chat/completions
Authorization: Bearer sk-xxxx

# Anthropic 协议
POST /v1/messages
x-api-key: sk-xxxx
```

## 控制面 vs 数据面

网关在架构上分成两层：

| | 控制面 (Control Plane) | 数据面 (Data Plane / edge) |
| --- | --- | --- |
| 二进制 | `cmd/gateway` | `cmd/edge`（可独立部署） |
| 职责 | 管理端 / 用户端 Web、模型与渠道配置、计费查询 | `/v1` 推理接入、路由、上游转发 |
| 端口 | `server.addr`（默认 `:8088`） | `edge.addr`（留空则**内嵌进控制面同端口**） |
| 状态 | 有状态（连 Postgres/Redis） | 无状态，可横向扩展 |

部署形态：

- **开发 / 单实例**：`edge.addr` 留空，edge 内嵌进 `cmd/gateway`，一个端口全包。
- **生产 / 可扩展**：`edge.addr` 设独立端口（或起多个 `cmd/edge` 副本），公网只暴露 edge；管理端走内网。`cmd/gateway` 与 `cmd/edge` 共享同一份配置与 Postgres/Redis，元数据天然一致。

> edge 无状态 + Redis 协调（限流桶、熔断状态、渠道健康标记），所以加副本就是加机器，无需会话亲和。

## 速查：一次请求会经过哪些概念？

```
SDK (model=gpt-4o, Authorization: sk-xxx)
  │
  │  1. API Key → 租户
  ▼
数据面 (edge)
  │
  │  2. 模型 gpt-4o.enabled & 对该租户可见？
  │  3. 在模型的渠道池里按 routing_strategy 选渠道（命中百练渠道）
  │  4. channel_models[gpt-4o].upstream_model → 上游名
  ▼
供应商 adapter (bailian, OpenAI 兼容)
  │
  │  5. 用渠道 key 发请求
  ▼
上游响应（带 usage）
  │
  │  6. 售价（模型）× usage → 扣租户余额
  │     成本（命中渠道）× usage → 记毛利
  ▼
返回 SDK
```

把这张图记在脑子里，后续读配置项、排查请求异常都会很顺手。
