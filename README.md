# LLM Gateway

> 一个开源的多租户大模型网关:统一对接**阿里云百练 / 火山方舟 / 百度千帆**,对外同时提供 **OpenAI** 与 **Anthropic** 兼容接口,内置多模态、预付计费、API Key 管理、用量仪表盘与多轮对话台。

基于"端口适配器(六方形)架构":入口双协议统一转为 OpenAI 格式内部规范(canonical),出口适配器再转为各供应商原生协议。三家国内供应商均提供 OpenAI 兼容端点,故共用一个适配器实现。

## 📚 文档

完整文档站位于 [`docs-site/`](./docs-site)(VitePress),涵盖快速开始、核心概念、模型与定价、多供应商负载均衡、计费、部署、配置参考、架构与数据模型。

```bash
cd docs-site
npm install
npm run docs:dev         # 本地预览 http://localhost:5173
npm run docs:build       # 构建静态站点到 docs-site/.vitepress/dist
```

## ✨ 特性

- **双协议入口** — `/v1/chat/completions`(OpenAI)与 `/v1/messages`(Anthropic)语义等价互转
- **多家供应商** — 百练(通义千问)、火山方舟(豆包)、千帆(文心)、DeepSeek、智谱 GLM + 开发用 Mock Provider;新增供应商只需一个 OpenAI 兼容 adapter
- **多模态** — OpenAI content-parts 为内部规范 + `/v1/files` 文件托管(图片/音频/文件)
- **预付计费** — 按 token 实时扣费(流式按 chunk 累计),售价/成本/毛利全链路记录;**单事务扣费(`FOR UPDATE`)+ 余额非负 CHECK + 输入成本预检**,杜绝并发透支与账实不符
- **计费闭环** — 计费失败**幂等重试**(内联指数退避 + 后台 worker 兜底 + 部分唯一索引),防漏账/双扣;账本为资金唯一真相
- **支付充值** — 微信 Native + 支付宝电脑网站支付 + Mock(开发模式),订单状态机(pending→paid/closed)+ 回调幂等入账
- **混合密钥** — 平台默认渠道池 + 租户 BYOK 覆盖
- **多租户** — 终端用户自助注册、聊天台、API Key、用量、充值;**平台/租户管理员二分**(平台超管跨租户,租户管理员仅限本租户,数据面/控制面按租户隔离)
- **团队协作** — 租户管理员凭**签名邀请链接**拉成员加入,团长可向成员**分发余额**(单事务原子转账,账本成对记账);平台默认渠道对租户只读可见
- **渠道调度** — 多渠道按优先级/权重,租户渠道优先于平台默认;瞬时错误单渠道退避重试 + 跨渠道故障转移
- **请求/响应日志** — 可选记录每次调用的请求与响应原文(采样/截断/保留天数可配),`X-Request-Id` 全链路关联请求日志/用量/账本/结构化日志,供生产排障与合规审计;管理端可查
- **用量配额** — API Key 级 RPM/TPM(分钟)+ 日/月请求数 + 日/月 token 上限,递进限流防滥用与成本失控,超额返回 429
- **工具调用归一** — OpenAI tools 与 Anthropic `tools`/`tool_use`/`tool_result` 跨协议双向互转(含流式 `input_json_delta`),经 `/v1/messages` 调 OpenAI 模型工具上下文不丢
- **可观测性** — Prometheus `/metrics`(RPM/TPM/延迟/计费/熔断/在途)、`/healthz` 存活 + `/readyz` 就绪探针、结构化日志 + `request_id` 全链路关联
- **单二进制部署** — 前端 SPA 构建产物内嵌,一个端口交付全部功能

## 🏗 架构

```
OpenAI client ──►  OpenAI ingress  ─┐
Anthropic client►  Anthropic ingress┘┘──► canonical(OpenAI 格式)
                                                 │
                                   ┌─────────────┴─────────────┐
                                   │  Core: 鉴权/路由/限流/计费 │
                                   └─────────────┬─────────────┘
                          Provider Port (Chat/Stream/Embed)
                 ┌────────┬────────┬────────┬──────────┬────────┐
              bailian  volcark  qianfan  deepseek/zhipuai  mock
```

详见 [设计规格](docs/design.md)。

## 🚪 控制面 / 数据面(网关接入点)

服务内部按两个平面组织,默认同进程同端口,可配置独立接入端口:

- **控制面(control plane)** `/api/*` + 公共展示页 + `/admin`:Web/管理端 REST、计费、渠道与定价管理。短超时、JWT/会话鉴权、应内网受限。
- **数据面(edge / 接入点)** `/v1/*` + `/files/*`:外部 API 客户端消费模型的入口(OpenAI/Anthropic 推理 + 文件)。API Key 鉴权、限流、长连接流式、无状态可横向扩展。

