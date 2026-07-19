// Package openaicomp 实现对"OpenAI 兼容"上游的通用适配器。
// 阿里云百练(DashScope compatible-mode)、火山方舟(Ark v3)、百度千帆(v2)
// 均提供 OpenAI 兼容的 /chat/completions 接口,因此共用此适配器,
// 仅 defaultBaseURL 不同,真正实现"一次编写、多家复用"。
package openaicomp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aitoys/llm-gateway/internal/canon"
	"github.com/aitoys/llm-gateway/internal/provider"
)

// Adapter OpenAI 兼容供应商适配器。
type Adapter struct {
	name           string
	defaultBaseURL string
	client         *http.Client
}

// New 构造一个 OpenAI 兼容适配器。
func New(name, defaultBaseURL string) *Adapter {
	return &Adapter{
		name:           name,
		defaultBaseURL: defaultBaseURL,
		client:         &http.Client{Timeout: 5 * time.Minute},
	}
}

func (a *Adapter) Name() string { return a.name }

func (a *Adapter) baseURL(ch *provider.Channel) string {
	if ch.BaseURL != "" {
		return strings.TrimRight(ch.BaseURL, "/")
	}
	return strings.TrimRight(a.defaultBaseURL, "/")
}

// parseCacheTokens 从上游原始响应体提取缓存 token,填入 canon.Usage。
// 不同供应商字段名不一,统一归一: OpenAI prompt_tokens_details.cached_tokens、
// DeepSeek prompt_cache_hit_tokens、Anthropic cache_read/cache_creation_input_tokens。
func parseCacheTokens(raw []byte, u canon.Usage) canon.Usage {
	var d struct {
		Usage struct {
			PromptTokensDetails struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			PromptCacheHitTokens     int `json:"prompt_cache_hit_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(raw, &d) == nil {
		u.CacheReadTokens = d.Usage.PromptTokensDetails.CachedTokens + d.Usage.PromptCacheHitTokens + d.Usage.CacheReadInputTokens
		u.CacheWriteTokens = d.Usage.CacheCreationInputTokens
	}
	return u
}

// Chat 非流式。
func (a *Adapter) Chat(ctx context.Context, ch *provider.Channel, req *canon.Request) (*canon.Response, error) {
	req.Stream = false
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL(ch)+"/chat/completions", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+ch.APIKey)
	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%s upstream %d: %s", a.name, resp.StatusCode, snippet(raw))
	}
	var out canon.Response
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode %s response: %w (body: %s)", a.name, err, snippet(raw))
	}
	out.Usage = parseCacheTokens(raw, out.Usage)
	return &out, nil
}

// ChatStream 流式。
func (a *Adapter) ChatStream(ctx context.Context, ch *provider.Channel, req *canon.Request) (<-chan *canon.StreamChunk, error) {
	req.Stream = true
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL(ch)+"/chat/completions", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+ch.APIKey)
	httpReq.Header.Set("Accept", "text/event-stream")
	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close() //nolint:errcheck // 错误响应体读取后立即关闭,关闭错误无意义
		return nil, fmt.Errorf("%s upstream %d: %s", a.name, resp.StatusCode, snippet(raw))
	}
	out := make(chan *canon.StreamChunk, 16)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "[DONE]" {
				return
			}
			var chunk canon.StreamChunk
			if err := json.Unmarshal([]byte(payload), &chunk); err == nil {
				// 最后一帧 usage 可能带缓存 token,从原始 payload 提取归一。
				if chunk.Usage != nil {
					u := parseCacheTokens([]byte(payload), *chunk.Usage)
					chunk.Usage = &u
				}
				select {
				case out <- &chunk:
				case <-ctx.Done():
					return
				}
			}
		}
		// 检查 Scanner 错误:网络中断/读超时/行超长时 sc.Scan() 返回 false 但 sc.Err() 非空。
		// 不检查会把上游断流当成正常结束 → 漏计费(按 lastUsage 少算输出 token)、
		// 静默截断响应、且病态渠道(200 OK 后立即断流)永不熔断。发一个错误信号 chunk 给 relay。
		if err := sc.Err(); err != nil {
			select {
			case out <- &canon.StreamChunk{StreamError: err.Error()}:
			case <-ctx.Done():
			}
		}
	}()
	return out, nil
}

// Embeddings 文本向量(OpenAI 兼容)。
func (a *Adapter) Embeddings(ctx context.Context, ch *provider.Channel, input []string, model string) ([][]float32, *canon.Usage, error) {
	payload := map[string]any{"model": model, "input": input}
	body, _ := json.Marshal(payload)
	httpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL(ch)+"/embeddings", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+ch.APIKey)
	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, nil, fmt.Errorf("%s embeddings %d: %s", a.name, resp.StatusCode, snippet(raw))
	}
	var res struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Usage *canon.Usage `json:"usage"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil, nil, err
	}
	out := make([][]float32, 0, len(res.Data))
	for _, d := range res.Data {
		out = append(out, d.Embedding)
	}
	return out, res.Usage, nil
}

func snippet(b []byte) string {
	s := string(b)
	if len(s) > 500 {
		return s[:500]
	}
	return s
}
