// Package openai 实现 OpenAI 兼容入口(/v1/chat/completions, /v1/models)。
package openai

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/aitoys/llm-gateway/internal/api/common"
	"github.com/aitoys/llm-gateway/internal/auth"
	"github.com/aitoys/llm-gateway/internal/canon"
	"github.com/aitoys/llm-gateway/internal/logging"
	"github.com/aitoys/llm-gateway/internal/relay"
	"github.com/gin-gonic/gin"
)

// Controller OpenAI 兼容入口。
type Controller struct {
	Relay *relay.Service
}

// ChatCompletions POST /v1/chat/completions
func (c *Controller) ChatCompletions(g *gin.Context) {
	var req canon.Request
	if err := g.ShouldBindJSON(&req); err != nil {
		common.Error(g, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	if req.Model == "" || len(req.Messages) == 0 {
		common.Error(g, http.StatusBadRequest, "invalid_request_error", "model and messages are required")
		return
	}
	req.Source = "openai"
	sub, ok := auth.FromContext(g.Request.Context())
	if !ok {
		common.Error(g, http.StatusUnauthorized, "authentication_error", "missing subject")
		return
	}

	if req.Stream {
		c.stream(g, sub, &req)
		return
	}
	resp, meta, err := c.Relay.Chat(g.Request.Context(), sub, &req)
	if err != nil {
		c.writeRelayErr(g, err)
		return
	}
	g.Header("X-Request-Id", meta.RequestID)
	g.Set("rl_tokens", int64(meta.Usage.TotalTokens))
	g.JSON(http.StatusOK, resp)
}

func (c *Controller) stream(g *gin.Context, sub auth.Subject, req *canon.Request) {
	ch, meta, err := c.Relay.ChatStream(g.Request.Context(), sub, req)
	if err != nil {
		c.writeRelayErr(g, err)
		return
	}
	g.Header("Content-Type", "text/event-stream")
	g.Header("Cache-Control", "no-cache")
	g.Header("Connection", "keep-alive")
	g.Header("X-Request-Id", meta.RequestID)
	g.Stream(func(w io.Writer) bool {
		select {
		case chunk, ok := <-ch:
			if !ok {
				g.SSEvent("", "[DONE]")
				return false
			}
			b, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\n\n", b)
			g.Writer.Flush()
			return true
		case <-g.Request.Context().Done():
			// 客户端或中间代理断开:仍尝试发 [DONE](经 LB/代理时下游可能仍在读,
			// 依赖 [DONE] 判定流正常结束);写入失败无副作用。
			g.SSEvent("", "[DONE]")
			return false
		}
	})
}

// Embeddings POST /v1/embeddings — OpenAI 兼容文本向量。
// 请求体: {model, input(string|string[])}. 返回 OpenAI embeddings 响应格式。
func (c *Controller) Embeddings(g *gin.Context) {
	var body struct {
		Model string      `json:"model"`
		Input any          `json:"input"` // string 或 []string
	}
	if err := g.ShouldBindJSON(&body); err != nil {
		common.Error(g, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	input := normalizeEmbeddingInput(body.Input)
	if body.Model == "" || len(input) == 0 {
		common.Error(g, http.StatusBadRequest, "invalid_request_error", "model and input are required")
		return
	}
	sub, ok := auth.FromContext(g.Request.Context())
	if !ok {
		common.Error(g, http.StatusUnauthorized, "authentication_error", "missing subject")
		return
	}
	vecs, meta, err := c.Relay.Embeddings(g.Request.Context(), sub, body.Model, input)
	if err != nil {
		c.writeRelayErr(g, err)
		return
	}
	data := make([]gin.H, 0, len(vecs))
	for i, v := range vecs {
		data = append(data, gin.H{"object": "embedding", "index": i, "embedding": v})
	}
	g.Header("X-Request-Id", meta.RequestID)
	g.Set("rl_tokens", int64(meta.Usage.PromptTokens))
	g.JSON(http.StatusOK, gin.H{
		"object": "list", "data": data, "model": body.Model,
		"usage": gin.H{"prompt_tokens": meta.Usage.PromptTokens, "total_tokens": meta.Usage.PromptTokens},
	})
}

// normalizeEmbeddingInput 把 OpenAI 的 input(string 或 []string)归一为 []string。
func normalizeEmbeddingInput(in any) []string {
	switch v := in.(type) {
	case string:
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return v
	}
	return nil
}

// Models GET /v1/models — 返回当前 Key 可用的启用模型列表(OpenAI 格式)。
// 过滤维度: ① 全局启用模型; ② 该 API Key 的 models 白名单(若配置); ③ 租户级覆盖(enabled=false 则排除)。
func (c *Controller) Models(g *gin.Context) {
	sub, ok := auth.FromContext(g.Request.Context())
	if !ok {
		common.Error(g, http.StatusUnauthorized, "authentication_error", "missing subject")
		return
	}
	ms, err := c.Relay.Store.ListModels(g.Request.Context(), true)
	if err != nil {
		common.Error(g, http.StatusInternalServerError, "server_error", "failed to list models")
		return
	}

	// Key 白名单(若配置了非空 models,取交集)。
	allow := map[string]bool{}
	if sub.APIKeyID != "" {
		if k, err := c.Relay.Store.GetAPIKeyByID(g.Request.Context(), sub.APIKeyID); err == nil && len(k.Models) > 0 {
			for _, m := range k.Models {
				allow[m] = true
			}
		}
	}
	// 租户覆盖: enabled=false 的模型排除。
	disabled := map[string]bool{}
	if ovs, err := c.Relay.Store.ListTenantOverrides(g.Request.Context(), sub.TenantID); err == nil {
		for _, o := range ovs {
			if !o.Enabled {
				disabled[o.ModelName] = true
			}
		}
	}

	data := make([]gin.H, 0, len(ms))
	for _, m := range ms {
		if disabled[m.ModelName] {
			continue
		}
		if len(allow) > 0 && !allow[m.ModelName] {
			continue
		}
		data = append(data, gin.H{"id": m.ModelName, "object": "model", "created": time.Now().Unix(), "owned_by": "gateway"})
	}
	g.JSON(http.StatusOK, gin.H{"object": "list", "data": data})
}

// writeRelayErr 把 relay 错误映射为对客户端友好的 HTTP 响应。
// 已知的业务哨兵错误透出明确类型;其余(含上游响应体)一律脱敏为通用 upstream_error,
// 避免把上游网关内部信息 / trace id / 鉴权调试信息泄露给客户端。
func (c *Controller) writeRelayErr(g *gin.Context, err error) {
	switch {
	case errors.Is(err, relay.ErrModelNotFound) || err.Error() == relay.ErrModelNotFound.Error():
		common.Error(g, http.StatusNotFound, "model_not_found", "model not found or disabled")
	case errors.Is(err, relay.ErrNoChannel) || err.Error() == relay.ErrNoChannel.Error():
		common.Error(g, http.StatusServiceUnavailable, "no_channel", "no available channel for model")
	case errors.Is(err, relay.ErrInsufficientBal) || err.Error() == relay.ErrInsufficientBal.Error():
		common.Error(g, http.StatusPaymentRequired, "insufficient_balance", "insufficient balance")
	case errors.Is(err, relay.ErrQuotaExceeded) || err.Error() == relay.ErrQuotaExceeded.Error():
		common.Error(g, http.StatusTooManyRequests, "quota_exceeded", "usage quota exceeded")
	default:
		// 上游错误脱敏: 完整 err 仅记服务端日志,客户端只收到通用提示。
		logging.L().Warn("relay upstream error",
			"req_id", g.GetString("request_id"), "model", "", "err", err.Error())
		common.Error(g, http.StatusBadGateway, "upstream_error", "upstream request failed")
	}
}
