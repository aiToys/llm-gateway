package openaicomp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aitoys/llm-gateway/internal/canon"
	"github.com/aitoys/llm-gateway/internal/provider"
)

// 测试非流式: 上游返回 OpenAI 格式响应,适配器应原样解析为 canon.Response。
func TestChatNonStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var req canon.Request
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Model != "qwen-max" || req.Stream {
			t.Fatalf("unexpected request: %+v", req)
		}
		// 校验多模态 content 透传
		if len(req.Messages) == 0 {
			t.Fatal("no messages")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(canon.Response{
			ID: "chat-1", Object: "chat.completion", Model: req.Model,
			Choices: []canon.Choice{{Index: 0, Message: canon.Message{Role: "assistant", Content: "hello"}, FinishReason: "stop"}},
			Usage:   canon.Usage{PromptTokens: 5, CompletionTokens: 3, TotalTokens: 8},
		})
	}))
	defer srv.Close()

	a := New("test", srv.URL, true) // dev=true: httptest 本地服务(127.0.0.1)需放行 SSRF 校验
	ch := &provider.Channel{Provider: "test", APIKey: "k", BaseURL: srv.URL}
	resp, err := a.Chat(context.Background(), ch, &canon.Request{
		Model:    "qwen-max",
		Messages: []canon.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.ID != "chat-1" || resp.Usage.TotalTokens != 8 {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.Choices[0].Message.Content != "hello" {
		t.Fatalf("unexpected content: %v", resp.Choices[0].Message.Content)
	}
}

// 测试流式: 解析上游 SSE chunks。
func TestChatStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		// 模拟两帧 + DONE
		for _, s := range []string{
			`data: {"id":"c1","object":"chat.completion.chunk","model":"qwen-max","choices":[{"index":0,"delta":{"role":"assistant","content":"你好"},"finish_reason":null}]}`,
			`data: {"id":"c1","object":"chat.completion.chunk","model":"qwen-max","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":2,"completion_tokens":2,"total_tokens":4}}`,
			`data: [DONE]`,
		} {
			w.Write([]byte(s + "\n\n"))
			fl.Flush()
		}
	}))
	defer srv.Close()

	a := New("test", srv.URL, true) // dev=true: httptest 本地服务(127.0.0.1)需放行 SSRF 校验
	ch := &provider.Channel{Provider: "test", BaseURL: srv.URL, APIKey: "k"}
	out, err := a.ChatStream(context.Background(), ch, &canon.Request{Model: "qwen-max", Messages: []canon.Message{{Role: "user", Content: "x"}}})
	if err != nil {
		t.Fatal(err)
	}
	var got strings.Builder
	var usageTotal int
	for chunk := range out {
		if chunk.Usage != nil {
			usageTotal = chunk.Usage.TotalTokens
		}
		if len(chunk.Choices) > 0 {
			got.WriteString(canon.TextContent(chunk.Choices[0].Delta))
		}
	}
	if got.String() != "你好" {
		t.Fatalf("streamed text want '你好' got %q", got.String())
	}
	if usageTotal != 4 {
		t.Fatalf("usage want 4 got %d", usageTotal)
	}
}

// 测试上游错误透传。
func TestChatError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer srv.Close()
	a := New("test", srv.URL, true) // dev=true: httptest 本地服务(127.0.0.1)需放行 SSRF 校验
	ch := &provider.Channel{BaseURL: srv.URL, APIKey: "bad"}
	_, err := a.Chat(context.Background(), ch, &canon.Request{Model: "x", Messages: []canon.Message{{Role: "user", Content: "y"}}})
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("expected 401 error, got %v", err)
	}
}
