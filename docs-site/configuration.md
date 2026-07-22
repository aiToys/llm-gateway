---
title: 配置参考
---

# 配置参考

LLM Gateway 通过 `config.yaml` 集中配置。所有字段均可被环境变量覆盖，规则为 `GATEWAY_` 前缀 + `__` 嵌套，例如：

```bash
GATEWAY_POSTGRES__HOST=db.internal
GATEWAY_AUTH__JWT_SECRET=$(openssl rand -hex 32)
```

下方按配置段列出全部字段（默认值与说明参照 `config.example.yaml`）。

---

## server — HTTP 服务

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `server.addr` | `:8080` | 控制面监听地址 |
| `server.public_url` | `http://localhost:8080` | 对外可见的公网地址，用于生成回调 / 文件 URL |

```yaml
server:
  addr: ":8080"
  public_url: "http://localhost:8080"
```

---

## postgres — 数据库

各字段会在内部拼接成 DSN：`host=... port=... user=... password=... dbname=... sslmode=...`。

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `postgres.host` | `localhost` | Postgres 主机 |
| `postgres.port` | `5432` | 端口 |
| `postgres.user` | `gateway` | 用户名 |
| `postgres.password` | `gateway` | 密码 |
| `postgres.database` | `gateway` | 库名 |
| `postgres.sslmode` | `disable` | `disable` / `require` / `verify-ca` / `verify-full` |
| `postgres.max_conns` | `20` | pgx 连接池上限 |

```yaml
postgres:
  host: "localhost"
  port: 5432
  user: "gateway"
  password: "gateway"
  database: "gateway"
  sslmode: "disable"
  max_conns: 20
```

---

## redis — 缓存 / 运行时状态

存储熔断（`cb:*`）、限流（`rl:*`）、轮询游标（`rr:*`）等运行时键。

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `redis.addr` | `localhost:6379` | Redis 地址 |
| `redis.password` | `""` | 密码，留空表示无密码 |
| `redis.db` | `0` | DB 编号 |

```yaml
redis:
  addr: "localhost:6379"
  password: ""
  db: 0
```

---

## auth — 鉴权与加密（生产必改）

::: danger 生产环境强制项
以下两项在 `config.example.yaml` 中为占位值，**上线前必须替换**：

- `auth.jwt_secret`：JWT 签名密钥，建议 ≥ 32 字节。
- `auth.channel_key_master`：渠道密钥加密主密钥（AES），32 字节 hex（即 64 个 hex 字符）。一旦设置后不可随意更改，否则历史加密的渠道凭证将无法解密。

生成方式：

```bash
openssl rand -hex 32
```
:::

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `auth.jwt_secret` | `change-me-...` | JWT 签名密钥，生产必改 |
| `auth.access_ttl` | `15m` | Access Token 有效期 |
| `auth.refresh_ttl` | `168h` | Refresh Token 有效期（默认 7 天） |
| `auth.channel_key_master` | `0123...` | 渠道密钥 AES 主密钥（32 字节 hex），生产必改 |

```yaml
auth:
  jwt_secret: "请替换为 openssl rand -hex 32 生成的值"
  access_ttl: "15m"
  refresh_ttl: "168h"
  channel_key_master: "请替换为 openssl rand -hex 32 生成的值"
```

---

## files — 文件存储

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `files.storage` | `local` | 存储后端，目前支持 `local`（`s3` 后续支持） |
| `files.local_root` | `./data/files` | 本地存储根目录 |
| `files.base_url` | `http://localhost:8080` | 文件对外访问基址 |

```yaml
files:
  storage: "local"
  local_root: "./data/files"
  base_url: "http://localhost:8080"
```

---

## billing — 计费策略

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `billing.min_balance_cents` | `0` | 余额不足时的兜底阈值（单位：分） |
| `billing.chars_per_token` | `2` | 无 usage 回传时的字符估算系数，`1 token ≈ N 字符` |

```yaml
billing:
  min_balance_cents: 0
  chars_per_token: 2
```

---

## req_log — 请求/响应原文日志

