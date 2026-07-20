package anthropic

import (
	"encoding/json"
	"testing"

	"github.com/aitoys/llm-gateway/internal/canon"
)

// canonToAnthropicReq 是出口侧的核心转换;曾有多模态丢失/角色未映射/tool_result 拆分等 bug。
// 本测试覆盖修复后的关键不变量。
func TestCanonToAnthropicReq(t *testing.T) {
	t.Run("developer role 归为 system", func(t *testing.T) {
		req := &canon.Request{
			Model:    "claude-3",
			MaxTokens: 100,
			Messages: []canon.Message{
				{Role: "developer", Content: "你是助手"},
				{Role: "user", Content: "hi"},
			},
		}
		b, err := canonToAnthropicReq(req, false)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var out map[string]any
		_ = json.Unmarshal(b, &out)
		if out["system"] != "你是助手" {
			t.Fatalf("developer 应归入 system,got system=%v", out["system"])
		}
		msgs := out["messages"].([]any)
		if len(msgs) != 1 {
			t.Fatalf("developer 提取后应只剩 1 条 message,got %d", len(msgs))
		}
	})

	t.Run("多模态 image_url 经 AsParts 归一", func(t *testing.T) {
		// 模拟 OpenAI 入口 ShouldBindJSON 绑出的 []interface{} 形态。
		content := []any{
			map[string]any{"type": "text", "text": "看图"},
			map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,AAAA"}},
		}
		req := &canon.Request{
			Model: "claude-3", MaxTokens: 100,
			Messages: []canon.Message{{Role: "user", Content: content}},
		}
		b, _ := canonToAnthropicReq(req, false)
		var out map[string]any
		_ = json.Unmarshal(b, &out)
		msgs := out["messages"].([]any)
		m := msgs[0].(map[string]any)
		blocks := m["content"].([]any)
		// 期望:text + image 两个 block(原先 []interface{} 被丢弃只剩空 text 兜底)。
		var hasText, hasImage bool
		for _, bk := range blocks {
			bm := bk.(map[string]any)
			switch bm["type"] {
			case "text":
				hasText = true
			case "image":
				hasImage = true
			}
		}
		if !hasText || !hasImage {
			t.Fatalf("多模态应保留 text+image,blocks=%v", blocks)
		}
	})

	t.Run("连续 tool 结果合并为单条 user 消息", func(t *testing.T) {
		req := &canon.Request{
			Model: "claude-3", MaxTokens: 100,
			Messages: []canon.Message{
				{Role: "assistant", ToolCalls: []canon.ToolCall{{ID: "c1", Type: "function", Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "get_weather", Arguments: `{"city":"北京"}`}}}},
				{Role: "tool", ToolCallID: "c1", Content: "晴"},
				{Role: "tool", ToolCallID: "c2", Content: "25度"},
			},
		}
		b, _ := canonToAnthropicReq(req, false)
		var out map[string]any
		_ = json.Unmarshal(b, &out)
		msgs := out["messages"].([]any)
		// assistant + 1 条 user(含 2 个 tool_result block)。
		var lastRole string
		var toolResultBlocks int
		for _, m := range msgs {
			mm := m.(map[string]any)
			lastRole = mm["role"].(string)
			if bl, ok := mm["content"].([]any); ok {
				for _, bk := range bl {
					if bm := bk.(map[string]any); bm["type"] == "tool_result" {
						toolResultBlocks++
					}
				}
			}
		}
		if toolResultBlocks != 2 {
			t.Fatalf("两条 tool 结果应在同一 user 消息内(2 个 tool_result block),got %d", toolResultBlocks)
		}
		if lastRole != "user" {
			t.Fatalf("末条消息应为 user(tool 结果),got %s", lastRole)
		}
	})

	t.Run("max_tokens 非法值回退默认", func(t *testing.T) {
		for _, mt := range []int{0, -1, -100} {
			req := &canon.Request{Model: "claude-3", MaxTokens: mt, Messages: []canon.Message{{Role: "user", Content: "x"}}}
			b, _ := canonToAnthropicReq(req, false)
			var out map[string]any
			_ = json.Unmarshal(b, &out)
			if out["max_tokens"].(float64) != 4096 {
				t.Fatalf("MaxTokens=%d 应回退 4096,got %v", mt, out["max_tokens"])
			}
		}
	})
}

func TestCanonToolChoice(t *testing.T) {
	cases := []struct {
		in   any
		want string // 期望 Anthropic type
	}{
		{"auto", "auto"},
		{"none", "none"},
		{"required", "any"},
		{map[string]any{"type": "auto"}, "auto"},
		{map[string]any{"type": "required"}, "any"},
		{map[string]any{"type": "function", "function": map[string]any{"name": "get_weather"}}, "tool"},
		{map[string]any{"type": "unknown"}, ""}, // 未知→nil
	}
	for _, c := range cases {
		got := canonToolChoice(c.in)
		if c.want == "" {
			if got != nil {
				t.Fatalf("in=%v 期望 nil,got %v", c.in, got)
			}
			continue
		}
		if got == nil || got["type"] != c.want {
			t.Fatalf("in=%v 期望 type=%s,got %v", c.in, c.want, got)
		}
	}
}

func TestStopToFinish(t *testing.T) {
	cases := map[string]string{
		"end_turn":      "stop",
		"stop_sequence": "stop", // 曾缺失,导致 Anthropic 客户端拿不到命中停止序列的语义
		"max_tokens":    "length",
		"tool_use":      "tool_calls",
		"":              "stop",
	}
	for in, want := range cases {
		if got := stopToFinish(in); got != want {
			t.Fatalf("stopToFinish(%q)=%q,want %q", in, got, want)
		}
	}
}
