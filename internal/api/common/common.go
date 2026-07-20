// Package common 提供 controller 共用的小工具。
package common

import (
	"errors"
	"net/http"

	"github.com/aitoys/llm-gateway/internal/canon"
	"github.com/aitoys/llm-gateway/internal/logging"
	"github.com/aitoys/llm-gateway/internal/relay"
	"github.com/gin-gonic/gin"
)

// Error 以 OpenAI 风格返回错误:{"error":{"type":..,"message":..}}。
func Error(g *gin.Context, status int, typ, message string) {
	g.JSON(status, gin.H{
		"error": gin.H{
			"type":    typ,
			"message": message,
		},
	})
}

// AnthropicError 以 Anthropic 风格返回错误:顶层必须有 type:"error"。
// Anthropic SDK / Claude Code 严格要求 {"type":"error","error":{"type":..,"message":..}};
// 用 OpenAI 格式会导致客户端 JSON 解析异常、真实错误信息丢失。
func AnthropicError(g *gin.Context, status int, typ, message string) {
	g.JSON(status, gin.H{
		"type": "error",
		"error": gin.H{
			"type":    typ,
			"message": message,
		},
	})
}

// RelayErrorStyle 错误响应的协议风格,决定错误体格式与 type 字符串取值。
type RelayErrorStyle int

const (
	// StyleOpenAI OpenAI 风格:{"error":{"type":..,"message":..}}。
	StyleOpenAI RelayErrorStyle = iota
	// StyleAnthropic Anthropic 风格:{"type":"error","error":{"type":..,"message":..}}。
	StyleAnthropic
)

// WriteRelayError 把 relay 错误映射为对客户端友好的 HTTP 响应,统一 OpenAI/Anthropic 双入口的回写逻辑。
// passthrough 开启时原样透传上游 4xx/5xx(status+body+Retry-After),让智能客户端据真实错误(429/529)退避;
// 已知业务哨兵透出明确类型;其余脱敏为通用错误(完整 err 仅记服务端日志,不泄露上游内部信息)。
// style 决定错误体格式与 type 字符串(两协议对同一语义的 type 命名不同)。
func WriteRelayError(g *gin.Context, err error, passthrough bool, style RelayErrorStyle) {
	var ue *canon.UpstreamError
	if errors.As(err, &ue) && passthrough {
		if ue.RetryAfter != "" {
			g.Header("Retry-After", ue.RetryAfter)
		}
		ct := ue.ContentType
		if ct == "" {
			ct = "application/json"
		}
		g.Data(ue.StatusCode, ct, ue.Body)
		return
	}
	// 哨兵 → (HTTP status, OpenAI type, Anthropic type, message)。
	status, oaiType, antType, msg := http.StatusBadGateway, "upstream_error", "api_error", "upstream request failed"
	switch {
	case errors.Is(err, relay.ErrModelNotFound):
		status, oaiType, antType, msg = http.StatusNotFound, "model_not_found", "not_found_error", "model not found or disabled"
	case errors.Is(err, relay.ErrNoChannel):
		status, oaiType, antType, msg = http.StatusServiceUnavailable, "no_channel", "api_error", "no available channel for model"
	case errors.Is(err, relay.ErrInsufficientBal):
		status, oaiType, antType, msg = http.StatusPaymentRequired, "insufficient_balance", "invalid_request_error", "insufficient balance"
	case errors.Is(err, relay.ErrQuotaExceeded):
		status, oaiType, antType, msg = http.StatusTooManyRequests, "quota_exceeded", "rate_limit_error", "usage quota exceeded"
	default:
		logging.L().Warn("relay upstream error", "req_id", g.GetString("request_id"), "err", err.Error())
	}
	if style == StyleAnthropic {
		AnthropicError(g, status, antType, msg)
	} else {
		Error(g, status, oaiType, msg)
	}
}
