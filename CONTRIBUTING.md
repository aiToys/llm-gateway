# 贡献指南

感谢你对 LLM Gateway 的关注!以下是参与贡献的约定。

## 开发环境
- Go 1.26+、Node 20+、Docker、PostgreSQL 16+、Redis 7+
- golangci-lint **v2.12.2**(v2 格式;`make lint` 未安装时回退 `go vet`)
- `make up` 起 postgres/redis,`make dev` 跑后端,前端走 `make run-web-user` / `run-web-admin`

## 代码规范
- 遵循已有目录与命名约定,中文注释(与现有代码库一致)
- 遵循 KISS / YAGNI / DRY / SOLID;**未使用的代码与依赖应一并移除**
- 数据库 schema 变更必须通过新增 `internal/store/migrations/NNNN_*.up.sql` + `.down.sql`(可回滚),不得手改历史迁移
- 计费/资金相关改动须保证事务原子性(`store.ChargeAtomic` / `AdjustAtomic` 模式)且余额非负(`users_balance_nonnegative` CHECK 兜底)
- 安全相关:不得硬编码密钥、不得放宽生产鉴权(`dev:false` 下校验仍须生效)、不得向客户端透出上游原始错误体
- 前端主题色统一走 `web/user/src/styles.css` 的 CSS 变量(`--bg-page`/`--text-strong`/`--border` 等),确保明暗主题正确切换;勿在组件内硬编码中性色

## 提交前自检
```bash
make fmt        # gofmt(若装了 goimports 一并)
make lint       # golangci-lint(若已安装),否则 go vet
make test       # 全部单元测试
make build-web  # 前端改动时
```
`make lint` 必须 **0 issues** 才可提交。CI(`.github/workflows/ci.yml`)会跑:Lint(golangci-lint)/ Go test(`-race` + 覆盖率上报 [Codecov](https://codecov.io/gh/aitoys/llm-gateway))/ 前端 build / Playwright E2E。PR 模板已含自检清单,请逐项确认。

## 提交信息
使用约定式提交:`feat: ...` / `fix: ...` / `docs: ...` / `refactor: ...` / `test: ...`

## 文档同步
涉及行为/配置/数据模型/部署变更时,同步更新 [`docs-site/`](./docs-site) 对应文档(`configuration.md`/`deployment.md`/`data-model.md` 等),避免文档与代码漂移。本地预览:`cd docs-site && npm install && npm run docs:dev`;`npm run docs:build` 做构建校验。

## 新增供应商
1. 若供应商提供 OpenAI 兼容端点:在 `cmd/gateway/main.go` 用 `openaicomp.New(name, baseURL)` 注册即可
2. 否则在 `internal/provider/<name>/` 新建包,实现 `provider.Provider` 接口(`Chat`/`ChatStream`/`Embeddings`),并在 main 中注册
3. 补 `httptest` 单元测试覆盖转换逻辑

## PR 流程
1. Fork → 新建分支 → 提交
2. 描述动机与测试方式
3. 关联相关 issue

## 行为准则
保持友善、对事不对人。技术讨论基于事实与数据。
