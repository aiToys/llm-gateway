---
title: 租户模型启停
---

# 租户模型启停

租户管理员可在用户端「模型管理」页对**平台可用模型**进行按需启停。被关闭的模型不会出现在本工作空间的可用列表中，所有成员调用该模型时一律被拒绝。

## 入口

用户端左侧导航 **模型管理**（路径 `/console/models`）以卡片/表格形式列出平台当前注册的全部模型，每条记录包含：

- 模型名称（如 `gpt-4o-mini`）
- 供应商标签
- 能力标签（`text` / `vision` / `audio` / `file` / `function_call` / `reasoning` / `code` / `web_search`）
- 启用状态开关
- 计费信息（ EffectivePrice，按启用后实际生效价格计费）

## 启停开关的影响范围

- **作用域**：仅作用于**当前工作空间（租户）**及其全部成员，不影响其他租户。
- **生效时机**：开关保存后立即生效，下一次推理请求即按新状态判定。
- **调用被拒**：模型关闭后，任何成员使用该模型发起推理，网关返回错误：

```json
{
  "error": {
    "message": "model not found or disabled",
    "type": "invalid_request_error"
  }
}
```

- **计费**：启用状态下按 `EffectivePrice`（平台定价经过租户策略后的实际生效价）计费；关闭后既不可调用，也不产生任何费用。

## 启停机制

- 后端通过 `tenant_model_overrides.enabled` 字段记录每个租户对每个模型的启停偏好。
- 未显式设置的模型默认继承平台状态（默认可用）。
- 列表接口（`GET /v1/models`、用户端模型列表）会合并 `tenant_model_overrides`，仅返回当前租户**已启用**的模型。
- 推理链路在路由匹配后再次校验 `tenant_model_overrides.enabled`，确保关闭状态在并发更新下也安全拦截。

## 通过 API 启停模型

除在用户端点击开关外，租户管理员可通过以下接口编程控制：

### 关闭某个模型

```bash
curl -X PUT "https://gateway.example.com/api/me/models/gpt-4o-mini/enabled" \
  -H "Authorization: Bearer <JWT>" \
  -H "Content-Type: application/json" \
  -d '{"enabled": false}'
```

### 重新启用某个模型

```bash
curl -X PUT "https://gateway.example.com/api/me/models/gpt-4o-mini/enabled" \
  -H "Authorization: Bearer <JWT>" \
  -H "Content-Type: application/json" \
  -d '{"enabled": true}'
```

### Python 示例

```python
import httpx

base_url = "https://gateway.example.com"
token = "<JWT>"  # 用户端登录后的 JWT

def set_model_enabled(model: str, enabled: bool) -> None:
    resp = httpx.put(
        f"{base_url}/api/me/models/{model}/enabled",
        headers={"Authorization": f"Bearer {token}"},
        json={"enabled": enabled},
    )
    resp.raise_for_status()

# 关闭工作空间内对 gpt-4o-mini 的访问
set_model_enabled("gpt-4o-mini", False)
```

### 响应

```json
{ "ok": true, "model": "gpt-4o-mini", "enabled": false }
```

## 最佳实践

- 用能力标签筛选模型，按团队实际需要保留最小可用集合，避免误调用高价模型。
- 为不同工作空间设置不同的启停组合，实现「开发/生产」「按部门」隔离。
- 关闭模型不会删除历史用量数据，可随时重新启用。
