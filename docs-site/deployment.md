---
title: 部署指南
---

# 部署指南

LLM Gateway 提供两种二进制形态：

- **`cmd/gateway`**：控制面（管理端 REST + Web SPA + 迁移 / seed）。默认**内嵌数据面**（`/v1` 推理接入点），也可通过 `edge.standalone=true` 拆分为纯控制面。
- **`cmd/edge`**：独立数据面二进制，仅提供 `/v1` 推理与 `/files` 上传下载，**无状态可横向扩展**，与控制面共享同一 Postgres / Redis。

两种形态可按规模从「单实例内嵌」平滑演进到「双二进制 + 多副本」。

---

## 一、本地开发

最简方式：单进程内嵌接入点，前端走 Vite dev server。

```bash
# 1. 准备依赖
docker run -d --name pg -p 5432:5432 \
  -e POSTGRES_USER=gateway -e POSTGRES_PASSWORD=gateway \
  -e POSTGRES_DB=gateway postgres:16
docker run -d --name redis -p 6379:6379 redis:7

# 2. 拷贝示例配置并改本地连接
cp config.example.yaml config.local.yaml
#   按需修改 postgres / redis / jwt_secret / channel_key_master

# 3. 启动控制面（自动执行 go:embed 迁移，默认内嵌 /v1 接入点）
go run ./cmd/gateway -config config.local.yaml
```

常用启动标志：

| 标志 | 说明 |
| --- | --- |
| `-config <path>` | 配置文件路径，留空时按默认顺序查找 |
| `-migrate up\|down` | 执行数据库迁移后立即退出 |
| `-seed` | 灌入 mock 种子数据后退出（用于本地体验） |

```bash
# 仅迁移
go run ./cmd/gateway -config config.local.yaml -migrate up

# 灌入演示数据
go run ./cmd/gateway -config config.local.yaml -seed
```

灌入后可用以下演示账号登录：

- 管理员：`admin@demo.com` / `admin123`
- 普通用户：`demo@demo.com` / `demo123`
- API Key：`sk-demo-key-1234567890`

---

## 二、Docker 部署

### 多阶段构建说明

`Dockerfile` 共 4 个阶段，最终镜像包含两份后端二进制与两份前端 dist：

| 阶段 | 基础镜像 | 产物 |
| --- | --- | --- |
| `backend` | `golang:1.26-alpine` | `/out/gateway`、`/out/edge` |
| `user-web` | `node:20-alpine` | `web/user/dist`（用户端 SPA） |
| `admin-web` | `node:20-alpine` | `web/admin/dist`（管理端 SPA） |
| 运行阶段 | `alpine:3.20` | `/app/gateway`、`/app/edge`、`/app/web/user/dist`、`/app/web/admin/dist`、`/app/config.yaml` |

构建时已内置国内镜像源加速：

- Go：`GOPROXY=https://goproxy.cn,https://goproxy.io,https://proxy.golang.org,direct`（含重试回退）
- npm：`npm_config_registry=https://registry.npmmirror.com`

默认入口为 `/app/gateway -config /app/config.yaml`，暴露 `8080`。

### 构建与运行

```bash
# 构建
docker build -t llm-gateway:latest .

# 运行（单实例内嵌模式）
docker run -d --name gateway \
  -p 8080:8080 \
  -v $(pwd)/config.yaml:/app/config.yaml:ro \
  -v gateway-data:/app/data/files \
  -e GATEWAY_POSTGRES__HOST=host.docker.internal \
  -e GATEWAY_REDIS__ADDR=host.docker.internal:6379 \
  llm-gateway:latest
```

环境变量以 `GATEWAY_` 前缀覆盖配置，嵌套用 `__`，例如：

```bash
GATEWAY_POSTGRES__HOST=...
GATEWAY_POSTGRES__PASSWORD=...
GATEWAY_REDIS__PASSWORD=...
GATEWAY_AUTH__JWT_SECRET=...
GATEWAY_AUTH__CHANNEL_KEY_MASTER=...
GATEWAY_EDGE__STANDALONE=true
```

