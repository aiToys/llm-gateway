package middleware

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// request_id 在 gin context 中的键,与错误日志(g.GetString("request_id"))共用。
const requestIDKey = "request_id"

// requestIDHeader 入站/出站传递 request_id 的 HTTP 头。
const requestIDHeader = "X-Request-Id"

type ctxKey struct{}

// RequestID 在最外层为每个请求生成或继承 request_id,注入 gin context 与 context.Context。
// 使请求日志、usage_records、billing_ledger、结构化日志共享同一 ID,便于排障串联。
// 优先采纳客户端入站 X-Request-Id(便于跨系统链路),否则生成 req-<uuid>。
func RequestID() gin.HandlerFunc {
	return func(g *gin.Context) {
		id := g.GetHeader(requestIDHeader)
		if id == "" {
			id = "req-" + uuid.NewString()
		}
		g.Set(requestIDKey, id)
		g.Header(requestIDHeader, id) // 回写响应头,客户端可见
		// 同时注入 context.Context,供 relay/store 等非 gin 代码经 FromContext 取用。
		g.Request = g.Request.WithContext(context.WithValue(g.Request.Context(), ctxKey{}, id))
		g.Next()
	}
}

// FromContext 从 ctx 取 request_id(中间件注入);不存在返回空串。
func FromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKey{}).(string); ok {
		return v
	}
	return ""
}
