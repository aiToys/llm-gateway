// Package mock 提供开发/测试用的 Mock Provider,实现 provider.Provider 接口。
// 行为: 回显输入,确定性生成回复;支持流式;支持多模态输入(返回占位描述);
// usage 由回复长度按 CharsPerToken 估算,便于计费链路验证。
package mock

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aitoys/llm-gateway/internal/canon"
	"github.com/aitoys/llm-gateway/internal/provider"
)

const Name = "mock"

// Provider Mock 供应商。
type Provider struct {
	CharsPerToken int
	Latency       time.Duration // 每帧延迟(模拟)
}

func New() *Provider { return &Provider{CharsPerToken: 2, Latency: 5 * time.Millisecond} }

func (p *Provider) Name() string { return Name }

func (p *Provider) Chat(ctx context.Context, ch *provider.Channel, req *canon.Request) (*canon.Response, error) {
	// 带 tools 的请求: 演示工具调用(返回首个工具的示例调用),便于端到端验证 tool_use 归一。
	if len(req.Tools) > 0 {
		return p.toolCallReply(req), nil
	}
	reply := p.buildReply(req)
	usage := p.usage(req, reply)
	return &canon.Response{
		ID:      "mock-" + randID(),
		Object:  "chat.completion",
		Created: nowUnix(),
		Model:   req.Model,
		Choices: []canon.Choice{{
			Index:        0,
			Message:      canon.Message{Role: "assistant", Content: reply},
			FinishReason: "stop",
		}},
		Usage: usage,
	}, nil
}

// toolCallReply 构造一个工具调用响应(调用请求中的首个工具,带示例参数)。
func (p *Provider) toolCallReply(req *canon.Request) *canon.Response {
	name := req.Tools[0].Function.Name
	tc := canon.ToolCall{ID: "call-mock-" + randID(), Type: "function", Index: 0}
	tc.Function.Name = name
	tc.Function.Arguments = `{"city":"北京"}`
	return &canon.Response{
		ID: "mock-" + randID(), Object: "chat.completion", Created: nowUnix(), Model: req.Model,
		Choices: []canon.Choice{{
			Index:        0,
			Message:      canon.Message{Role: "assistant", ToolCalls: []canon.ToolCall{tc}},
			FinishReason: "tool_calls",
		}},
		Usage: canon.Usage{PromptTokens: 12, CompletionTokens: 6, TotalTokens: 18},
	}
}

func (p *Provider) ChatStream(ctx context.Context, ch *provider.Channel, req *canon.Request) (<-chan *canon.StreamChunk, error) {
	// 带 tools: 流式产出 tool_call 增量(首帧 id/name,次帧 arguments 片段),验证 input_json_delta 归一。
	if len(req.Tools) > 0 {
		return p.toolCallStream(req)
	}
	reply := p.buildReply(req)
	out := make(chan *canon.StreamChunk, 8)
	go func() {
		defer close(out)
		id := "mock-" + randID()
		// 按 token(粗粒度: 每 4 字符)切片发送。
		tokens := tokenize(reply, 4)
		for _, tok := range tokens {
			select {
			case <-ctx.Done():
				return
			case <-time.After(p.Latency):
			}
			out <- &canon.StreamChunk{
				ID: id, Object: "chat.completion.chunk", Created: nowUnix(), Model: req.Model,
				Choices: []canon.StreamChoice{{Index: 0, Delta: canon.Message{Role: "assistant", Content: tok}}},
			}
		}
		fr := "stop"
		usage := p.usage(req, reply)
		out <- &canon.StreamChunk{
			ID: id, Object: "chat.completion.chunk", Created: nowUnix(), Model: req.Model,
			Choices: []canon.StreamChoice{{Index: 0, Delta: canon.Message{}, FinishReason: &fr}},
			Usage:   &usage,
		}
	}()
	return out, nil
}