### 仅运行 edge 接入点

同一镜像内置 `edge` 二进制，覆盖 entrypoint 即可：

```bash
docker run -d --name edge-1 \
  -p 8090:8090 \
  -v $(pwd)/config.yaml:/app/config.yaml:ro \
  --entrypoint /app/edge \
  llm-gateway:latest -config /app/config.yaml
```

---

## 三、单实例内嵌模式（默认）

`edge.standalone` 留空或为 `false` 时，`/v1` 推理接入点内嵌于 `cmd/gateway` 进程。

- `edge.addr` 留空：接入点与控制面**同端口**（`/v1/*`、`/files/*` 挂在主 engine）。
- `edge.addr` 设值（且与 `server.addr` 不同）：接入点**同进程独立端口**，便于公网只暴露接入点、管理端内网隔离。

```yaml
edge:
  addr: ""          # 同端口
  # addr: ":8090"   # 同进程独立端口
```

适用：中小流量、开发、POC。一台机器 + 一个进程即可对外提供完整能力。

---

## 四、双二进制拆分（水平扩展）

将控制面与数据面彻底分离，接入点无状态、可多副本 + 负载均衡横向扩展。

### 拓扑

```
                ┌──────────────┐
  公网 ────►    │  LB / Nginx  │
                └──────┬───────┘
                       │ /v1 推理流量
        ┌──────────────┼──────────────┐
        ▼              ▼              ▼
   ┌─────────┐    ┌─────────┐    ┌─────────┐
   │ edge-1  │    │ edge-2  │    │ edge-N  │   (cmd/edge，可多副本)
   └────┬────┘    └────┬────┘    └────┬────┘
        └──────────────┼──────────────┘
                       │
                ┌──────┴───────┐
                │ Postgres     │   共享存储
                │ Redis        │   (cb:* / rl:* / rr:* 等)
                └──────┬───────┘
                       │ 内网
                ┌──────┴───────┐
                │  gateway     │   控制面（单实例，管理端 + Web）
                └──────────────┘
```

### 配置要点

控制面侧把 `edge.standalone` 设为 `true`，进程不再内嵌 `/v1`：

```yaml
edge:
  standalone: true
  addr: ":8090"   # 拆分形态下此值控制面会忽略，由 edge 二进制读取其自身地址
```

启动命令：

```bash
# 控制面（内网，承担迁移、seed、管理端、Web）
./gateway -config config.yaml

# 接入点副本（公网，每个副本一份）
./edge -config config.yaml
```

`cmd/edge` 的监听地址取 `edge.addr`，为空则回退 `server.addr`。多个 edge 副本共用同一份 `config.yaml`，前方挂 LB（轮询 / 最少连接）即可水平扩展。

> 注意：`edge` 进程不负责迁移与 seed（由控制面独占管理），但启动时会幂等地确保 schema 存在。

---

## 五、数据库迁移与 seed

迁移文件通过 `go:embed` 内嵌到二进制，**无需额外文件**，进程启动时自动执行 `up`。

```bash
# 升级到最新
./gateway -config config.yaml -migrate up

# 回滚（谨慎，会撤销最近迁移）
./gateway -config config.yaml -migrate down

# 灌入演示数据
./gateway -config config.yaml -seed
```

`-migrate` / `-seed` 执行完毕后进程立即退出，适合在 CI / 容器 init 容器中单独运行：

```yaml
# Kubernetes init 示例
initContainers:
  - name: migrate
    image: llm-gateway:latest
    command: ["/app/gateway", "-config", "/app/config.yaml", "-migrate", "up"]
```

容器场景下，日常运行实例可保持默认（启动自动 `up`），仅在需要回滚时显式执行 `-migrate down`。
