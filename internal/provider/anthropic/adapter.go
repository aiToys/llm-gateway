// Package anthropic 实现对 Anthropic 原生 API(api.anthropic.com /v1/messages)的出口适配器。
// 入口(/v1/messages)已实现 Anthropic↔canon 转换;此处为出口侧: canon→Anthropic 请求、
// Anthropic 响应/流式→canon。使网关能直连 Claude 官方 API 作为上游供应商,无需经 OpenAI 兼容中转。
package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aitoys/llm-gateway/internal/canon"
	"github.com/aitoys/llm-gateway/internal/provider"
)

// anthropic-version 头: Anthropic API 要求,固定为稳定版。
const apiVersion = "2023-06-01"

// Adapter Anthropic 原生出口适配器。
type Adapter struct {
	name           string
	defaultBaseURL string
	client         *http.Client // 非流式:整体超时 5min
	streamClient   *http.Client // 流式:无整体超时(长对话/reasoning 流不被硬截断)
}

// New 构造 Anthropic 适配器;defaultBaseURL 通常为 https://api.anthropic.com。
func New(name, defaultBaseURL string) *Adapter {
	// DisableKeepAlives: 与 openaicomp 同理,避免对端关闭空闲连接导致 EOF 复用问题。
	transport := func() *http.Transport {
		return &http.Transport{DisableKeepAlives: true, ResponseHeaderTimeout: 30 * time.Second}
	}
	return &Adapter{
		name:           name,
		defaultBaseURL: defaultBaseURL,
		client:         &http.Client{Timeout: 5 * time.Minute, Transport: transport()},
		streamClient:   &http.Client{Transport: transport()}, // 无整体 Timeout;由请求 ctx 控制
	}
}

func (a *Adapter) Name() string { return a.name }

func (a *Adapter) baseURL(ch *provider.Channel) string {
	if ch.BaseURL != "" {
		return strings.TrimRight(ch.BaseURL, "/")
	}
	return strings.TrimRight(a.defaultBaseURL, "/")
}

// do 发 POST,返回状态码+响应体。鉴权用 x-api-key + anthropic-version(非 Bearer)。
func (a *Adapter) do(ctx context.Context, ch *provider.Channel, path string, body []byte) (*http.Response, []byte, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL(ch)+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", ch.APIKey)
	req.Header.Set("anthropic-version", apiVersion)
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close() //nolint:errcheck // 错误响应体读取后立即关闭,关闭错误无意义
	return resp, raw, nil
}

// Chat 非流式: canon→Anthropic /v1/messages,响应→canon。
func (a *Adapter) Chat(ctx context.Context, ch *provider.Channel, req *canon.Request) (*canon.Response, error) {
	payload, err := canonToAnthropicReq(req, false)
	if err != nil {
		return nil, err
	}
	resp, raw, err := a.do(ctx, ch, "/v1/messages", payload)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, &canon.UpstreamError{Provider: a.name, StatusCode: resp.StatusCode, Body: raw, ContentType: resp.Header.Get("Content-Type"), RetryAfter: resp.Header.Get("Retry-After")}
	}
	out, err := anthropicRespToCanon(raw, req.Model)
	if err != nil {
		return nil, fmt.Errorf("decode %s response: %w (body: %s)", a.name, err, canon.Snippet(raw))
	}
	return out, nil
}

// ChatStream 流式: 解析 Anthropic SSE 事件流,转 canon.StreamChunk。
func (a *Adapter) ChatStream(ctx context.Context, ch *provider.Channel, req *canon.Request) (<-chan *canon.StreamChunk, error) {
	payload, err := canonToAnthropicReq(req, true)
	if err != nil {
		return nil, err
	}
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL(ch)+"/v1/messages", bytes.NewReader(payload))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", ch.APIKey)
	httpReq.Header.Set("anthropic-version", apiVersion)
	httpReq.Header.Set("Accept", "text/event-stream")
	resp, err := a.streamClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close() //nolint:errcheck
		return nil, &canon.UpstreamError{Provider: a.name, StatusCode: resp.StatusCode, Body: raw, ContentType: resp.Header.Get("Content-Type"), RetryAfter: resp.Header.Get("Retry-After")}
	}
	out := make(chan *canon.StreamChunk, 16)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		// 工具调用入参按 block index 累积;Anthropic input_json_delta 分片到达,需按 index 聚合后整段发。
		toolArgs := map[int]string{}
		toolMeta := map[int]struct{ id, name string }{} // content_block_start 时记录 id/name
		var msgID string
		// prompt(input) tokens 仅在 message_start.message.usage 中出现一次;message_delta.usage 只含
		// output/cache。缓存此值,在末帧 message_delta 时与 output 合并,否则流式 PromptTokens 恒为 0 → 漏算输入计费。
		var promptTokens int
		roleSent := false // 是否已投递首帧 role:assistant(OpenAI SDK 状态机要求)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" {
				continue
			}
			var ev evLite
			if json.Unmarshal([]byte(data), &ev) == nil {
				a.emitChunk(ctx, out, &ev, toolArgs, toolMeta, &msgID, req.Model, &promptTokens, &roleSent)
			}
		}
		if err := sc.Err(); err != nil {
			select {
			case out <- &canon.StreamChunk{StreamError: err.Error()}:
			case <-ctx.Done():
			}
		}
	}()
	return out, nil
}