// toolCallStream 流式产出工具调用: 首帧开 tool_call(id/name),次帧发 arguments 增量,末帧 finish=tool_calls。
func (p *Provider) toolCallStream(req *canon.Request) (<-chan *canon.StreamChunk, error) {
	out := make(chan *canon.StreamChunk, 8)
	go func() {
		defer close(out)
		id := "mock-" + randID()
		name := req.Tools[0].Function.Name
		callID := "call-mock-" + randID()
		emit := func(delta canon.Message) {
			out <- &canon.StreamChunk{
				ID: id, Object: "chat.completion.chunk", Created: nowUnix(), Model: req.Model,
				Choices: []canon.StreamChoice{{Index: 0, Delta: delta}},
			}
		}
		time.Sleep(p.Latency)
		// 首帧: tool_call 索引/id/name(arguments 空)。
		tc0 := canon.ToolCall{Index: 0, ID: callID, Type: "function"}
		tc0.Function.Name = name
		emit(canon.Message{Role: "assistant", ToolCalls: []canon.ToolCall{tc0}})
		time.Sleep(p.Latency)
		// 次帧: arguments 增量(JSON 片段,模拟分片)。
		tc1 := canon.ToolCall{Index: 0}
		tc1.Function.Arguments = `{"city":"北京"}`
		emit(canon.Message{ToolCalls: []canon.ToolCall{tc1}})
		time.Sleep(p.Latency)
		// 末帧: finish + usage。
		fr := "tool_calls"
		usage := canon.Usage{PromptTokens: 12, CompletionTokens: 6, TotalTokens: 18}
		out <- &canon.StreamChunk{
			ID: id, Object: "chat.completion.chunk", Created: nowUnix(), Model: req.Model,
			Choices: []canon.StreamChoice{{Index: 0, Delta: canon.Message{}, FinishReason: &fr}},
			Usage:   &usage,
		}
	}()
	return out, nil
}

func (p *Provider) Embeddings(ctx context.Context, ch *provider.Channel, input []string, model string) ([][]float32, *canon.Usage, error) {
	out := make([][]float32, 0, len(input))
	total := 0
	for _, s := range input {
		vec := make([]float32, 8)
		for i := 0; i < len(s); i++ {
			vec[i%8] += float32(s[i]) / 255.0
		}
		out = append(out, vec)
		total += len(s) / p.CharsPerToken
	}
	return out, &canon.Usage{PromptTokens: total, TotalTokens: total}, nil
}

// buildReply 生成确定性回复。
func (p *Provider) buildReply(req *canon.Request) string {
	var lastUser string
	var images, audios, files int
	for _, m := range req.Messages {
		if m.Role != "user" {
			continue
		}
		switch v := m.Content.(type) {
		case string:
			lastUser = v
		case []canon.ContentPart:
			for _, part := range v {
				switch part.Type {
				case "text":
					lastUser += part.Text
				case "image_url":
					images++
				case "input_audio":
					audios++
				case "file":
					files++
				}
			}
		}
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "[mock 回复] 已收到你的请求(模型 %s)。", req.Model)
	if images > 0 || audios > 0 || files > 0 {
		fmt.Fprintf(&sb, " 多模态输入: 图片=%d 音频=%d 文件=%d。", images, audios, files)
	}
	if lastUser != "" {
		fmt.Fprintf(&sb, " 你说的是: %s", truncate(lastUser, 200))
	}
	sb.WriteString("\n\n我是 Mock 供应商,仅用于开发与测试。配置真实渠道(百炼/火山方舟/千帆)后将由对应 adapter 响应。")
	return sb.String()
}

func (p *Provider) usage(req *canon.Request, reply string) canon.Usage {
	prompt := 0
	for _, m := range req.Messages {
		prompt += len(canon.TextContent(m)) / max(1, p.CharsPerToken)
	}
	comp := len(reply) / max(1, p.CharsPerToken)
	// 保底至少 1 个 completion token,便于计费测试。
	if comp == 0 {
		comp = 1
	}
	return canon.Usage{PromptTokens: prompt, CompletionTokens: comp, TotalTokens: prompt + comp}
}

// --- helpers ---

// nowUnix 避免在测试中直接依赖时间(此处允许,运行时使用)。
func nowUnix() int64 { return time.Now().Unix() }

func randID() string { return fmt.Sprintf("%d", time.Now().UnixNano()) }

func tokenize(s string, step int) []string {
	runes := []rune(s)
	var out []string
	for i := 0; i < len(runes); i += step {
		end := i + step
		if end > len(runes) {
			end = len(runes)
		}
		out = append(out, string(runes[i:end]))
	}
	if len(out) == 0 {
		out = []string{s}
	}
	return out
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
