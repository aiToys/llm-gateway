# 安全策略 / Security Policy

## 报告漏洞

感谢你帮助提升 LLM Gateway 的安全性。**请勿在公开 Issue 中提交安全漏洞**。

请通过以下任一方式私下报告:

- GitHub Security Advisory(推荐):仓库 → Security → Report a vulnerability
- 邮件:发送至仓库维护者(见 README 中的联系方式),标题前缀 `[SECURITY]`

我们会在 **3 个工作日内**确认收到,并在 **14 天内**给出评估与修复计划。

## 支持的版本

仅对最新发布版本提供安全修复。

## 生产部署安全清单

部署前务必(详见 README "生产安全须知"):

- `dev: false`(生产绝不开启 dev 模式,否则密钥校验放宽、自助充值/注册放开)
- 设置唯一的 `auth.jwt_secret`(≥ 16 字符,建议 ≥ 32 字节随机)
- 设置唯一的 `auth.channel_key_master`(32 字节 hex,`openssl rand -hex 32`)
- `auth.allow_signup: false`(按需开放)
- 通过 `auth.bootstrap_admin` 或 `--seed` 建立首个平台管理员
- 反向代理层启用 TLS、限流与访问控制;管理端 `/api/admin/*` 限制内网访问

## 范围

- LLM Gateway 自身代码漏洞(鉴权绕过、注入、计费/资金逻辑、信息泄露等)
- 默认配置下的不安全行为

不在范围内:用户自有密钥泄露、上游供应商故障、依赖库的已知 CVE(请直接报告给上游)。
