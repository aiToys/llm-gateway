---
title: 多供应商负载均衡
---

# 多供应商负载均衡

一个模型（Model）可挂载多个渠道（Channel），由路由策略决定流量如何在多供应商之间分布。核心代码位于 `internal/relay/relay.go` 的 `selectOrdered`。本页讲清五种策略、主备语义、故障转移，以及一个完整的 `gpt-4o` 三渠道示例。

## 渠道的两个关键字段

| 字段 | 语义 | 取值 |
| --- | --- | --- |
| `priority` | 优先级，**数值越大越优先** | 整数；同值归为同一优先级组 |
| `weight` | 组内权重，用于加权随机 | 正整数；仅在 `weighted` 策略下参与计算 |

主备关系由 `priority` **分组**决定：最高优先级组是「主」，其余组依次为备。组内多个渠道视为副本，可按 `weight` 分摊流量。

## 五种路由策略

| 策略 | 行为 | 适用场景 |
| --- | --- | --- |
| `weighted`（默认） | 最高优先级组内按 `weight` 加权随机；组内剩余渠道与低优先级组共同作为故障转移池 | 通用多副本分摊 + 自动容灾 |
| `round_robin` | 跨副本轮询，Redis 游标 `rr:{model}` 记录位置 | 副本同质、追求绝对均衡 |
| `failover` | 只用最高优先级组的**第一个**作主，其余全部作备；主挂即切 | 明确主备、成本敏感 |
| `random` | 组内纯随机，**忽略权重** | 灰度、压测、无所谓分布 |
| `pinned` | 固定到 `pinned_channel_id`，其余渠道仅在固定渠道不可用时兜底 | 灰度单点、定向排障、A/B |

::: tip 策略在模型上配置
`routing_strategy` 与 `pinned_channel_id` 都是**模型层**字段。在**模型编辑页**即可一站式选择策略、挂载渠道、设置权重与主备，无需在渠道页来回切换。
:::

## 故障转移与熔断

熔断由 `CircuitBreaker` 基于 Redis 实现，状态机如下：

1. 渠道连续 **3 次**失败 → 打开熔断，键 `cb:{id}:open` 写入。
2. 熔断打开 **60 秒**，期间该渠道在 `selectOrdered` 中被过滤，请求自动跳到下一个候选。
3. 60 秒后进入**半开**状态，放行一次试探请求；成功则关闭熔断，失败则重新计时。

失败计数键 `cb:{id}:fail`，打开键 `cb:{id}:open`。**当 Redis 自身故障时，熔断组件 fail-open（放行）**，避免基础设施抖动导致全局不可用。

限流是独立的两道闸：

- RPM：`rl:rpm:{keyid}:{minute}`，`INCR` + `EXPIRE 65s`
- TPM：`rl:tpm:{keyid}:{minute}`，`GET` 预检 → `INCRBY` 实际 token

## 完整示例：gpt-4o 挂三个渠道

假设模型 `gpt-4o` 挂载以下三个渠道：

| 渠道 | provider | priority | weight |
| --- | --- | --- | --- |
| 百炼渠道 | `bailian` | 10 | 5 |
| 火山方舟渠道 | `volcark` | 10 | 3 |
| 千帆渠道 | `qianfan` | 5 | 1 |

优先级分组：`{priority=10}` 为主组（百炼、方舟），`{priority=5}` 为备组（千帆）。

### `weighted`（默认）下的流量分布

主组（priority=10）健康时，全部流量落在主组内，按权重分摊：

- 百炼：`5 / (5+3) = 62.5%`
- 火山方舟：`3 / (5+3) = 37.5%`
- 千帆（priority=5）：`0%`，仅作故障转移

当主组中某渠道被熔断或全部主组渠道不可用时，流量自动落到备组千帆。

### `failover` 下的主备

只认最高优先级组里的**第一个**作主。主组 priority=10 内按定义顺序取第一个（如百炼）为主，方舟、千帆全部作备。主挂 → 切下一个备。

### `pinned` 下的固定渠道

若 `pinned_channel_id` 设为「火山方舟渠道」，则所有流量固定到方舟；只有方舟不可用时才回退到其余渠道兜底。

## 创建渠道示例

```bash
curl -X POST https://gateway.example.com/api/admin/channels \
  -H "Authorization: Bearer <ADMIN_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "百炼-gpt-4o",
    "provider": "bailian",
    "api_key": "<明文KEY，服务端用AES-GCM加密存储>",
    "channel_models": [
      {
        "model_name": "gpt-4o",
        "upstream_model": "qwen-plus-2024",
        "input_cost_cents_per_m": 40,
        "output_cost_cents_per_m": 120
      }
    ],
    "priority": 10,
    "weight": 5
  }'
```

要点：

- `api_key` 传入明文，服务端使用 **AES-GCM** 加密后落库。
- `channel_models` 是该渠道承载的逻辑模型清单（每行一个）：`model_name` 为逻辑名，`upstream_model` 为供应商上游真实名（空 = 同名直通），`input/output_cost_cents_per_m` 为该渠道该模型的**供应商成本单价**（0 回退到渠道级），与模型层售价分离。一个渠道可挂多行。
- `priority` / `weight` 控制渠道调度（高优先级优先，同优先级按权重加权随机）。
- `tenant_id` 留空即平台共享渠道；填入租户 ID 则为该租户的 **BYOK** 专属渠道。

创建好渠道后，回到**模型编辑页**即可一次性把多个渠道挂到 `gpt-4o` 上、调整 weight 与 priority、选择路由策略——所有多供应商编排集中在一处完成。
