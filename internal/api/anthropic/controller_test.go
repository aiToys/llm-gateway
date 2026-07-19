package anthropic

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/aitoys/llm-gateway/internal/canon"
)

func TestToCanonSystemAndText(t *testing.T) {
	req := &request{
		Model:     "claude-3-5-sonnet",
		MaxTokens: 100,
		System:    "你是助手",
		Messages: []message{
			{Role: "user", Content: json.RawMessage(`"你好"`)},
		},
	}
	c, err := toCanon(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Messages) != 2 || c.Messages[0].Role != "system" {
		t.Fatalf("system message not first: %+v", c.Messages)
	}
	if c.Messages[0].Content != "你是助手" {
		t.Fatalf("system content: %v", c.Messages[0].Content)
	}
	if c.Messages[1].Content != "你好" {
		t.Fatalf("user content: %v", c.Messages[1].Content)
	}
}

func TestToCanonImageBlock(t *testing.T) {
	raw := `[{"type":"text","text":"看图"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAA"}}]`
	req := &request{Model: "m", MaxTokens: 10, Messages: []message{{Role: "user", Content: json.RawMessage(raw)}}}
	c, err := toCanon(req)
	if err != nil {
		t.Fatal(err)
	}
	parts, ok := c.Messages[0].Content.([]canon.ContentPart)
	if !ok {
		t.Fatalf("want []ContentPart got %T", c.Messages[0].Content)
	}
	if len(parts) != 2 || parts[1].Type != "image_url" {
		t.Fatalf("unexpected parts: %+v", parts)
	}
	if parts[1].ImageURL.URL != "data:image/png;base64,AAA" {
		t.Fatalf("image url: %s", parts[1].ImageURL.URL)
	}
}

func TestFromCanonMapsStopReason(t *testing.T) {
	r := &canon.Response{
		ID: "r1", Model: "m",
		Choices: []canon.Choice{{Message: canon.Message{Role: "assistant", Content: "hi"}, FinishReason: "length"}},
		Usage:   canon.Usage{PromptTokens: 3, CompletionTokens: 2},
	}
	out := fromCanon(r)
	if out.StopReason != "max_tokens" {
		t.Fatalf("stop_reason want max_tokens got %s", out.StopReason)
	}
	if out.Usage.OutputTokens != 2 {
		t.Fatalf("output tokens: %d", out.Usage.OutputTokens)
	}
	if len(out.Content) != 1 || out.Content[0].Text != "hi" {
		t.Fatalf("content: %+v", out.Content)
	}
}

func TestMapStop(t *testing.T) {
	cases := map[string]string{
		"stop":       "end_turn",
		"length":     "max_tokens",
		"tool_calls": "tool_use",
		"":           "end_turn",
	}
	for in, want := range cases {
		if got := mapStop(in); got != want {
			t.Errorf("mapStop(%q)=%q want %q", in, got, want)
		}
	}
}

