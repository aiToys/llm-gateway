---
title: 计费与账本
---

# 计费与账本

LLM Gateway 的计费体系围绕三条原则：**整数分精度**、**售价归模型 / 成本归渠道**、**预付余额实时扣费**。本页讲清精度设计、毛利模型、账本结构与统计维度。

## 整数分精度

所有金额以 **bigint cents（分）** 存储，单位是「分 / 百万 token」：

- 售价字段：`input_price_cents_per_m` / `output_price_cents_per_m`（在模型上），可选 `cache_read_price_cents_per_m` / `cache_write_price_cents_per_m`（缓存命中分段售价，见下）
- 成本字段：`input_cost_cents_per_m` / `output_cost_cents_per_m`（在渠道上），可按模型覆盖（见下）

用整数分而非浮点元，是为了消除浮点累计误差，保证账本在亿级请求下仍能逐笔对账、分毫不差。

## 售价归模型，成本归渠道

这是整套计费设计的核心：

| 维度 | 归属 | 理由 |
| --- | --- | --- |
| 售价（price） | 模型层 | 对租户价格必须统一，与背后命中哪家供应商无关 |
| 成本（cost） | 渠道层 | 供应商报价各异，BYOK 渠道甚至零成本，必须按实际命中核算 |

由此推出毛利公式：

```
margin = QuotePrice(model) − QuoteCost(channel)
```

::: warning 同一模型，毛利按实际命中渠道核算
`gpt-4o` 同时挂载百练、方舟、千帆三家。一次请求路由到方舟，则本次成本按方舟报价计算；下一次路由到百练，成本就按百练计算。**售价对租户不变，毛利随命中渠道波动**。运营仪表盘上的总毛利是所有请求的逐笔累加，而不是按某一家供应商的固定差价估算。
:::

## 渠道×模型成本覆盖

同一渠道挂载多个模型时，各模型进货价往往差异巨大（如 `qwen-turbo` 与 `qwen-max` 同属一个百练渠道，单价相差数倍）。渠道级单一成本价无法表达这种差异，因此每个渠道可**按模型覆盖成本**：

每个渠道×模型一行 `channel_models`，原生承载这种差异：

- 渠道级回退：`channels.input_cost_cents_per_m` / `output_cost_cents_per_m`（当某 `channel_models` 行成本为 0 时回退）
- 模型级（首选）：`channel_models[model_name]` → `{ input_cost_cents_per_m, output_cost_cents_per_m, cache_read_cost_cents_per_m, cache_write_cost_cents_per_m }`

计费时优先取 `channel_models` 行的成本，为 0 则回退渠道级。在管理端「渠道」编辑页的模型表格逐行配置（每行 `模型 / 上游名 / 输入 / 输出 / 缓存读 / 缓存写 / 启停`）。

## 缓存命中分段计价

OpenAI、Anthropic、DeepSeek 等供应商对 prompt 缓存命中部分按折扣计价（OpenAI cached input 0.5x、Anthropic cache read 0.1x / cache write 1.25x、DeepSeek 0.1x）。网关对此做**分段计量与计价**，避免毛利在启用缓存的供应商上失真：

- **计量**：上游返回的 `cached_tokens` / `cache_read_input_tokens` / `cache_creation_input_tokens` 被解析归一为 `cache_read_tokens` / `cache_write_tokens`。
- **售价**：模型配置 `cache_read_price_cents_per_m` / `cache_write_price_cents_per_m`。
- **成本**：在 `channel_models` 行的 `cache_read_cost_cents_per_m` / `cache_write_cost_cents_per_m` 中按渠道×模型配置缓存成本。

```
price = normal_input × input_price
      + cache_read   × cache_read_price
      + cache_write  × cache_write_price
      + completion   × output_price
```

未配置缓存价（为 0）的模型，缓存 token 按普通输入价核算，行为与升级前完全一致（向后兼容）。

## 预付余额实时扣费

采用预付制：租户先充值（`recharge`）获得余额，每次请求完成后实时扣费。

### Preflight：余额不足的「拒绝」发生在这里

请求**进入**推理前先做余额预检（preflight）：余额 ≤ 0 直接拒绝，不消耗上游额度。这是「余额不足拒绝」唯一发生的位置，热路径优先内存判定，落库异步保障一致性。

### ChargeAtomic：扣费 + 记账的单事务原子

请求完成后调 `Store.ChargeAtomic` 完成扣费，**不是简单的「检查 → 扣款 → 记账」三步**，而是用以下机制消除双扣与漏账：

1. **`SELECT ... FOR UPDATE` 行锁**：先锁住 `users` 行，串行化同一用户的并发扣款并取到可信余额快照。锁必须在幂等判断之前，否则两个并发事务的幂等 SELECT 都看不到对方未提交的行，各自 UPDATE 扣款会导致余额扣两次但账本只落一条。
2. **`INSERT ... ON CONFLICT DO NOTHING` 幂等闸**：当 `type='usage'` 且 `request_id` 非空时，先尝试插入账本行；仅当本次真正 `INSERT` 成功（`RETURNING` 有行）才扣减余额。幂等闸依赖 `0015_billing_outbox` 建立的部分唯一索引 `uniq_ledger_usage_request ON billing_ledger(request_id) WHERE type='usage'`，是该路径并发幂等的唯一闸门。
3. **实扣 = min(余额, 应收)**：扣到 0，配合 `users.balance_cents >= 0` 的 CHECK 约束保证余额非负；**账目仍记完整应收价**（不丢失应收信息，便于对账与坏账识别）。