// emitChunk 按事件类型转 canon chunk 并投递(out 满时尊重 ctx,不丢消息)。
func (a *Adapter) emitChunk(ctx context.Context, out chan<- *canon.StreamChunk, ev *evLite, toolArgs map[int]string, toolMeta map[int]struct{ id, name string }, msgID *string, model string, promptTokens *int, roleSent *bool) {
	switch ev.Type {
	case "message_start":
		var m struct {
			ID    string `json:"id"`
			Usage *struct {
				InputTokens int `json:"input_tokens"`
			} `json:"usage"`
		}
		_ = json.Unmarshal(ev.Msg, &m)
		*msgID = m.ID
		if m.Usage != nil {
			*promptTokens = m.Usage.InputTokens
		}
		// 首帧投递 role:assistant 占位(OpenAI SDK 状态机要求首帧 delta 含 role),
		// 与 openaicomp/mock 行为对齐。
		if !*roleSent {
			*roleSent = true
			select {
			case out <- &canon.StreamChunk{ID: *msgID, Object: "chat.completion.chunk", Model: model,
				Choices: []canon.StreamChoice{{Index: 0, Delta: canon.Message{Role: "assistant"}}}}:
			case <-ctx.Done():
			}
		}
	case "content_block_start":
		var cb struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		_ = json.Unmarshal(ev.ContentBlock, &cb)
		if cb.Type == "tool_use" {
			toolMeta[ev.Index] = struct{ id, name string }{cb.ID, cb.Name}
		}
	case "content_block_delta":
		var d struct {
			Type        string `json:"type"`
			Text        string `json:"text"`
			PartialJSON string `json:"partial_json"`
		}
		_ = json.Unmarshal(ev.Delta, &d)
		if d.Type == "text_delta" && d.Text != "" {
			select {
			case out <- &canon.StreamChunk{ID: *msgID, Object: "chat.completion.chunk", Model: model,
				Choices: []canon.StreamChoice{{Index: 0, Delta: canon.Message{Content: d.Text}}}}:
			case <-ctx.Done():
			}
		} else if d.Type == "input_json_delta" && d.PartialJSON != "" {
			toolArgs[ev.Index] += d.PartialJSON
		}
	case "content_block_stop":
		// tool_use block 结束: 把累积的 arguments 整段发出(OpenAI 流式 tool_calls 增量)。
		if meta, ok := toolMeta[ev.Index]; ok {
			args := toolArgs[ev.Index]
			if args == "" {
				args = "{}"
			}
			tc := canon.ToolCall{Index: ev.Index, ID: meta.id, Type: "function"}
			tc.Function.Name = meta.name
			tc.Function.Arguments = args
			select {
			case out <- &canon.StreamChunk{ID: *msgID, Object: "chat.completion.chunk", Model: model,
				Choices: []canon.StreamChoice{{Index: 0, Delta: canon.Message{ToolCalls: []canon.ToolCall{tc}}}}}:
			case <-ctx.Done():
			}
			delete(toolMeta, ev.Index)
			delete(toolArgs, ev.Index)
		}
	case "message_delta":
		// 末帧: stop_reason + usage。input_tokens 在此处恒为 0(Anthropic 只在 message_start 给),
		// 用 message_start 缓存的 promptTokens 补齐,否则漏算输入计费。
		var d struct {
			StopReason string `json:"stop_reason"`
		}
		_ = json.Unmarshal(ev.Delta, &d)
		fr := stopToFinish(d.StopReason)
		in := *promptTokens
		outT := 0
		var cr, cw int
		if ev.Usage != nil {
			outT = ev.Usage.OutputTokens
			cr = ev.Usage.CacheReadInputTokens
			cw = ev.Usage.CacheCreationInputTokens
		}
		u := &canon.Usage{
			PromptTokens:     in,
			CompletionTokens: outT,
			TotalTokens:      in + outT,
			CacheReadTokens:  cr,
			CacheWriteTokens: cw,
		}
		select {
		case out <- &canon.StreamChunk{ID: *msgID, Object: "chat.completion.chunk", Model: model, Usage: u,
			Choices: []canon.StreamChoice{{Index: 0, FinishReason: &fr}}}:
		case <-ctx.Done():
		}
	}
}

