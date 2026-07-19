---
title: 双协议接入
---

# 双协议接入

LLM Gateway 对外同时提供 **OpenAI 兼容** 与 **Anthropic 兼容** 两套协议。现有 SDK（`openai`、`@anthropic-ai/sdk`、LangChain 等）只需把 `base_url` 指向网关即可零改动接入，鉴权使用本项目签发的 API Key（详见 [API 密钥与限流](./api-keys)）。

## 接入地址

| 协议 | base_url | 主要端点 |
| --- | --- | --- |
| OpenAI 兼容 | `https://gateway.example.com/v1` | `POST /v1/chat/completions`、`GET /v1/models` |
| Anthropic 兼容 | `https://gateway.example.com/v1` | `POST /v1/messages` |
| 文件 | `https://gateway.example.com/v1` | `POST /v1/files`、`GET /files/*path` |

所有推理端点都接受 `Authorization: Bearer sk-xxxx`。

## OpenAI 兼容

### curl 非流式

```bash
curl https://gateway.example.com/v1/chat/completions \
  -H "Authorization: Bearer sk-xxxxxxxxxxxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [{"role": "user", "content": "用一句话介绍 Go"}]
  }'
```

### curl 流式

加 `"stream": true` 即可获取 SSE 流：

```bash
curl https://gateway.example.com/v1/chat/completions \
  -H "Authorization: Bearer sk-xxxxxxxxxxxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "stream": true,
    "messages": [{"role": "user", "content": "讲个 50 字的故事"}]
  }'
```

### Python openai SDK

SDK 1.x，`base_url` 指向网关，`api_key` 填项目签发的 Key：

```python
from openai import OpenAI

client = OpenAI(
    base_url="https://gateway.example.com/v1",
    api_key="sk-xxxxxxxxxxxx",
)

# 非流式
resp = client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[{"role": "user", "content": "用一句话介绍 Go"}],
)
print(resp.choices[0].message.content)

# 流式
stream = client.chat.completions.create(
    model="gpt-4o-mini",
    stream=True,
    messages=[{"role": "user", "content": "讲个 50 字的故事"}],
)
for chunk in stream:
    delta = chunk.choices[0].delta.content
    if delta:
        print(delta, end="", flush=True)
```

## Anthropic 兼容

`POST /v1/messages` 兼容 Anthropic Messages API 形态，鉴权同样使用 `Authorization: Bearer sk-xxxx`（不需要 Anthropic 官方的 `x-api-key`）。

### curl 示例

```bash
curl https://gateway.example.com/v1/messages \
  -H "Authorization: Bearer sk-xxxxxxxxxxxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-5-sonnet-20241022",
    "max_tokens": 512,
    "messages": [{"role": "user", "content": "用一句话介绍 Go"}]
  }'
```

### 流式

加入 `"stream": true`，按 Anthropic SSE 事件规范（`message_start`、`content_block_delta`、`message_stop` 等）下发。

```bash
curl https://gateway.example.com/v1/messages \
  -H "Authorization: Bearer sk-xxxxxxxxxxxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-5-sonnet-20241022",
    "max_tokens": 512,
    "stream": true,
    "messages": [{"role": "user", "content": "讲个 50 字的故事"}]
  }'
```

### Python anthropic SDK

```python
from anthropic import Anthropic

client = Anthropic(
    base_url="https://gateway.example.com",
    auth_token="sk-xxxxxxxxxxxx",  # 注入到 Authorization: Bearer
)

msg = client.messages.create(
    model="claude-3-5-sonnet-20241022",
    max_tokens=512,
    messages=[{"role": "user", "content": "用一句话介绍 Go"}],
)
print(msg.content[0].text)
```

## 模型列表

`GET /v1/models` 返回当前 Key 所属租户**已启用**的模型（合并租户 `tenant_model_overrides` 后的结果）：

```bash
curl https://gateway.example.com/v1/models \
  -H "Authorization: Bearer sk-xxxxxxxxxxxx"
```

响应：

```json
{
  "object": "list",
  "data": [
    {
      "id": "gpt-4o-mini",
      "object": "model",
      "owned_by": "openai",
      "capabilities": ["text", "vision", "function_call"]
    },
    {
      "id": "claude-3-5-sonnet-20241022",
      "object": "model",
      "owned_by": "anthropic",
      "capabilities": ["text", "vision", "reasoning"]
    }
  ]
}
```

`capabilities` 为多标签：`text` / `vision` / `audio` / `file` / `function_call` / `reasoning` / `code` / `web_search`。