> 「拒绝」不在 ChargeAtomic 内发生——ChargeAtomic 只会「扣到 0」，不会拒绝。拒绝只发生在 preflight 层。这是为了让已经在跑的推理尽可能落账（少漏账），坏账风险由 `balance_after` 快照与运营人工处理。

### 失败入 pending_charges 重试队列

若 ChargeAtomic 经有限次内联重试（覆盖瞬时抖动）仍失败，应扣项落 `pending_charges` 表，由后台 worker 每 ~20s 扫描重试，保证最终一致（见 [数据模型 · pending_charges](./data-model#pending-charges-计费重试队列)）。指标 `llm_billing_charges_abandoned_total`（彻底放弃）与 `llm_billing_charges_enqueue_fail_total`（连重试队列都进不去）标记资金风险，运营应配置告警。

## 账本：billing_ledger

每笔资金流转在 `billing_ledger` 留下一条不可变记录，共 13 个字段：

| 字段 | 含义 |
| --- | --- |
| `id` | 账目 ID |
| `tenant_id` / `user_id` | 租户 / 用户 |
| `request_id` | 请求唯一 ID（usage 类的幂等键） |
| `model` | 模型名 |
| `input_tokens` / `output_tokens` | 用量 |
| `type` | 账目类型，五类（见下） |
| `price_cents` | 售价（分），带符号（见下） |
| `cost_cents` | 成本（分） |
| `margin_cents` | 毛利 = `price_cents − cost_cents` |
| `balance_after` | 本次流转后余额快照，用于对账 |
| `created_at` | 时间戳 |

账本只追加（append-only），适合直接对接数仓或 BI 做长期分析。

### 五类账目与 `price_cents` 符号约定

`price_cents` 表示**对用户余额的影响方向**：正数=出账（扣余额），负数=入账（加余额）。`balance_after` 是扣/加之后的快照。

| `type` | 含义 | `price_cents` 符号 | 写入路径 |
| --- | --- | --- | --- |
| `usage` | 正向消费 | 正（应收价） | `ChargeAtomic`，受 usage 幂等闸保护 |
| `recharge` | 充值 | 负，`PriceCents = -amountCents` | `Recharge` → `AdjustAtomic` |
| `refund` | 退款 | 负，`PriceCents = -amountCents` | `Refund` → `AdjustAtomic` |
| `transfer` | 团队转账（团长→成员，出账/入账成对） | 出账正、入账负 | `Transfer` → 双行 `AdjustAtomic` |
| `adjust` | 管理员人工调整 | 负（`PriceCents = -delta`，调整逻辑 `balance += -PriceCents`） | `Adjust` → `AdjustAtomic` |

> 「裸改 `users.balance_cents`」是严格禁止的反模式——任何余额变动都必须走账本，否则「账本 = 余额唯一真相」不变量破裂、对账断裂、无法追溯。`adjust` 类账目正是为此存在。

## 统计：usage_records

除账本外，每次请求还会写一条 `usage_records`，面向运营统计：

- 维度：`request_id` / `model` / `provider` / `channel_id` / `api_key` / `tokens` / `price` / `cost` / `latency`
- 聚合粒度：按 `model` / `provider` / `api_key` × `minute` / `hour` / `day`

管理端仪表盘基于 `usage_records` 与 `billing_ledger` 汇总，典型看板包括：

- 总请求数、总 token 数
- 总收入（Σ `price`）、总成本（Σ `cost`）、总毛利（Σ `margin`）
- 按模型、按供应商、按 API Key 的趋势与占比
- 命中渠道分布（用于评估路由策略与成本优化效果）

## 充值：recharge

`recharge` 操作给租户账户加余额，并同样在 `billing_ledger` 留痕（`type='recharge'`，`price_cents = -amountCents` 即负数表示入账，`cost_cents`/`margin_cents` 为 0），保证资金流水与请求扣费在同一张账本里闭环。详见上文「五类账目与符号约定」。

## 真实支付：payment_orders

预付制要闭环，用户必须能往账户充值。系统接入微信支付（Native 扫码）与支付宝（电脑网站支付），订单全生命周期记录在独立的 `payment_orders` 表（与 `recharges`/`billing_ledger` 解耦）：

- **下单**：`POST /api/recharge/order` 创建订单并调渠道下单，返回预支付数据（微信 `code_url` / 支付宝跳转 URL）。
- **回调入账**：渠道异步通知 `POST /api/payments/{provider}/notify`，验签后将订单 `pending → paid`，再调一次 `recharge` 入账（加余额 + 写账本）。
- **幂等**：以 `out_trade_no` 为唯一键，`MarkPaid` 仅在 `pending → paid` 时返回 true 才入账，回调重入/重复通知只加一次余额。
- **防回调丢失**：前端轮询 `GET /api/recharge/order/:no` 时后端顺带主动查单一次；后台每分钟扫描超时未支付订单，先向渠道查单确认未付再关单（双重确认避免误关已付单）。

渠道通过配置启用（`payment.wechat.*` / `payment.alipay.*`）；`payment.mock=true` 或 `dev=true` 时启用 mock provider，无需商户资质即可端到端验证全链路。