// evLite 提取流式事件公共字段(避免重复定义)。
type evLite struct {
	Type  string          `json:"type"`
	Msg   json.RawMessage `json:"message"`
	Delta json.RawMessage `json:"delta"`
	Usage *struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
	Index        int             `json:"index"`
	ContentBlock json.RawMessage `json:"content_block"`
}

// Embeddings Anthropic 无 embeddings 接口。
func (a *Adapter) Embeddings(ctx context.Context, ch *provider.Channel, input []string, model string) ([][]float32, *canon.Usage, error) {
	return nil, nil, errors.New("anthropic: embeddings not supported")
}

// canonToAnthropicReq canon.Request → Anthropic /v1/messages 请求体。
//
// system(developer 视为 system)单独提取;OpenAI tool/function 结果消息转 user+tool_result block。
// 连续多条 tool 结果合并为单条 user 消息的多个 tool_result block(Anthropic 要求同一轮工具结果
// 同属一条 user 消息,拆成多条孤立 user 消息部分模型会拒绝)。
func canonToAnthropicReq(req *canon.Request, stream bool) ([]byte, error) {
	msgs := make([]map[string]any, 0, len(req.Messages))
	var system string
	// pendingToolResults 累积连续 tool 结果;遇到非 tool 消息时先 flush 为一条 user 消息。
	var pendingToolResults []map[string]any
	flushTools := func() {
		if len(pendingToolResults) == 0 {
			return
		}
		msgs = append(msgs, map[string]any{"role": "user", "content": pendingToolResults})
		pendingToolResults = nil
	}
	roleFor := func(r string) string {
		switch r {
		case "assistant":
			return "assistant"
		default: // user / 未知角色按 user 处理
			return "user"
		}
	}
	for _, m := range req.Messages {
		switch m.Role {
		case "system", "developer":
			system += canon.TextContent(m)
			continue
		case "tool", "function":
			// OpenAI tool/function 结果 → Anthropic user + tool_result block(累积,最后统一 flush)。
			pendingToolResults = append(pendingToolResults, map[string]any{
				"type": "tool_result", "tool_use_id": m.ToolCallID, "content": canon.TextContent(m),
			})
			continue
		}
		flushTools()
		entry := map[string]any{"role": roleFor(m.Role), "content": canonContentToAnthropic(m)}
		msgs = append(msgs, entry)
	}
	flushTools()
	out := map[string]any{
		"model":      req.Model,
		"messages":   msgs,
		"max_tokens": maxTokens(req),
		"stream":     stream,
	}
	if system != "" {
		out["system"] = system
	}
	if req.Temperature != nil {
		out["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		out["top_p"] = *req.TopP
	}
	if len(req.Stop) > 0 {
		out["stop_sequences"] = req.Stop
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, t := range req.Tools {
			tools = append(tools, map[string]any{
				"name": t.Function.Name, "description": t.Function.Description,
				"input_schema": orEmptySchema(t.Function.Parameters),
			})
		}
		out["tools"] = tools
		if tc := canonToolChoice(req.ToolChoice); tc != nil {
			out["tool_choice"] = tc
		}
	}
	return json.Marshal(out)
}

// canonContentToAnthropic canon.Message.Content+ToolCalls → Anthropic content blocks。
// 经 canon.AsParts 归一,兼容入口 JSON 绑定产生的 []interface{} 形态(否则多模态全丢)。
func canonContentToAnthropic(m canon.Message) []map[string]any {
	blocks := []map[string]any{}
	for _, p := range canon.AsParts(m) {
		switch p.Type {
		case "text":
			if p.Text != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": p.Text})
			}
		case "image_url":
			if p.ImageURL != nil {
				blocks = append(blocks, map[string]any{"type": "image", "source": imageURLToSource(p.ImageURL.URL)})
			}
		case "input_audio":
			if p.InputAudio != nil {
				mt := "audio/" + p.InputAudio.Format
				blocks = append(blocks, map[string]any{"type": "audio", "source": map[string]any{
					"type": "base64", "media_type": mt, "data": p.InputAudio.Data,
				}})
			}
		}
	}
	for _, tc := range m.ToolCalls {
		args := tc.Function.Arguments
		if args == "" {
			args = "{}"
		}
		var input any
		_ = json.Unmarshal([]byte(args), &input)
		if input == nil {
			input = map[string]any{}
		}
		blocks = append(blocks, map[string]any{"type": "tool_use", "id": tc.ID, "name": tc.Function.Name, "input": input})
	}
	if len(blocks) == 0 {
		blocks = append(blocks, map[string]any{"type": "text", "text": ""})
	}
	return blocks
}