```yaml
# config.yaml
edge:
  addr: ""        # 空=同端口(默认,单实例/开发); ":8090"=接入点独立端口
```

| 形态 | 适用 | 部署 |
|---|---|---|
| 同端口(默认) | 单实例 / 开发 / 中小流量 | 一个二进制,`/v1` 与 `/api` 共用 `server.addr` |
| 独立接入端口 | 安全隔离 / 接入点公网暴露 | `edge.addr` 独立,公网只放接入点,管理端内网 |
| 双进程(演进) | 接入点需横向扩容 | 抽出 `cmd/edge` 二进制(仅 relay),共享 Postgres/Redis |

接入点路径与控制面解耦(`/v1` 仅经 `relay.Service`),故从"同端口"演进到"独立 edge 二进制"是文件级移动而非重写。

## 🚀 快速开始

### 依赖
- Go 1.26+、Node 20+、Docker(含 compose)
- Postgres 16、Redis 7(由 docker-compose 提供)

### 一键起栈(推荐)

```bash
git clone https://github.com/aitoys/llm-gateway && cd llm-gateway
docker-compose up -d --build   # 构建 gateway + 前端,起 postgres/redis
# 访问:
#   用户端  http://localhost:8080     (demo@demo.com / demo123)
#   管理端  http://localhost:8080/admin(admin@demo.com / admin123)
```

> ⚠️ 上面的演示账号由 `make seed`(或首次空库启动)生成,**仅用于本地体验**。
> 生产部署请务必:① 设 `dev: false`;② 用 `openssl rand -hex 32` 重新生成 `jwt_secret` 与 `channel_key_master`;③ 删除/改密演示账号;④ 按需关闭 `auth.allow_signup`。未配置真实密钥时,网关会**拒绝启动**(见下文「生产部署须知」)。

### 本地开发

```bash
make up                 # 起 postgres + redis
cp config.example.yaml config.local.yaml   # 按需修改(端口/密码)
make dev                # 跑后端(:8088,自动迁移)
make seed               # 灌入 mock 模型/渠道/用户/API Key

# 前端(另开终端)
make run-web-user       # http://localhost:5173(代理 /api 到 :8088)
make run-web-admin
```

> 国内网络:Go 默认走官方代理 `proxy.golang.org`(可在命令行覆盖 `make build GOPROXY=https://goproxy.cn,direct`);Docker 构建可用 `--build-arg GOPROXY=https://goproxy.cn,direct --build-arg NPM_REGISTRY=https://registry.npmmirror.com`。Docker 基础镜像如拉取超时,可用 `docker pull docker.m.daocloud.io/library/<image>` 后 retag。

## 📡 接口示例

**OpenAI 兼容**
```bash
curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer sk-demo-key-1234567890" \
  -H "Content-Type: application/json" \
  -d '{"model":"qwen-max","messages":[{"role":"user","content":"你好"}],"stream":true}'
```

**Anthropic 兼容**
```bash
curl http://localhost:8080/v1/messages \
  -H "Authorization: Bearer sk-demo-key-1234567890" \
  -H "Content-Type: application/json" \
  -d '{"model":"claude-3-5-sonnet","max_tokens":256,"system":"你是助手","messages":[{"role":"user","content":"你好"}]}'
```

任意兼容 OpenAI 的客户端(openai-python、LangChain、NextChat、Cherry Studio 等)把 `base_url` 指向本网关即可使用。

## 🔌 接入真实供应商

在管理端「渠道管理」新增渠道:
| 供应商 | provider | Base URL(留空用默认) | API Key |
|---|---|---|---|
| 阿里云百练 | `bailian` | `https://dashscope.aliyuncs.com/compatible-mode/v1` | DashScope API Key |
| 火山方舟 | `volcark` | `https://ark.cn-beijing.volces.com/api/v3` | Ark API Key |
| 百度千帆 | `qianfan` | `https://qianfan.baidubce.com/v2` | 千帆 API Key |

并在「模型与定价」配置模型名、售价(分/百万 token)、成本价。租户可在渠道处填 `tenant_id` 实现自带密钥(BYOK)覆盖。

## 💰 计费模型

- 金额一律以**分**为单位(bigint),避免浮点误差
- 售价 = `输入token × 输入售价/百万 + 输出token × 输出售价/百万`
- 预付余额 → 实时扣费 → 写 `billing_ledger`(usage/recharge/refund)
- 管理端仪表盘展示总收入 / 总成本 / 毛利

## 🧪 测试

