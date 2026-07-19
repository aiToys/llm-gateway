---
title: 5 分钟上手
---

# 5 分钟上手

本页带你从零跑起 LLM Gateway：起依赖 → 配置 → 启动 → 在浏览器里看到控制台 → 用 curl 发出第一个推理请求。

预计耗时 5 分钟。前置条件：已安装 [Go 1.26+](https://go.dev/dl/)、[Docker](https://www.docker.com/) 与 Docker Compose。

## 1. 起依赖（Postgres + Redis）

仓库根目录自带 `docker-compose.yml`，一键拉起 Postgres 16 与 Redis 7：

```bash
docker compose up -d postgres redis
docker compose ps   # 两个服务都应是 healthy
```

如果你已有自管的 Postgres / Redis，跳过这一步，在下一步配置里指向它们即可。

## 2. 准备配置

仓库根目录有两份示例：`config.example.yaml`（端口 8080，模板）和 `config.local.yaml`（端口 8088，本地开发用）。本地直接用后者即可：

```bash
cp config.example.yaml config.local.yaml   # 或直接改仓库里那份
```

关键配置项（详见文件内注释）：

| 字段 | 说明 | 本地默认 |
| --- | --- | --- |
| `server.addr` | 控制面监听端口（用户端 + 管理端 + 内嵌 edge） | `:8088` |
| `server.public_url` | 对外可访问地址，用于文件回链等 | `http://localhost:8088` |
| `postgres.*` / `redis.*` | 数据库连接 | `localhost:5432` / `localhost:6379` |
| `auth.jwt_secret` | JWT 签名密钥，**生产必改** | 占位串 |
| `auth.channel_key_master` | 渠道 API Key 加密主密钥（32 字节 hex） | 占位串 |
| `defaults.default_provider` | 兜底供应商 | `mock` |
| `edge.addr` | 数据面独立端口，留空表示与控制面同进程 | `""` |

生产环境务必重新生成密钥：

```bash
openssl rand -hex 32   # 用作 channel_key_master
```

## 3. 启动网关

从仓库根目录执行：

```bash
go run ./cmd/gateway -config config.local.yaml
```

启动时网关会**自动执行数据库迁移**（建表、索引一气呵成），无需手动跑 SQL。

::: tip 想要演示数据？
加一个 `-seed` 参数，会灌入 mock 模型、demo 账号与演示用 API Key：

```bash
go run ./cmd/gateway -config config.local.yaml -seed
```
:::

首次运行 `go run` 会下载依赖，可能稍慢；后续启动是秒级。看到类似下面这行即代表就绪：

```
gateway listening on :8088 (edge embedded)
```

## 4. 打开控制台

浏览器访问：

| 入口 | 地址 | 账号 / 密码 |
| --- | --- | --- |
| 用户端 | `http://localhost:8088/` | `demo@demo.com` / `demo123` |
| 管理端 | `http://localhost:8088/admin/` | `admin@demo.com` / `admin123` |

> 上面账号来自 `-seed` 灌入的演示数据。生产环境请勿使用这些弱口令。

## 5. 发第一个 OpenAI 兼容请求

`-seed` 会创建一把演示用 API Key：`sk-demo-key-1234567890`。用它调 `glm-4-plus` 这个逻辑模型：

```bash
curl -s http://localhost:8088/v1/chat/completions \
  -H "Authorization: Bearer sk-demo-key-1234567890" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "glm-4-plus",
    "messages": [
      {"role": "user", "content": "用一句话介绍 LLM Gateway"}
    ],
    "stream": false
  }' | jq
```

兼容 OpenAI 协议，所以任何 OpenAI SDK 都能直接对接——只需把 `base_url` 指向 `http://localhost:8088/v1`，`api_key` 填 `sk-` 开头的 Key。

流式响应同样支持：

```bash
curl -N http://localhost:8088/v1/chat/completions \
  -H "Authorization: Bearer sk-demo-key-1234567890" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "glm-4-plus",
    "messages": [{"role": "user", "content": "数到 5"}],
    "stream": true
  }'
```

## 6. 发一个 Anthropic 兼容请求

网关同时暴露 Anthropic 协议端点 `POST /v1/messages`。注意：网关统一用 `Authorization: Bearer` 鉴权（与 OpenAI 端点一致），不使用 Anthropic 官方的 `x-api-key`：

```bash
curl -s http://localhost:8088/v1/messages \
  -H "Authorization: Bearer sk-demo-key-1234567890" \
  -H "anthropic-version: 2023-06-01" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "glm-4-plus",
    "max_tokens": 256,
    "messages": [
      {"role": "user", "content": "用一句话介绍 LLM Gateway"}
    ]
  }' | jq
```

## 下一步

- 想理解「模型 / 渠道 / 供应商」是怎么协作把上面的请求路由出去的？→ [核心概念](/concepts)
- 想接真实供应商（百炼 / 火山方舟 / 千帆）？→ 进管理端「渠道」页新建渠道，填 API Key，并在模型表格逐行配置逻辑模型、上游名与成本（`channel_models`）。
- 想脱离 `go run` 部署？→ `make build` 或使用根目录 `Dockerfile`（多阶段构建，同时产出 gateway 与 edge 二进制并嵌入前端 dist）。