可选记录每次调用的请求与响应原文，供生产排障与合规审计。默认关闭（隐私与存储考量），按需开启。落库异步、失败仅记日志（请求日志非资金，容忍偶丢）；失败请求一律落库，不受采样限制。

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `req_log.enabled` | `false` | 总开关 |
| `req_log.sample_rate` | `1.0` | 采样率 `0~1`，`1`=全量；失败请求不受此限制 |
| `req_log.max_body_bytes` | `32768` | 请求/响应体截断长度（字节），超出截断 |
| `req_log.retain_days` | `7` | 保留天数，超期由后台 worker 自动清理 |
| `req_log.log_bodies` | `true` | `false`=只记元信息不记原文（纯合规统计场景） |

```yaml
req_log:
  enabled: false
  sample_rate: 1.0
  max_body_bytes: 32768
  retain_days: 7
  log_bodies: true
```

> 每条记录带 `request_id`，与用量记录（`usage_records`）、账本（`billing_ledger`）、结构化日志共享同一 ID，形成全链路关联。管理端「请求日志」页可按租户/用户/密钥/模型/状态/时间过滤查询。

---

## defaults — 默认值

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `defaults.default_provider` | `mock` | 开发期默认使用 Mock 供应商；生产请在管理端配置真实渠道 |

```yaml
defaults:
  default_provider: "mock"
```

---

## web — 前端 SPA 挂载

任务约定字段名（驼峰式，对应源码 `cfg.Web.UserDist` / `cfg.Web.AdminDist`）。值为空字符串时跳过对应 SPA 挂载。

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `web.user_dist` | `""` | 用户端 SPA dist 目录，空则不挂载 `/` |
| `web.admin_dist` | `""` | 管理端 SPA dist 目录，空则不挂载 `/admin` |
| `web.auth_rpm` | `20` | 公开鉴权端点（`/api/auth/login`/`register`/`invites/accept`）按来源 IP 的每分钟请求上限，防爆破；`0` 表示不限 |

```yaml
web:
  user_dist: "./web/user/dist"
  admin_dist: "./web/admin/dist"
  auth_rpm: 20  # 登录/注册/邀请接受 按 IP 每分钟上限
```

---

## edge — 数据面（接入点）

| 字段 | 默认 | 说明 |
| --- | --- | --- |
| `edge.standalone` | `false` | `true` 时控制面不内嵌 `/v1`，由独立 `cmd/edge` 二进制承担 |
| `edge.addr` | `""` | 接入点监听地址；为空 → 与 `server.addr` 同端口；设值且不同 → 独立端口 |
| `edge.read_timeout` | `""` | 读超时，留空表示不限（流式默认不限） |
| `edge.write_timeout` | `""` | 写超时，留空表示不限（SSE 默认不限） |

```yaml
edge:
  standalone: false
  addr: ""
  read_timeout: ""
  write_timeout: ""
```

三种形态速查：

| 形态 | `standalone` | `addr` | 行为 |
| --- | --- | --- | --- |
| 同端口内嵌 | `false` | `""` | `/v1` 与控制面共用 `server.addr` |
| 同进程独立端口 | `false` | `:8090` | 接入点独立监听 `:8090` |
| 双二进制拆分 | `true` | （由 edge 进程读取） | 控制面纯管理，`cmd/edge` 承担推理 |

---

## 最小可用 config.yaml

适合本地快速拉起（含内嵌接入点）：

```yaml
server:
  addr: ":8080"
  public_url: "http://localhost:8080"

postgres:
  host: "localhost"
  port: 5432
  user: "gateway"
  password: "gateway"
  database: "gateway"
  sslmode: "disable"
  max_conns: 20

redis:
  addr: "localhost:6379"
  password: ""
  db: 0

auth:
  jwt_secret: "use-openssl-rand-hex-32-in-production"
  access_ttl: "15m"
  refresh_ttl: "168h"
  channel_key_master: "use-openssl-rand-hex-32-in-production"

files:
  storage: "local"
  local_root: "./data/files"
  base_url: "http://localhost:8080"

defaults:
  default_provider: "mock"

edge:
  standalone: false
  addr: ""
```

生产环境除上述字段外，还建议：

- 将 `auth.jwt_secret` 与 `auth.channel_key_master` 通过环境变量 / Secret 注入，避免落盘。
- 把 `defaults.default_provider` 改为真实 provider，或在管理端配置真实渠道。
- 在 LB / 反向代理层启用 HTTPS，并把 `server.public_url` 设为对外 HTTPS 地址。