```bash
make test               # 全部单元测试(含计费数学、canonical、relay 路由/重试判定、限流桶、鉴权角色)
make lint               # golangci-lint(若已安装),否则 go vet
```

覆盖:计费数学、canonical 转换、OpenAI 兼容适配器(非流/流式/错误透传)、relay 路由策略/瞬时错误判定/输入估算、限流内存桶、鉴权角色二分。

## 🗂 项目结构

```
cmd/gateway/         入口(main + seed)
internal/
  canon/             规范消息(OpenAI 格式)
  provider/          Provider 接口 + mock + openaicomp(百练/方舟/千帆共用)
  relay/             核心:路由/计费编排
  billing/           定价与记账
  store/             Postgres 仓储 + 嵌入式迁移
  auth/  crypto/  apikey/   鉴权与密钥
  api/openai/  api/anthropic/  api/web/   入口与 REST
  files/  static/   文件托管与 SPA 托管
web/user/  web/admin/    Vue 3 + Naive UI 前端
```

## ⚙️ 配置

见 [`config.example.yaml`](config.example.yaml)。环境变量以 `GATEWAY_` 前缀覆盖,嵌套用 `__`(例:`GATEWAY_POSTGRES__HOST`)。**生产务必**修改 `jwt_secret` 与 `channel_key_master`(`openssl rand -hex 32`)。

### 生产部署须知(安全)

- **`dev: false`**(默认):开启后网关校验 `jwt_secret` / `channel_key_master`,若为空或为示例值则**拒绝启动**;模拟充值与自助注册同时关闭。仅在本地开发设 `dev: true`。
- **密钥生成**:`openssl rand -hex 32` 分别用于 `auth.jwt_secret` 与 `auth.channel_key_master`(后者用于加密上游渠道 API Key,泄露即等于全部上游密钥泄露,务必妥善保管且支持轮换需重加密)。
- **首个平台管理员**:自助注册只能得到**租户级**管理员;跨租户的**平台超级管理员**需通过 `auth.bootstrap_admin.{email,password}` 引导创建(启动时若不存在则建),或本地 `make seed`(仅演示)。
- **跨域(CORS)**:默认仅同源;浏览器端跨域调用需在 `server.cors_origins` 显式配置允许的 Origin。
- **API Key 传递**:仅接受 `Authorization: Bearer` 头,不再支持 `?key=` 查询参数(避免密钥进入访问日志)。
- **运维探针**:`/healthz` 存活(恒 200)、`/readyz` 就绪(探测 Postgres+Redis,任一不可用返回 503)、`/metrics` Prometheus 指标。建议在负载均衡/K8s 上用 `/readyz` 作就绪门,`/metrics` 接入监控。

漏洞披露流程见 [SECURITY.md](SECURITY.md)。

## 🛣 路线图

- [x] 渠道调度:优先级分组 + 加权/轮询/主备/随机/钉选 + 熔断(含半开)+ 故障转移 + 瞬时错误重试
- [x] 安全基线:生产密钥强校验、密钥哈希不外泄、CORS 白名单、上传大小/类型限制、平台/租户管理员二分 + 数据面租户隔离
- [x] 计费防护:单事务扣费 + 余额非负约束 + 输入成本预检 + **失败幂等重试闭环**(内联退避 + worker 兜底)
- [x] 支付充值:微信 Native + 支付宝 PC + Mock,订单状态机 + 回调幂等入账
- [x] 团队协作:签名邀请链接 + 团长余额分发(原子转账)+ 平台渠道只读
- [x] 可观测性:Prometheus `/metrics`、`/readyz` 就绪探针、结构化日志 + `request_id` 关联
- [x] 限流健壮性:Redis 故障降级到进程内桶 + 流式 TPM 补登
- [x] 请求/响应原文日志:采样/截断/TTL 清理 + 管理端查询(`request_id` 关联 usage/billing/日志)
- [x] 用量配额:日/月请求数 + 日/月 token 上限(中间件桶 + preflight 预检,超额 429)
- [x] 工具调用归一:OpenAI tools ↔ Anthropic `tool_use`/`tool_result`(含流式 `input_json_delta`)
- [x] 工程基线:golangci-lint v2 接入 CI、JSON snake_case 统一、错误响应统一、资金核心集成测试
- [ ] 分布式部署:异步计费队列、接入点横向扩容(架构已预留扩展点)
- [ ] OTel 分布式链路追踪
- [x] 渠道模型独立实体化(`channel_models`,每个渠道×模型独立配成本/映射/启停/权重,数据迁移已保 26→26)— [设计文档](docs/design/channel-models-entity.md)
- [ ] Prompt 模板库、对话历史持久化
- [ ] S3/MinIO 文件后端

## 📄 License

MIT © aitoys / llm-gateway contributors
