package canon

import "testing"

func TestTextContentString(t *testing.T) {
	m := Message{Role: "user", Content: "hello"}
	if TextContent(m) != "hello" {
		t.Fatal("string content mismatch")
	}
}

func TestTextContentParts(t *testing.T) {
	m := Message{Role: "user", Content: []ContentPart{
		{Type: "text", Text: "a"},
		{Type: "image_url", ImageURL: &ImageURL{URL: "x"}},
		{Type: "text", Text: "b"},
	}}
	if TextContent(m) != "ab" {
		t.Fatalf("want 'ab' got %q", TextContent(m))
	}
}

func TestAsPartsString(t *testing.T) {
	m := Message{Content: "hi"}
	parts := AsParts(m)
	if len(parts) != 1 || parts[0].Type != "text" || parts[0].Text != "hi" {
		t.Fatalf("unexpected parts: %+v", parts)
	}
}

// TestAsPartsInterfaceSlice 覆盖 JSON 反序列化产生的 []interface{} 形态。
// OpenAI 入口用 ShouldBindJSON 绑定到 canon.Request 时,Content 数组得到此形态;
// AsParts 必须能归一,否则跨协议多模态在出口适配器全部丢失。
func TestAsPartsInterfaceSlice(t *testing.T) {
	m := Message{Content: []any{
		map[string]any{"type": "text", "text": "hello"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,AAA", "detail": "high"}},
		map[string]any{"type": "input_audio", "input_audio": map[string]any{"data": "AAA", "format": "mp3"}},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "http://x/a.png"}},
	}}
	parts := AsParts(m)
	if len(parts) != 4 {
		t.Fatalf("want 4 parts, got %d: %+v", len(parts), parts)
	}
	if parts[0].Text != "hello" {
		t.Fatalf("part0 text: %q", parts[0].Text)
	}
	if parts[1].ImageURL == nil || parts[1].ImageURL.URL == "" || parts[1].ImageURL.Detail != "high" {
		t.Fatalf("part1 image_url not parsed: %+v", parts[1])
	}
	if parts[2].InputAudio == nil || parts[2].InputAudio.Data != "AAA" || parts[2].InputAudio.Format != "mp3" {
		t.Fatalf("part2 audio not parsed: %+v", parts[2])
	}
	if parts[3].ImageURL == nil || parts[3].ImageURL.URL != "http://x/a.png" {
		t.Fatalf("part3 image_url not parsed: %+v", parts[3])
	}
}
