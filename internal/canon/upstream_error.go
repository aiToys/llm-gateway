package canon

import "fmt"

// UpstreamError 上游返回的非 2xx 响应。
// 开启 passthrough_upstream_errors(默认开)时,controller 用其 StatusCode/Body 原样回写客户端,
// 让智能客户端据真实状态码(429/529 等)与 body(Retry-After / error.type)自行退避重试;
// 关闭时 controller 脱敏为 502 upstream_error,避免对外多租户场景泄露上游内部信息。
type UpstreamError struct {
	Provider    string
	StatusCode  int
	Body        []byte
	ContentType string
	RetryAfter  string // 上游 Retry-After header(若有),透传时原样回写
}

// Error 兼容原 fmt.Errorf("%s upstream %d: %s", ...) 的日志格式,isTransient 据此判断:
// 不含 timeout/eof/connection reset 等关键字 -> 非瞬时错误 -> 不重试,直接故障转移。
func (e *UpstreamError) Error() string {
	return fmt.Sprintf("%s upstream %d: %s", e.Provider, e.StatusCode, Snippet(e.Body))
}

// Snippet 截断响应体片段用于错误信息/日志,避免超长 body 撑爆日志。500 字符与历史实现一致。
func Snippet(b []byte) string {
	s := string(b)
	if len(s) > 500 {
		return s[:500]
	}
	return s
}