// imageURLToSource OpenAI image_url(http/data:base64) → Anthropic source。
func imageURLToSource(u string) map[string]any {
	if strings.HasPrefix(u, "data:") {
		// data:image/png;base64,XXXX
		rest := strings.TrimPrefix(u, "data:")
		semi := strings.Index(rest, ";")
		comma := strings.Index(rest, ",")
		if semi > 0 && comma > semi {
			return map[string]any{
				"type": "base64", "media_type": rest[:semi], "data": rest[comma+1:],
			}
		}
	}
	return map[string]any{"type": "url", "url": u}
}

// anthropicRespToCanon Anthropic 非流式响应 → canon.Response。
func anthropicRespToCanon(raw []byte, model string) (*canon.Response, error) {
	var r struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, err
	}
	msg := canon.Message{Role: "assistant"}
	var sb strings.Builder
	for _, b := range r.Content {
		switch b.Type {
		case "text":
			sb.WriteString(b.Text)
		case "tool_use":
			args := string(b.Input)
			if args == "" {
				args = "{}"
			}
			msg.ToolCalls = append(msg.ToolCalls, canon.ToolCall{ID: b.ID, Type: "function", Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: b.Name, Arguments: args}})
		}
	}
	msg.Content = sb.String()
	out := &canon.Response{
		ID: r.ID, Object: "chat.completion", Model: ifEmpty(model, r.Model),
		Choices: []canon.Choice{{Index: 0, Message: msg, FinishReason: stopToFinish(r.StopReason)}},
		Usage: canon.Usage{
			PromptTokens:     r.Usage.InputTokens,
			CompletionTokens: r.Usage.OutputTokens,
			TotalTokens:      r.Usage.InputTokens + r.Usage.OutputTokens,
			CacheReadTokens:  r.Usage.CacheReadInputTokens,
			CacheWriteTokens: r.Usage.CacheCreationInputTokens,
		},
	}
	return out, nil
}

// canonToolChoice OpenAI tool_choice → Anthropic tool_choice。
// 支持字符串简写("auto"/"none"/"required",OpenAI 协议允许)与对象形式。
func canonToolChoice(tc any) map[string]any {
	if s, ok := tc.(string); ok {
		switch s {
		case "auto":
			return map[string]any{"type": "auto"}
		case "none":
			return map[string]any{"type": "none"}
		case "required":
			return map[string]any{"type": "any"}
		}
		return nil
	}
	m, ok := tc.(map[string]any)
	if !ok {
		return nil
	}
	switch m["type"] {
	case "auto":
		return map[string]any{"type": "auto"}
	case "none":
		return map[string]any{"type": "none"}
	case "required":
		return map[string]any{"type": "any"}
	case "function":
		if fn, ok := m["function"].(map[string]any); ok {
			return map[string]any{"type": "tool", "name": fn["name"]}
		}
	}
	return nil
}

// maxTokens Anthropic 必填且需 >0;canon 未设(或非法值如 -1,部分客户端用 -1 表"不限")则默认 4096。
func maxTokens(req *canon.Request) int {
	if req.MaxTokens > 0 {
		return req.MaxTokens
	}
	return 4096
}

func stopToFinish(stop string) string {
	switch stop {
	case "end_turn", "stop_sequence", "":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	}
	return "stop"
}

func orEmptySchema(v any) any {
	if v == nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	return v
}

func ifEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
