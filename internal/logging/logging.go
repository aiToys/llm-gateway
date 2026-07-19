// Package logging 提供全局结构化日志器(log/slog)与请求日志中间件。
//
// 设计: 全局单 logger(经 Init 配置),业务包通过 L() 取用;每条日志带 request_id /
// tenant_id / user_id 等维度,便于按请求聚合排障。运维侧可接 JSON → ELK/Loki。
package logging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"time"

	"github.com/aitoys/llm-gateway/internal/auth"
	"github.com/gin-gonic/gin"
)

// newRequestID 生成 8 字节 hex 请求 id。
func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

type reqIDKey struct{}

// WithReqID 把 request_id 注入 context,供下游 logging.From 自动提取关联。
func WithReqID(ctx context.Context, rid string) context.Context {
	if rid == "" {
		return ctx
	}
	return context.WithValue(ctx, reqIDKey{}, rid)
}

// ReqIDFrom 从 context 取 request_id(可能为空)。
func ReqIDFrom(ctx context.Context) string {
	v, _ := ctx.Value(reqIDKey{}).(string)
	return v
}

var logger *slog.Logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

// Init 按配置初始化全局 logger。format: "json" | "text"(默认);level: debug|info|warn|error。
func Init(format, level string) {
	opts := &slog.HandlerOptions{Level: parseLevel(level)}
	var h slog.Handler
	if format == "json" {
		h = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		h = slog.NewTextHandler(os.Stdout, opts)
	}
	logger = slog.New(h)
	slog.SetDefault(logger)
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// L 返回全局 logger。
func L() *slog.Logger { return logger }

// From 从请求上下文提取 request_id / subject,返回带维度 logger(用于业务日志)。
func From(ctx context.Context) *slog.Logger {
	l := logger
	if rid := ReqIDFrom(ctx); rid != "" {
		l = l.With("req_id", rid)
	}
	if sub, ok := auth.FromContext(ctx); ok {
		l = l.With("tenant_id", sub.TenantID, "user_id", sub.UserID)
	}
	return l
}

// statusLevel 按 HTTP 状态码决定日志级别。
func statusLevel(status int) slog.Level {
	switch {
	case status >= 500:
		return slog.LevelError
	case status >= 400:
		return slog.LevelWarn
	default:
		return slog.LevelInfo
	}
}

// Middleware gin 请求日志中间件: 记录 method/path/status/latency/request_id/tenant。
func Middleware() gin.HandlerFunc {
	return func(g *gin.Context) {
		rid := g.GetHeader("X-Request-Id")
		if rid == "" {
			rid = newRequestID()
		}
		g.Set("request_id", rid)
		g.Header("X-Request-Id", rid)
		// 注入到 request context,使下游业务日志(logging.From)能自动带上 req_id。
		g.Request = g.Request.WithContext(WithReqID(g.Request.Context(), rid))
		start := time.Now()
		path := g.Request.URL.Path
		g.Next()
		status := g.Writer.Status()
		latency := time.Since(start)
		args := []any{
			"method", g.Request.Method,
			"path", path,
			"status", status,
			"latency_ms", latency.Milliseconds(),
			"ip", g.ClientIP(),
			"req_id", g.GetString("request_id"),
		}
		if sub, ok := auth.FromContext(g.Request.Context()); ok {
			args = append(args, "tenant_id", sub.TenantID, "user_id", sub.UserID)
		}
		if e := len(g.Errors); e > 0 {
			args = append(args, "err", g.Errors.String())
		}
		switch statusLevel(status) {
		case slog.LevelError:
			logger.Error("http", args...)
		case slog.LevelWarn:
			logger.Warn("http", args...)
		default:
			logger.Info("http", args...)
		}
	}
}
