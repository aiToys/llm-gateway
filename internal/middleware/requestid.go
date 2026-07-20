package middleware

import (
	"github.com/aitoys/llm-gateway/internal/requestid"
	"github.com/gin-gonic/gin"
)

// request_id 在 gin context 中的键,与错误日志(g.GetString("request_id"))共用。
const requestIDKey = "request_id"

// RequestID 在最外层为每个请求生成或继承 request_id,注入 gin context 与 context.Context。
// 使请求日志、usage_records、billing_ledger、结构化日志共享同一 ID,便于排障串联。
// 优先采纳客户端入站 X-Request-Id(便于跨系统链路),否则生成 req-<uuid>。
func RequestID() gin.HandlerFunc {
	return func(g *gin.Context) {
		id := g.GetHeader(requestid.Header)
		if id == "" {
			id = requestid.New()
		}
		g.Set(requestIDKey, id)
		g.Header(requestid.Header, id) // 回写响应头,客户端可见
		// 同时注入 context.Context,供 relay/store 等非 gin 代码经 requestid.FromContext 取用。
		g.Request = g.Request.WithContext(requestid.With(g.Request.Context(), id))
		g.Next()
	}
}
