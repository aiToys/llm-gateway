// Package anthropic 实现 Anthropic 兼容入口(/v1/messages)。
// 入口把 Anthropic 请求转 canonical(OpenAI 格式);响应/流式按入口标记转回 Anthropic。
package anthropic

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aitoys/llm-gateway/internal/api/common"
	"github.com/aitoys/llm-gateway/internal/auth"
	"github.com/aitoys/llm-gateway/internal/canon"
	"github.com/aitoys/llm-gateway/internal/logging"
	"github.com/aitoys/llm-gateway/internal/relay"
	"github.com/gin-gonic/gin"
)

// Controller Anthropic 兼容入口。
type Controller struct{ Relay *relay.Service }

// --- Anthropic 请求/响应结构 ---

// block Anthropic content block: text / image / tool_use / tool_result。
// 字段按类型复用(tool_use 用 ID/Name/Input;tool_result 用 ToolUseID/Content)。
type block struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	// image
	Source json.RawMessage `json:"source,omitempty"`
	// tool_use(assistant 发起工具调用)
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`
	// tool_result(user 回传工具结果)
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"` // string | []block
}

type message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // string | []block
}

// anthropicTool Anthropic 工具定义(input_schema 对应 OpenAI function.parameters)。
type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// toolChoice Anthropic 工具选择(type: auto | any | tool | none)。
type toolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

type request struct {
	Model       string          `json:"model"`
	System      any             `json:"system,omitempty"` // string | []block
	Messages    []message       `json:"messages"`
	MaxTokens   int             `json:"max_tokens"`
	Temperature *float64        `json:"temperature,omitempty"`
	TopP        *float64        `json:"top_p,omitempty"`
	Stream      bool            `json:"stream,omitempty"`
	Stop        []string        `json:"stop_sequences,omitempty"`
	Tools       []anthropicTool `json:"tools,omitempty"`
	ToolChoice  *toolChoice     `json:"tool_choice,omitempty"`
}

// contentBlock Anthropic 响应 content block: text 或 tool_use。
type contentBlock struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input any    `json:"input,omitempty"`
}