## 多模态

视觉/多模态走 OpenAI 风格的 `content` **数组**形式，每个元素是一个 part：

- `type: "text"` —— 文本
- `type: "image_url"` —— 图片，`image_url.url` 可为公网 URL 或 data URL
- 其他 part 类型按模型能力扩展（音频、文件等）

### curl 示例

```bash
curl https://gateway.example.com/v1/chat/completions \
  -H "Authorization: Bearer sk-xxxxxxxxxxxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [{
      "role": "user",
      "content": [
        {"type": "text", "text": "这张图里是什么？"},
        {"type": "image_url", "image_url": {"url": "https://example.com/cat.png"}}
      ]
    }]
  }'
```

### Python 示例

```python
client.chat.completions.create(
    model="gpt-4o-mini",
    messages=[{
        "role": "user",
        "content": [
            {"type": "text", "text": "这张图里是什么？"},
            {"type": "image_url",
             "image_url": {"url": "https://example.com/cat.png"}},
        ],
    }],
)
```

Anthropic 协议同样支持 `content` 数组（`type: "image"` + `source`），与官方 SDK 用法一致。

## 文件上传

需要引用本地文件的场景（如把图片交给模型）：

1. `POST /v1/files` 上传文件，获得可访问 URL。
2. 将 URL 放入多模态 `image_url.url` 字段调用推理。

### 上传

```bash
curl -X POST https://gateway.example.com/v1/files \
  -H "Authorization: Bearer sk-xxxxxxxxxxxx" \
  -F "file=@/path/to/cat.png" \
  -F "purpose=vision"
```

响应：

```json
{
  "id": "file_abc123",
  "url": "https://gateway.example.com/files/tenant-1/cat-20260714.png",
  "filename": "cat.png",
  "bytes": 102400,
  "content_type": "image/png"
}
```

### 引用上传后的文件

```bash
curl https://gateway.example.com/v1/chat/completions \
  -H "Authorization: Bearer sk-xxxxxxxxxxxx" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-4o-mini",
    "messages": [{
      "role": "user",
      "content": [
        {"type": "text", "text": "描述这张图"},
        {"type": "image_url",
         "image_url": {"url": "https://gateway.example.com/files/tenant-1/cat-20260714.png"}}
      ]
    }]
  }'
```

`GET /files/*path` 按相对路径读取已上传文件，鉴权同样要求携带有效 Key/JWT。

## 零改动接入清单

| 接入方式 | 改动点 |
| --- | --- |
| OpenAI SDK / LangChain OpenAI | `base_url` 指向网关，`api_key` 换成本项目 Key |
| Anthropic SDK | `base_url` 指向网关，用 `auth_token` 注入 Key |
| curl / 原生 HTTP | URL 改为网关域名，`Authorization` 用本项目 Key |
| 现有应用代码 | 业务逻辑、消息结构、流式处理代码完全保留 |

## 控制面 REST 响应约定

管理端 / 用户端控制台调用的 `/api/*` REST 接口遵循以下响应格式约定(便于前端统一处理):

**成功(带数据)** — `{"data": <payload>}`,标准格式:
```json
{ "data": { "id": "ch_xxx", "name": "OpenAI 主", "provider": "openai" } }
```

**成功(无数据)** — `{"ok": true}`,用于 PUT/DELETE 等只需状态的操作。

**认证** — `POST /api/auth/login` 返回顶层 `{"token","user","expires_at}`(业界惯例,前端直接取,不走 `data` 包装)。

**失败** — `{"error": "<友好中文提示>"}`(4xx 业务错误);`{"error":"internal_error"}`(5xx,脱敏,完整 err 记服务端日志带 request_id)。字段校验错误已中文化(如 `"Name 不能为空"`),不暴露 gin validator 内部结构。

**前端统一访问**:两端 `api.js` 提供 `unwrap(resp)` helper —— 优先取 `body.data`,无则回退 `body`,新代码用它消除 `data.data.xxx` 与 `data.xxx` 的不一致:

```js
import { api, unwrap } from '../api.js'
const list = unwrap(await api.channels())   // 一律拿到数据,无需关心包装层
```

**后端约定**:新接口返回数据用 `s.okData(g, data)`,无数据操作用 `s.ok(g)`,请求绑定用 `s.bindJSON(g, &req)`(均见 `internal/api/web/respond.go`)。历史顶层响应(login / 早期 `{id}`/`{ok}`)保留兼容,新增接口一律走 `{data:...}`。
