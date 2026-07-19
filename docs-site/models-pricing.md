---
title: 模型与定价
---

# 模型与定价

模型（Model）是 LLM Gateway 对外暴露的**逻辑名称**，是租户在 API 调用、计费、配额中看到的唯一身份。一个模型（如 `gpt-4o`）背后可以挂载多个渠道（Channel）做负载均衡，而用户始终只用模型名调用。

## 在哪里配置

管理端进入 **模型与定价** 页，可新建或编辑模型。每条模型记录包含对外身份、面向用户的统一售价、能力标签以及路由策略。模型本身**不记录供应商成本**——成本归渠道，售价归模型。

## 字段说明

| 字段 | 含义 | 说明 |
| --- | --- | --- |
| `model_name` | 模型逻辑名 | 对外暴露的调用名，如 `gpt-4o`（JSON tag 为 `model_name`） |
| `input_price_cents_per_m` | 输入售价 | 单位：分 / 百万 token |
| `output_price_cents_per_m` | 输出售价 | 单位：分 / 百万 token |
| `cache_read_price_cents_per_m` | 缓存读取售价 | 命中 prompt 缓存的折扣分段售价，0=按普通输入价核算（`0013_model_cache_price`） |
| `cache_write_price_cents_per_m` | 缓存写入售价 | 写入 prompt 缓存的加价分段售价，0=按普通输入价核算 |
| `enabled` | 平台级启停 | 关闭后所有租户不可见 |
| `description` / `long_desc` | 展示文案 | 模型广场短/长描述 |
| `providers` | 展示用供应商标签 | 仅用于模型广场分组与筛选，与「由哪些渠道挂载」解耦（`0018_model_providers`） |
| `capabilities` | 能力多标签 | 如 `chat`、`vision`、`function-calling`、`embedding` |
| `context_length` | 上下文长度 | 最大上下文窗口（token） |
| `tags` | 业务标签 | 用于分组、筛选、展示 |
| `routing_strategy` | 路由策略 | `weighted` / `round_robin` / `failover` / `random` / `pinned`，默认 `weighted` |
| `pinned_channel_id` | 固定渠道 | 仅 `pinned` 策略生效，其余作兜底 |

## 售价语义

售价是**面向用户的统一价格**，归在模型层：

- 同一个模型无论路由到哪家供应商，对外计费价格恒定。
- 单位为**整数分（cents） / 百万 token**，避免浮点误差。
- 改价只需在模型层改一处，无需关心背后挂了几家供应商。

## 为什么模型层没有成本

成本随**实际命中的渠道**而变化：百练、火山方舟、千帆对同一上游模型的报价各不相同，BYOK（租户自带 Key）渠道甚至可以是零成本。因此：

- **售价**定义在模型上——对租户一致。
- **成本**定义在渠道上——按命中供应商的真实单价核算。
- **毛利 = 模型售价 − 命中渠道成本**，每次请求独立核算。

详见 [计费与账本](./billing) 与 [多供应商负载均衡](./load-balancing)。

## 创建示例

以下示例通过管理端 API 创建一个 `gpt-4o` 模型，售价为输入 250 分、输出 1000 分（每百万 token），具备 `chat` 与 `vision` 能力，采用默认 `weighted` 策略：

```bash
curl -X POST https://gateway.example.com/api/admin/models \
  -H "Authorization: Bearer <ADMIN_TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{
    "model_name": "gpt-4o",
    "input_price_cents_per_m": 250,
    "output_price_cents_per_m": 1000,
    "cache_read_price_cents_per_m": 125,
    "cache_write_price_cents_per_m": 313,
    "enabled": true,
    "description": "OpenAI 旗舰多模态模型",
    "capabilities": ["chat", "vision", "function-calling"],
    "context_length": 128000,
    "tags": ["openai", "旗舰"],
    "routing_strategy": "weighted"
  }'
```

创建完成后，在**模型编辑页**可一站式挂载渠道、设置权重与主备优先级，无需切换到渠道页单独维护。路由策略的完整语义见 [多供应商负载均衡](./load-balancing)。
