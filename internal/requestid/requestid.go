// Package requestid 提供请求 ID 的生成与 context 传递。
// 从 middleware 解耦出来:核心业务包(relay/store)需要读取 request_id 做链路关联,
// 但不应反向依赖传输层(middleware)——故提取为中立的基础包,middleware 与业务都依赖它。
package requestid

import (
	"context"

	"github.com/google/uuid"
)

// Header 入站/出站传递 request_id 的 HTTP 头。
const Header = "X-Request-Id"

type ctxKey struct{}

// New 生成一个新的请求 ID(req-<uuid>)。
func New() string { return "req-" + uuid.NewString() }

// With 把 id 注入 ctx,返回新 ctx。
func With(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// FromContext 从 ctx 取 request_id;不存在返回空串。
func FromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKey{}).(string); ok {
		return v
	}
	return ""
}