// TestToCanonTools 验证 tools 与 tool_choice 的协议归一:
// input_schema→function.parameters;tool_choice auto/any/tool 各自映射。
func TestToCanonTools(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`)
	req := &request{
		Model: "m", MaxTokens: 10,
		Tools:      []anthropicTool{{Name: "get_weather", Description: "查天气", InputSchema: schema}},
		ToolChoice: &toolChoice{Type: "auto"},
		Messages:   []message{{Role: "user", Content: json.RawMessage(`"北京天气"`)}},
	}
	c, err := toCanon(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Tools) != 1 || c.Tools[0].Function.Name != "get_weather" {
		t.Fatalf("tools: %+v", c.Tools)
	}
	if c.Tools[0].Function.Description != "查天气" {
		t.Fatalf("desc: %s", c.Tools[0].Function.Description)
	}
	// parameters 应为反序列化后的 schema 对象(map[string]any),非 RawMessage。
	params, ok := c.Tools[0].Function.Parameters.(map[string]any)
	if !ok {
		t.Fatalf("parameters want map got %T", c.Tools[0].Function.Parameters)
	}
	if params["type"] != "object" {
		t.Fatalf("parameters.type: %v", params["type"])
	}
	if c.ToolChoice != "auto" {
		t.Fatalf("tool_choice auto: %v", c.ToolChoice)
	}
}

// TestMapToolChoice 验证 tool_choice 各类型映射。
func TestMapToolChoice(t *testing.T) {
	cases := []struct {
		tc   *toolChoice
		want any
	}{
		{&toolChoice{Type: "auto"}, "auto"},
		{&toolChoice{Type: "any"}, "required"},
		{&toolChoice{Type: "none"}, "none"},
		{&toolChoice{Type: "tool", Name: "get_weather"}, map[string]any{"type": "function", "function": map[string]any{"name": "get_weather"}}},
		{nil, nil},
	}
	for _, tc := range cases {
		got := mapToolChoice(tc.tc)
		if fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Errorf("mapToolChoice(%+v)=%v want %v", tc.tc, got, tc.want)
		}
	}
}

// TestToCanonToolUseBlock assistant 的 tool_use block → canon.Message.ToolCalls。
func TestToCanonToolUseBlock(t *testing.T) {
	raw := `[{"type":"tool_use","id":"call-1","name":"get_weather","input":{"city":"北京"}}]`
	req := &request{Model: "m", MaxTokens: 10,
		Messages: []message{{Role: "assistant", Content: json.RawMessage(raw)}}}
	c, err := toCanon(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Messages) != 1 {
		t.Fatalf("messages: %d", len(c.Messages))
	}
	m := c.Messages[0]
	if m.Role != "assistant" || len(m.ToolCalls) != 1 {
		t.Fatalf("assistant tool_calls: %+v", m)
	}
	tc := m.ToolCalls[0]
	if tc.ID != "call-1" || tc.Function.Name != "get_weather" {
		t.Fatalf("tool_call: %+v", tc)
	}
	if tc.Function.Arguments != `{"city":"北京"}` {
		t.Fatalf("arguments: %s", tc.Function.Arguments)
	}
}

// TestToCanonToolResult user 的 tool_result block → 独立 role=tool 消息。
// 同一条 Anthropic user 消息含多个 tool_result 时,应展开为多条 OpenAI tool 消息。
func TestToCanonToolResult(t *testing.T) {
	raw := `[{"type":"tool_result","tool_use_id":"call-1","content":"晴,25度"},
	          {"type":"tool_result","tool_use_id":"call-2","content":"多云"}]`
	req := &request{Model: "m", MaxTokens: 10,
		Messages: []message{{Role: "user", Content: json.RawMessage(raw)}}}
	c, err := toCanon(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Messages) != 2 {
		t.Fatalf("want 2 tool messages got %d: %+v", len(c.Messages), c.Messages)
	}
	for i, m := range c.Messages {
		if m.Role != "tool" {
			t.Fatalf("msg[%d] role: %s", i, m.Role)
		}
	}
	if c.Messages[0].ToolCallID != "call-1" || c.Messages[0].Content != "晴,25度" {
		t.Fatalf("first tool msg: %+v", c.Messages[0])
	}
	if c.Messages[1].ToolCallID != "call-2" {
		t.Fatalf("second tool msg: %+v", c.Messages[1])
	}
}

// TestFromCanonToolUse canon 响应的 tool_calls → Anthropic tool_use block(input 反序列化为对象)。
func TestFromCanonToolUse(t *testing.T) {
	tc := canon.ToolCall{ID: "call-1", Type: "function"}
	tc.Function.Name = "get_weather"
	tc.Function.Arguments = `{"city":"北京"}`
	r := &canon.Response{
		ID: "r1", Model: "m",
		Choices: []canon.Choice{{
			Message:      canon.Message{Role: "assistant", ToolCalls: []canon.ToolCall{tc}},
			FinishReason: "tool_calls",
		}},
	}
	out := fromCanon(r)
	if out.StopReason != "tool_use" {
		t.Fatalf("stop_reason want tool_use got %s", out.StopReason)
	}
	if len(out.Content) != 1 || out.Content[0].Type != "tool_use" {
		t.Fatalf("content: %+v", out.Content)
	}
	b := out.Content[0]
	if b.ID != "call-1" || b.Name != "get_weather" {
		t.Fatalf("tool_use block: %+v", b)
	}
	input, ok := b.Input.(map[string]any)
	if !ok || input["city"] != "北京" {
		t.Fatalf("input: %+v", b.Input)
	}
}

// TestFromCanonEmptyContent 兜底: 无文本无 tool_calls 时补空 text block(Anthropic 要求 content 非空)。
func TestFromCanonEmptyContent(t *testing.T) {
	r := &canon.Response{ID: "r1", Model: "m", Choices: []canon.Choice{{Message: canon.Message{Role: "assistant"}}}}
	out := fromCanon(r)
	if len(out.Content) != 1 || out.Content[0].Type != "text" || out.Content[0].Text != "" {
		t.Fatalf("want 1 empty text block got %+v", out.Content)
	}
}