type response struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Role       string         `json:"role"`
	Model      string         `json:"model"`
	Content    []contentBlock `json:"content"`
	StopReason string         `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

// Messages POST /v1/messages
func (c *Controller) Messages(g *gin.Context) {
	var req request
	if err := g.ShouldBindJSON(&req); err != nil {
		common.AnthropicError(g, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	sub, ok := auth.FromContext(g.Request.Context())
	if !ok {
		common.AnthropicError(g, http.StatusUnauthorized, "authentication_error", "missing subject")
		return
	}
	creq, err := toCanon(&req)
	if err != nil {
		common.AnthropicError(g, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	creq.Source = "anthropic"

	if req.Stream {
		c.stream(g, sub, creq)
		return
	}
	resp, meta, err := c.Relay.Chat(g.Request.Context(), sub, creq)
	if err != nil {
		c.writeErr(g, err)
		return
	}
	g.Header("X-Request-Id", meta.RequestID)
	g.JSON(http.StatusOK, fromCanon(resp))
}

func (c *Controller) stream(g *gin.Context, sub auth.Subject, req *canon.Request) {
	ch, meta, err := c.Relay.ChatStream(g.Request.Context(), sub, req)
	if err != nil {
		c.writeErr(g, err)
		return
	}
	g.Header("Content-Type", "text/event-stream")
	g.Header("Cache-Control", "no-cache")
	g.Header("Connection", "keep-alive")
	g.Header("X-Request-Id", meta.RequestID)

	// 累计用量:OpenAI 流式 usage 通常只在末帧出现,逐帧覆盖取最终值。
	var inputT, outT, cacheR, cacheW int
	var finished bool

	g.Stream(func(w io.Writer) bool {
		first := true
		// 输出 block 序号:文本块与每个 tool_use 块各占一个递增 index(Anthropic content block 序号)。
		blockIdx := 0
		textOpen := false
		textIdx := -1
		toolIdx := map[int]int{} // OpenAI toolcall.Index -> Anthropic block index
		toolOrder := []int{}     // tool_use 块开启顺序,保证 content_block_stop 按序(Anthropic SDK 用 index 跟踪状态机)
		finish := func(stopReason string) {
			if textOpen {
				writeSSE(w, "event: content_block_stop\n", fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, textIdx))
				textOpen = false
			}
			// 按开启顺序关闭 tool_use 块(map 遍历无序会让 SDK 报 "content_block_stop for unknown index")。
			for _, bi := range toolOrder {
				writeSSE(w, "event: content_block_stop\n", fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, bi))
			}
			// usage 同时回传 input/output 与缓存 token:Anthropic 计费依赖 cache_read_input_tokens / cache_creation_input_tokens。
			b, _ := json.Marshal(map[string]any{
				"type":  "message_delta",
				"delta": map[string]any{"stop_reason": stopReason},
				"usage": map[string]any{
					"input_tokens":                inputT,
					"output_tokens":               outT,
					"cache_read_input_tokens":     cacheR,
					"cache_creation_input_tokens": cacheW,
				},
			})
			fmt.Fprintf(w, "event: message_delta\ndata: %s\n\n", b)
			writeSSE(w, "event: message_stop\n", `{"type":"message_stop"}`)
			g.Writer.Flush()
			finished = true
		}
		for {
			select {
			case chunk, ok := <-ch:
				if !ok {
					// 上游关闭但未发 finish_reason(断流/超时/panic):兜底发 message_stop,
					// 否则 Anthropic SDK 状态机卡死等待 message_stop,且计费拿不到 usage。
					if !finished {
						if first {
							writeSSE(w, "event: message_start\n", fmt.Sprintf(`{"type":"message_start","message":{"id":%q,"type":"message","role":"assistant","model":%q,"content":[],"stop_reason":null,"usage":{"input_tokens":0,"output_tokens":0}}}`, meta.RequestID, req.Model))
						}
						finish("end_turn")
					}
					return false
				}
				if first {
					writeSSE(w, "event: message_start\n", fmt.Sprintf(`{"type":"message_start","message":{"id":%q,"type":"message","role":"assistant","model":%q,"content":[],"stop_reason":null,"usage":{"input_tokens":0,"output_tokens":0}}}`, meta.RequestID, req.Model))
					first = false
				}
				// 累计 usage(末帧覆盖;流首 message_start 暂以 0 占位,真实值在 message_delta 补全)。
				if chunk.Usage != nil {
					if chunk.Usage.PromptTokens > 0 {
						inputT = chunk.Usage.PromptTokens
					}
					if chunk.Usage.CompletionTokens > 0 {
						outT = chunk.Usage.CompletionTokens
					}
					if chunk.Usage.CacheReadTokens > 0 {
						cacheR = chunk.Usage.CacheReadTokens
					}
					if chunk.Usage.CacheWriteTokens > 0 {
						cacheW = chunk.Usage.CacheWriteTokens
					}
				}
				if len(chunk.Choices) > 0 {
					delta := chunk.Choices[0].Delta
					// 文本增量。
					if text := canon.TextContent(delta); text != "" {
						if !textOpen {
							textIdx = blockIdx
							blockIdx++
							writeSSE(w, "event: content_block_start\n", fmt.Sprintf(`{"type":"content_block_start","index":%d,"content_block":{"type":"text","text":""}}`, textIdx))
							textOpen = true
						}
						b, _ := json.Marshal(map[string]any{"type": "content_block_delta", "index": textIdx, "delta": map[string]any{"type": "text_delta", "text": text}})
						fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", b)
						g.Writer.Flush()
					}
					// 工具调用增量:每个 toolcall.Index 首帧(带 id/name)开 tool_use 块,后续 arguments 增量发 input_json_delta。
					for _, tc := range delta.ToolCalls {
						bi, seen := toolIdx[tc.Index]
						if !seen {
							// 开新 tool_use 块前先关闭进行中的文本块(Anthropic 块按序,文本与工具不交错)。
							if textOpen {
								writeSSE(w, "event: content_block_stop\n", fmt.Sprintf(`{"type":"content_block_stop","index":%d}`, textIdx))
								textOpen = false
							}
							bi = blockIdx
							blockIdx++
							toolIdx[tc.Index] = bi
							toolOrder = append(toolOrder, bi)
							start := map[string]any{"type": "content_block_start", "index": bi, "content_block": map[string]any{"type": "tool_use", "id": tc.ID, "name": tc.Function.Name, "input": map[string]any{}}}
							sb, _ := json.Marshal(start)
							fmt.Fprintf(w, "event: content_block_start\ndata: %s\n\n", sb)
						}
						if tc.Function.Arguments != "" {
							d, _ := json.Marshal(map[string]any{"type": "content_block_delta", "index": bi, "delta": map[string]any{"type": "input_json_delta", "partial_json": tc.Function.Arguments}})
							fmt.Fprintf(w, "event: content_block_delta\ndata: %s\n\n", d)
						}
						g.Writer.Flush()
					}
					if chunk.Choices[0].FinishReason != nil {
						finish(mapStop(*chunk.Choices[0].FinishReason))
						return false
					}
				}
			case <-g.Request.Context().Done():
				return false
			}
		}
	})
}

func writeSSE(w io.Writer, event, data string) {
	fmt.Fprintf(w, "%sdata: %s\n\n", event, data)
}

// toCanon Anthropic 请求 -> canonical(OpenAI 格式)。
// 含工具调用归一: tools(input_schema→parameters)、tool_choice、tool_use→ToolCalls、tool_result→role=tool。
func toCanon(r *request) (*canon.Request, error) {
	msgs := make([]canon.Message, 0, len(r.Messages)+1)
	if r.System != nil {
		sys := extractText(r.System)
		if sys != "" {
			msgs = append(msgs, canon.Message{Role: "system", Content: sys})
		}
	}
	for _, m := range r.Messages {
		expanded, err := convertMessage(m)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, expanded...)
	}
	creq := &canon.Request{
		Model:       r.Model,
		Messages:    msgs,
		Temperature: r.Temperature,
		TopP:        r.TopP,
		MaxTokens:   r.MaxTokens,
		Stream:      r.Stream,
		Stop:        r.Stop,
	}
	// tools: Anthropic input_schema -> OpenAI function.parameters(透传 JSON Schema)。
	if len(r.Tools) > 0 {
		tools := make([]canon.Tool, 0, len(r.Tools))
		for _, t := range r.Tools {
			tc := canon.Tool{Type: "function"}
			tc.Function.Name = t.Name
			tc.Function.Description = t.Description
			if len(t.InputSchema) > 0 {
				var schema any
				if json.Unmarshal(t.InputSchema, &schema) == nil {
					tc.Function.Parameters = schema
				}
			}
			tools = append(tools, tc)
		}
		creq.Tools = tools
		creq.ToolChoice = mapToolChoice(r.ToolChoice)
	}
	return creq, nil
}

// mapToolChoice Anthropic tool_choice -> OpenAI tool_choice。
// auto→"auto"; any→"required"(OpenAI 强制调用某工具的等价); tool{name}→{type:function,function:{name}}; none→"none"。
func mapToolChoice(tc *toolChoice) any {
	if tc == nil {
		return nil
	}
	switch tc.Type {
	case "auto":
		return "auto"
	case "any":
		return "required"
	case "none":
		return "none"
	case "tool":
		return map[string]any{"type": "function", "function": map[string]any{"name": tc.Name}}
	}
	return nil
}

// convertMessage Anthropic 消息 -> canonical 消息序列。
// 一条 Anthropic 消息可能展开为多条 OpenAI 消息:
//   - assistant 含 tool_use block -> 该消息带 ToolCalls(文本部分仍进 Content)
//   - user 含 tool_result block  -> 每个 tool_result 产出一条独立 role=tool 消息(OpenAI 模型)
func convertMessage(m message) ([]canon.Message, error) {
	// 纯字符串 content: 直通。
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return []canon.Message{{Role: m.Role, Content: s}}, nil
	}
	var bs []block
	if err := json.Unmarshal(m.Content, &bs); err != nil {
		return nil, fmt.Errorf("invalid content")
	}

	var parts []canon.ContentPart
	var toolCalls []canon.ToolCall
	var out []canon.Message
	for _, b := range bs {
		switch b.Type {
		case "text":
			parts = append(parts, canon.ContentPart{Type: "text", Text: b.Text})
		case "image":
			parts = append(parts, canon.ContentPart{Type: "image_url", ImageURL: &canon.ImageURL{URL: imageSourceToData(b.Source)}})
		case "tool_use":
			// assistant 发起的工具调用 -> OpenAI ToolCall(input 序列化为 arguments 字符串)。
			tc := canon.ToolCall{ID: b.ID, Type: "function"}
			tc.Function.Name = b.Name
			tc.Function.Arguments = string(b.Input)
			if tc.Function.Arguments == "" {
				tc.Function.Arguments = "{}"
			}
			toolCalls = append(toolCalls, tc)
		case "tool_result":
			// user 回传的工具结果 -> OpenAI role=tool 消息(content 取文本)。
			out = append(out, canon.Message{Role: "tool", ToolCallID: b.ToolUseID, Content: toolResultText(b.Content)})
		default:
			parts = append(parts, canon.ContentPart{Type: b.Type, Text: b.Text})
		}
	}

	// 主消息: 若有文本/图片部分,按原 role 产出;若仅有 tool_calls(assistant)也产出空 content 消息承载 ToolCalls。
	// 顺序: tool_result(role=tool)消息必须在前,主消息(user 文本 / assistant tool_calls)在后——
	// OpenAI 协议要求 tool 结果紧跟在被调用的 assistant(tool_calls) 之后,然后才是下一轮 user 文本。
	if len(parts) > 0 || len(toolCalls) > 0 {
		msg := canon.Message{Role: m.Role}
		if len(parts) > 0 {
			msg.Content = parts
		}
		msg.ToolCalls = toolCalls
		out = append(out, msg)
	}
	return out, nil
}

// imageSourceToData: Anthropic source{"base64",media_type,data} -> data: URI。
func imageSourceToData(src json.RawMessage) string {
	var s struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
		Url       string `json:"url"`
	}
	if err := json.Unmarshal(src, &s); err != nil {
		return ""
	}
	if s.Type == "url" {
		return s.Url
	}
	if s.Data != "" {
		return "data:" + s.MediaType + ";base64," + s.Data
	}
	return ""
}

func extractText(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []interface{}:
		var sb strings.Builder
		for _, it := range t {
			if mp, ok := it.(map[string]any); ok {
				if s, ok := mp["text"].(string); ok {
					sb.WriteString(s)
				}
			}
		}
		return sb.String()
	}
	return ""
}

// toolResultText 解析 tool_result.content(string 或 []block)为纯文本,作为 role=tool 消息内容。
func toolResultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var bs []block
	if json.Unmarshal(raw, &bs) == nil {
		var sb strings.Builder
		for _, b := range bs {
			if b.Type == "text" {
				sb.WriteString(b.Text)
			}
		}
		return sb.String()
	}
	return strings.TrimSpace(string(raw))
}

// fromCanon canonical 响应 -> Anthropic 响应。
// content 可能含多个 block: 文本 + 工具调用(tool_calls 各自成一个 tool_use block)。
func fromCanon(r *canon.Response) *response {
	out := &response{ID: r.ID, Type: "message", Role: "assistant", Model: r.Model}
	out.Content = []contentBlock{}
	if len(r.Choices) > 0 {
		msg := r.Choices[0].Message
		// 文本部分(若有)。
		if text := canon.TextContent(msg); text != "" {
			out.Content = append(out.Content, contentBlock{Type: "text", Text: text})
		}
		// 工具调用部分 -> tool_use block(input 反序列化为对象)。
		for _, tc := range msg.ToolCalls {
			var input any
			if tc.Function.Arguments != "" {
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &input)
			}
			if input == nil {
				input = map[string]any{}
			}
			out.Content = append(out.Content, contentBlock{Type: "tool_use", ID: tc.ID, Name: tc.Function.Name, Input: input})
		}
		out.StopReason = mapStop(r.Choices[0].FinishReason)
	}
	// 无任何 content 时补一个空文本 block(Anthropic 要求 content 非空)。
	if len(out.Content) == 0 {
		out.Content = append(out.Content, contentBlock{Type: "text", Text: ""})
	}
	out.Usage.InputTokens = r.Usage.PromptTokens
	out.Usage.OutputTokens = r.Usage.CompletionTokens
	return out
}

// mapStop OpenAI finish_reason -> Anthropic stop_reason。
func mapStop(finish string) string {
	switch finish {
	case "stop", "":
		return "end_turn"
	case "length":
		return "max_tokens"
	case "tool_calls":
		return "tool_use"
	}
	return "end_turn"
}

// writeErr 把 relay 错误映射为 Anthropic 风格的 HTTP 响应(顶层 type:"error")。
// 已知业务哨兵透出明确类型;其余(含上游响应体)脱敏为通用错误,避免泄露上游内部信息。
// 用 errors.Is 识别哨兵(容忍 %w 包装),回退到字符串相等以兼容历史路径。
func (c *Controller) writeErr(g *gin.Context, err error) {
	switch {
	case errors.Is(err, relay.ErrModelNotFound) || err.Error() == relay.ErrModelNotFound.Error():
		common.AnthropicError(g, http.StatusNotFound, "not_found_error", "model not found or disabled")
	case errors.Is(err, relay.ErrNoChannel) || err.Error() == relay.ErrNoChannel.Error():
		common.AnthropicError(g, http.StatusServiceUnavailable, "api_error", "no available channel for model")
	case errors.Is(err, relay.ErrInsufficientBal) || err.Error() == relay.ErrInsufficientBal.Error():
		common.AnthropicError(g, http.StatusPaymentRequired, "invalid_request_error", "insufficient balance")
	case errors.Is(err, relay.ErrQuotaExceeded) || err.Error() == relay.ErrQuotaExceeded.Error():
		common.AnthropicError(g, http.StatusTooManyRequests, "rate_limit_error", "usage quota exceeded")
	default:
		// 上游错误脱敏: 完整 err 仅记服务端日志,客户端只收到通用提示。
		logging.L().Warn("relay upstream error",
			"req_id", g.GetString("request_id"), "err", err.Error())
		common.AnthropicError(g, http.StatusBadGateway, "api_error", "upstream request failed")
	}
}

var _ = time.Now
