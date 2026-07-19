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
