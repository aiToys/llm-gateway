---
title: 可观测性
---

# 可观测性

LLM Gateway 的可观测体系由六部分构成：健康检查、Prometheus 指标、用量统计接口、管理端可视化（仪表盘 / 分析 / 审计）、渠道连通性测试、Redis 运行时状态键，以及结构化日志。

---

## 一、健康检查

```bash
GET /healthz
```

始终返回 `200 {"ok": true}`，不依赖下游依赖。适合接入 LB 健康检查与 Kubernetes liveness/readiness 探针：

```yaml
# Kubernetes 示例
livenessProbe:
  httpGet:
    path: /healthz
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 10
```

---

## 二、Prometheus 指标

控制面在 `/metrics` 暴露 Prometheus 抓取端点（`internal/metrics`），核心指标如下：

| 指标 | 类型 | 说明 |
| --- | --- | --- |
| `llm_requests_total` | Counter (`status`, `model`, `provider`) | LLM 请求总数 |
| `llm_request_duration_seconds` | Histogram (`status`, `model`, `provider`) | 请求耗时分布 |
| `llm_tokens_total` | Counter (`type=input/output/cache_read/cache_write`, `model`) | 处理的 token 总量 |
| `llm_charge_cents_total` | Counter (`type=usage/recharge/refund/transfer/adjust`) | 资金流水金额（分） |
| `llm_channel_up` | Gauge (`channel_id`) | 渠道熔断状态：1=正常放行，0=已熔断 |
| `llm_inflight_requests` | Gauge | 当前在飞请求 |
| `llm_billing_charges_abandoned_total` | Counter | 计费重试耗尽被放弃（资金漏账风险） |
| `llm_billing_charges_enqueue_fail_total` | Counter | 计费失败且无法入重试队列（不可恢复漏账） |

```bash
# 抓取
curl http://localhost:8080/metrics

# Prometheus 示例 scrape_config
scrape_configs:
  - job_name: llm-gateway
    static_configs: [{ targets: ['gateway:8080'] }]
```

后两个 `..._abandoned_total` / `..._enqueue_fail_total` 直接对应资金风险，运营应配置告警。

---

## 三、用量统计接口

聚合接口支持按维度分组与时间桶聚合，是仪表盘 / 分析页的底层数据源。

```bash
GET /api/usage/aggregate
    ?group_by=model|provider|api_key
    &bucket=minute|hour|day
    &days=N
```

| 参数 | 取值 | 说明 |
| --- | --- | --- |
| `group_by` | `model` / `provider` / `api_key` | 分组维度 |
| `bucket` | `minute` / `hour` / `day` | 时间桶粒度 |
| `days` | 整数 | 回溯天数 |

示例：

```bash
# 最近 7 天按模型分组的日用量
curl -H "Authorization: Bearer <token>" \
  "http://localhost:8080/api/usage/aggregate?group_by=model&bucket=day&days=7"
```

返回数据可直接绘制为时间序列柱状图 / 折线图。

---

## 四、管理端可视化

### 仪表盘

管理端首页仪表盘汇总核心经营指标：

- 总请求数
- 总收入
- 总成本
- 毛利（收入 − 成本）
- 活跃租户 / 活跃用户

### 分析页

基于 `/api/usage/aggregate` 渲染的多维图表，可切换 `group_by` 与 `bucket`，下钻到模型 / 供应商 / API Key 维度查看趋势。

### 审计日志页

审计表 `audit_logs` 记录管理端敏感操作，字段包括：

| 字段 | 说明 |
| --- | --- |
| `actor_id` | 操作者（用户 ID，可空表示系统） |
| `action` | 动作类型（创建 / 更新 / 删除 / 登录等） |
| `target` | 操作目标（资源类型 + ID） |
| `payload` | jsonb，动作详情（变更前后等） |
| `ip` | 来源 IP |

审计日志在管理端按时间倒序展示，支持按字段过滤，用于合规追溯与安全审计。

---

## 五、请求/响应原文日志

用量统计只记元信息（token/费用/状态），生产排障与合规审计常需回溯请求与响应原文。`request_logs` 表按可配策略落库：

| 配置项 | 默认 | 说明 |
| --- | --- | --- |
| `req_log.enabled` | `false` | 总开关，默认关闭（隐私与存储考量） |
| `req_log.sample_rate` | `1.0` | 采样率；**失败请求一律落库**，不受采样限制 |
| `req_log.max_body_bytes` | `32768` | 请求/响应体截断长度，超出截断 |
| `req_log.retain_days` | `7` | 保留天数，后台 worker 自动清理超期记录 |
| `req_log.log_bodies` | `true` | `false`=只记元信息不记原文 |

每条记录带 `request_id`，与用量（`usage_records`）、账本（`billing_ledger`）、结构化日志共享同一 ID。响应头 `X-Request-Id` 返回给客户端，便于端到端关联。

