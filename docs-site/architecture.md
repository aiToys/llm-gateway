---
title: 架构
---

# 架构

LLM Gateway 采用**六边形架构（端口与适配器）**，并按**控制面 / 数据面分离**部署。本文用文字与图说明两条主线：部署形态与请求流。

## 控制面 / 数据面分离

系统有两个入口二进制，职责清晰切开：

- **控制面 `cmd/gateway`**：管理 REST API（`internal/api/web`）、迁移与 seed、计费/审计等后台编排。默认**内嵌数据面**于同一进程同一端口（`/v1/*`、`/files/*` 反向代理到内嵌的 `EdgeEngine`），适合单机或小规模部署。
- **数据面 `cmd/edge`**：仅承担推理接入点（`/v1/*` + `/files/*`），无管理面、不跑迁移。当 `config.edge.standalone = true` 时，`cmd/gateway` 不再内嵌接入点，由独立 `cmd/edge` 二进制承担，可置于负载均衡后**水平扩展**。

```
            ┌──────────────────────────────────────────────┐
            │              控制面 cmd/gateway               │
            │  internal/api/web (REST)  内置迁移 / seed     │
            │  billing · audit · static(SPA)               │
            └───────────────┬──────────────────────────────┘
                            │ edge.standalone
                ┌───────────┴────────────┐
                │ =false(默认)           │ =true
        ┌───────▼────────┐      ┌────────▼─────────┐
        │ 同进程内嵌 edge │      │ 独立 cmd/edge ×N │
        │ /v1 /files 同端口│      │ LB 后水平扩展    │
        └────────────────┘      └──────────────────┘
                 共用 EdgeEngine()（internal/bootstrap）
```

两种形态共用 `Deps.EdgeEngine()`，保证接入点行为完全一致。

## 六边形分层

```
                   驱动适配器（入站）                 │
   openai controller  ─┐                            │
   anthropic controller┘                            │
            │                                        │
            ▼   internal/canon (规范 OpenAI 格式)    │ 应用核心
       internal/relay  ── 路由 / 限流 / 熔断 / 计费   │ （不依赖框架）
            │                                        │
   ────── 端口（接口） ──────                        │
   store.Store  │  provider.Provider  │  billing     │
            │                                        │
            ▼   被驱动适配器（出站）                  │
   pgx + 迁移   │  openaicomp 共享 adapter            │
   internal/store│  mock / 各供应商                    │
```

- **核心**：`relay`（编排）、`billing`（计费）、`canon`（规范格式）、`auth`（主体抽象）。不感知 HTTP 框架与具体供应商。
- **端口**：`store.Store`（持久化）、`provider.Provider`（上游调用）、`billing.Service`（账务）以接口暴露。
- **适配器**：`internal/store`（pgx + 内嵌迁移）、`internal/provider/openaicomp`（OpenAI 兼容协议共享 adapter，复用于百炼/火山方舟/千帆等）、`internal/provider/mock`（测试）。

## Canonical 内部格式

**OpenAI Chat Completions 格式即内部标准**（`internal/canon`）。两个协议入口各自负责协议翻译：

- `internal/api/openai`：OpenAI 原生，直接进出 `canon.Request/Response`。
- `internal/api/anthropic`：Anthropic Messages，双向翻译。

这样核心编排（路由、限流、计费、用量记录）只需对接一种格式，新增协议入口只写翻译层。

## 请求流

```
客户端
  │  POST /v1/chat/completions  (或 /v1/messages)
  ▼
edge /v1
  │  ① middleware.APIKeyAuth   — argon2id 校验 sk- key，Redis 缓存主体(TTL 2m)
  │  ② middleware.RateLimit    — RPM/TPM 滑动分钟桶，超限 429
  ▼
relay.Service.Chat
  │  ③ route()
  │     · EffectivePrice：租户覆盖售价优先，回落到 models 售价
  │     · 渠道选择：按 routing_strategy(weighted/round_robin/
  │       failover/pinned/random) 排序出主备序列
  │     · 熔断过滤：redisBreaker.Allow 过滤已熔断渠道
  │  ④ preflight：余额预检
  │  ⑤ 遍历主备序列：
  │       provider adapter(openaicomp) → 上游
  │       失败 → OnFailure 计入熔断，转下一渠道
  │  ⑥ Billing.Charge：售价(模型) - 成本(渠道) = 毛利，扣余额
  │  ⑦ recordUsage：写 usage_records(provider/channel/价格/成本)
  ▼
canon.Response → 协议入口翻译 → 客户端
```

定价取自模型表（或租户覆盖），成本取自实际命中的渠道——见[数据模型](./data-model)。

## Redis 的角色

Redis 承担所有**跨副本共享的运行时状态**，确保多 edge 副本行为一致：

| 用途 | Key 模式 | 说明 |
| --- | --- | --- |
| API Key 主体缓存 | `apikey:{hash}` | 避免每请求查库算 argon2id，TTL 2 分钟 |
| 限流 RPM/TPM | `rl:rpm:{key}:{minute}`<br>`rl:tpm:{key}:{minute}` | 滑动分钟桶，INCR + 65s 过期 |
| 熔断状态 | `cb:{id}:open`<br>`cb:{id}:fail` | 失败计数窗口衰减；达阈值打开冷却 |
| round_robin 游标 | `rr:{modelName}` | INCR 全局递增取模，跨副本严格轮询 |

**为什么用 Redis 而非进程内存？** 数据面可水平扩展为 N 个 `cmd/edge` 副本。若限流、熔断、轮询游标只存单机内存，则同一 API Key 的 RPM 会被各副本各自计数（实际超出 N 倍）、熔断无法跨副本隔离故障渠道、round_robin 退化为各副本独立轮询。把状态外置到 Redis 后，所有副本共享同一份计数与游标，语义正确。Redis 故障时各环节均设计为**安全降级**（限流放行、熔断放行、round_robin 回退 weighted），避免状态存储不可用导致误杀流量。
