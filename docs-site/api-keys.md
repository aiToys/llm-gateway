---
title: API 密钥与限流
---

# API 密钥与限流

推理接口（`/v1/chat/completions`、`/v1/messages`、`/v1/files`）通过 **API Key** 鉴权；用户端 Web 控制台则使用 JWT。两者职责分离：API Key 用于服务端到服务端、SDK、脚本等长期调用场景。

## 创建 API Key

在用户端「API 密钥」页新建 Key 时需要填写：

- **名称**：便于识别用途（如 `prod-server`、`ci-batch`）
- **RPM 限流**：每分钟最大请求数，`0` 表示不限
- **TPM 限流**：每分钟最大 token 数，`0` 表示不限
- **模型白名单**：限制该 Key 只能调用指定模型（留空或全选表示继承租户已启用模型）
- **作用域（scopes）**：限制该 Key 的权限范围（如只读、仅推理等），留空表示默认推理权限

### 明文仅显示一次

创建成功后，**明文 Key 只会在弹窗中显示这一次**，关闭弹窗后无法再次查看。请立即复制并妥善保存到密钥管理器（如 Vault、AWS Secrets Manager、1Password）。

::: warning 明文不存储
服务端**不保存 Key 明文**，仅保存 Argon2id 哈希值。任何丢失的 Key 都**无法找回**，只能重新创建。
:::

Key 格式以 `sk-` 开头，形如：

```
sk-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
```

## 鉴权方式

调用推理接口时，通过 `Authorization: Bearer <API Key>` 携带：

```bash
curl https://gateway.example.com/v1/chat/completions \
  -H "Authorization: Bearer sk-xxxxxxxxxxxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [{"role": "user", "content": "你好"}]
  }'
```

## 限流机制

所有限流/配额均**仅在 API Key 维度**生效（`Subject.APIKeyID` 非空时才检查），不区分租户或渠道。每分钟级走 RPM/TPM，更长周期走日/月配额，形成「分钟 → 天 → 月」递进：

| 配额字段 | Redis 键前缀 | 含义 |
| --- | --- | --- |
| `rpm_limit` | `rl:rpm:{keyid}:{YYYYMMDDHHMM}` | 每分钟请求数（进入网关即 INCR 预检） |
| `tpm_limit` | `rl:tpm:{keyid}:{YYYYMMDDHHMM}` | 每分钟 token 数（推理完成后补登，预判当前桶累计） |
| `daily_request_limit` | `quota:req:d:{keyid}:{YYYYMMDD}` | 每日请求数（0=不限，TTL 25h 跨自然日滚动） |
| `monthly_request_limit` | `quota:req:m:{keyid}:{YYYYMM}` | 每月请求数（0=不限，TTL 32d 跨自然月滚动） |
| `daily_token_limit` / `monthly_token_limit` | `quota:tok:d:{keyid}:…` / `quota:tok:m:{keyid}:…` | 日/月 token 上限：**字段存在但中间件尚未消费**，预留扩展 |

`0` 表示不限。桶键集群共享，所有节点一致。

### 超限响应

超限时返回 HTTP `429 Too Many Requests`，**未设置 `Retry-After` 头**：

```json
// RPM/TPM 超限
{
  "error": {
    "type": "rate_limit_exceeded",
    "message": "RPM limit exceeded"
  }
}

// 日/月配额超限
{
  "error": {
    "type": "quota_exceeded",
    "message": "daily request quota exceeded"
  }
}
```

## IP 白名单

每个 API Key 可配置 `ip_whitelist`（`text[]`，空数组=不限）。非空时，请求来源 IP 必须命中列表中的单 IP 或 CIDR 才放行，否则直接拒绝。中间件 (`middleware/auth.go ipAllowed`) 在鉴权阶段强制校验，适合把长期密钥锁定到固定出口 IP（如堡垒机、生产服务器）。

## 模型白名单

- 创建 Key 时可指定 `models` 白名单，**只允许调用列表中的模型**。
- 白名单与租户级「模型启停」叠加生效：调用必须**同时**通过 Key 白名单和租户 `tenant_model_overrides.enabled` 校验。
- 留空表示继承租户已启用模型集合，不做额外限制。

## Python 调用示例

使用 `openai` SDK 直接对接（详见 [双协议接入](./api-compat)）：

```python
from openai import OpenAI

client = OpenAI(
    base_url="https://gateway.example.com/v1",
    api_key="sk-xxxxxxxxxxxx",  # 仅在受控环境通过环境变量注入
)

resp = client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[{"role": "user", "content": "你好"}],
)
print(resp.choices[0].message.content)
```

推荐通过环境变量注入而非硬编码：

```python
import os
client = OpenAI(
    base_url=os.environ["LLM_GATEWAY_BASE_URL"],
    api_key=os.environ["LLM_GATEWAY_API_KEY"],
)
```

## 安全建议

- **按用途隔离**：不同服务/环境使用不同 Key，便于单独吊销与审计。
- **最小白名单**：仅授权必需模型，降低泄漏后影响面。
- **合理限流**：即使 Key 泄漏，RPM/TPM 上限也能抑制刷量损失。
- **及时轮换**：发现可疑调用立即在用户端删除 Key 并新建。
- **日志脱敏**：不要把完整 Key 打到日志，记录前 8 位即可定位。