管理端「请求日志」页支持按租户 / 用户 / API Key / 模型 / 状态 / 时间过滤，点击查看请求与响应原文。详见 [配置参考 · req_log](./configuration#req-log)。

```bash
# 管理端查询（平台超管可跨租户）
curl -H "Authorization: Bearer <admin-token>" \
  "http://localhost:8080/api/admin/request-logs?model=qwen-max&status=500&limit=50"

# 查看单条详情（含原文 body）
curl -H "Authorization: Bearer <admin-token>" \
  "http://localhost:8080/api/admin/request-logs/<id>"
```

---

## 六、渠道连通性测试

对任一渠道执行一次实际探测，验证密钥与网络可达性：

```bash
POST /api/admin/channels/:id/test
```

```bash
curl -X POST -H "Authorization: Bearer <admin-token>" \
  "http://localhost:8080/api/admin/channels/42/test"
```

返回通常包含：HTTP 状态、延迟、上游返回摘要。在管理端「渠道」页点击「测试」按钮即调用此接口；新增渠道或更换密钥后建议先测试再启用。

---

## 七、Redis 运行时状态键

运行时的熔断、限流、轮询游标、配额计数等动态状态全部落在 Redis。键名严格按维度组织，**限流与配额仅落在 API Key 维度**（`Subject.APIKeyID`），没有 tenant/channel 维度的运行时键。

| 前缀 | 含义 | 真实键名（`{id}` = 渠道 ID / `{keyid}` = API Key ID） |
| --- | --- | --- |
| `cb:` | **C**ircuit **B**reaker 熔断状态（按渠道） | `cb:{channelID}:open` / `cb:{channelID}:fail` / `cb:{channelID}:probe` |
| `rl:rpm:` | **R**ate **L**imit RPM（每分钟请求数） | `rl:rpm:{keyid}:{YYYYMMDDHHMM}` |
| `rl:tpm:` | Rate Limit TPM（每分钟 token 数） | `rl:tpm:{keyid}:{YYYYMMDDHHMM}` |
| `quota:req:d:` / `quota:req:m:` | 日/月请求数配额 | `quota:req:d:{keyid}:{YYYYMMDD}` / `quota:req:m:{keyid}:{YYYYMM}` |
| `quota:tok:d:` / `quota:tok:m:` | 日/月 token 配额（已写入 Redis，中间件尚未消费 `*_token_limit` 字段，预留） | `quota:tok:d:{keyid}:{YYYYMMDD}` / `quota:tok:m:{keyid}:{YYYYMM}` |
| `rr:` | **R**ound-**R**obin 轮询游标（按租户×模型） | `rr:{tenantID}:{modelName}` |

### 用 redis-cli 查看

```bash
# 熔断：列出 / 查看某渠道的失败计数与打开标记
redis-cli --scan --pattern 'cb:*'
redis-cli GET cb:42:fail
redis-cli GET cb:42:open

# 限流（API Key 维度，分钟桶键格式 YYYYMMDDHHMM）
redis-cli --scan --pattern 'rl:rpm:*'
redis-cli GET rl:rpm:ak-abc:202607181430

# 配额（API Key 维度）
redis-cli --scan --pattern 'quota:req:*'
redis-cli GET quota:req:d:ak-abc:20260718

# 轮询游标（租户 × 模型）
redis-cli --scan --pattern 'rr:*'
redis-cli GET rr:tnt-123:gpt-4o
```

> `--scan` 是安全的惰性遍历，避免在生产使用 `KEYS *`。键结构以代码实现为准（均为 string 类型）。

### 排障场景

- **某渠道一直 503**：先查 `cb:{channelID}:open` 是否存在、`cb:{channelID}:fail` 计数是否触达阈值；若处于半开态则等待恢复或人工介入。
- **请求被拒但上游正常**：查 `rl:rpm:{keyid}:*` / `quota:req:*:{keyid}:*` 是否触达上限。
- **流量集中在某个密钥**：查 `rr:{tenantID}:{model}` 游标是否异常，或仅配置了一个渠道/密钥。

如需重置运行时状态（谨慎），可按前缀删除：

```bash
# 仅删除某渠道熔断状态
redis-cli DEL cb:42:open cb:42:fail cb:42:probe

# 清空所有限流计数（影响线上，谨慎）
redis-cli --scan --pattern 'rl:*' | xargs redis-cli DEL
```

---

## 八、结构化日志

- 控制面与 edge 进程均使用标准库 `log/slog` 输出**结构化日志**（全局 logger 由 `internal/logging` 提供），默认 `TextHandler` 输出到 stdout，启动时会打印监听地址与运行模式（内嵌 / 独立端口 / 纯控制面）。
- 每条请求日志带 `request_id`，与 `usage_records` / `billing_ledger` / `request_logs` 共享同一 ID，便于端到端关联。
- edge 进程日志以 `[edge]` 前缀标识。
- 容器部署建议将 stdout 收敛到集中日志系统（Loki / ELK / CloudWatch 等）。

```bash
# 实时查看
docker logs -f gateway

# Kubernetes
kubectl logs -f deployment/gateway -c gateway
kubectl logs -f deployment/edge -c edge
```

结合审计日志（落库）与运行时状态键（Redis），可形成「请求 → 计费 → 熔断/限流 → 审计」的完整可观测闭环。
