// Package middleware 提供鉴权、限流等 gin 中间件。
package middleware

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/aitoys/llm-gateway/internal/auth"
	"github.com/aitoys/llm-gateway/internal/crypto"
	"github.com/aitoys/llm-gateway/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// safeTouch 异步刷新 API Key 最后使用时间,带 recover 避免 store 层 panic 拖垮进程。
func safeTouch(s *store.Store, id string) {
	go func() {
		defer func() { _ = recover() }()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.TouchAPIKey(ctx, id)
	}()
}

// APIKeyAuth 校验 Bearer API Key 并注入主体。
// 仅接受 Authorization 头;不再支持 ?key= 查询参数,避免密钥泄漏到访问日志 / Referrer / 浏览器历史。
func APIKeyAuth(s *store.Store, rdb *redis.Client) gin.HandlerFunc {
	return func(g *gin.Context) {
		token := g.GetHeader("Authorization")
		key := auth.Bearer(token)
		if key == "" {
			abortAuth(g, "missing_api_key")
			return
		}
		hash := crypto.APIKeyHash(key)
		cacheKey := "apikey:" + hash
		var cached *auth.Subject
		if rdb != nil {
			if b, err := rdb.Get(g.Request.Context(), cacheKey).Bytes(); err == nil && len(b) > 0 {
				// 命中: JSON 反序列化 Subject(避免旧版 "|" 分隔符在 Email/Name 含 "|" 时被注入破坏)。
				var s2 auth.Subject
				if json.Unmarshal(b, &s2) == nil && s2.UserID != "" {
					cached = &s2
				}
			}
		}
		var sub auth.Subject
		if cached != nil {
			sub = *cached
		} else {
			k, err := s.GetAPIKeyByHash(g.Request.Context(), hash)
			if err != nil || k == nil {
				abortAuth(g, "invalid_api_key")
				return
			}
			if k.ExpiresAt != nil && k.ExpiresAt.Before(time.Now()) {
				abortAuth(g, "expired_api_key")
				return
			}
			u, err := s.GetUser(g.Request.Context(), k.UserID)
			if err != nil {
				// DB 错误(连接抖动/超时)不可伪装成"用户禁用":否则 active 用户的合法请求被永久拒绝,
				// 且调用方拿不到可重试的 5xx 信号。与账户状态分别判定。
				g.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": gin.H{"type": "internal_error", "message": "internal_error"}})
				return
			}
			if u.Status != "active" {
				abortAuth(g, "user_disabled")
				return
			}
			// 租户禁用则拒绝:使 adminSetTenantStatus 真正生效(对 API 调用拦截)。
			tenantStatus := "active"
			if t, err := s.GetTenant(g.Request.Context(), u.TenantID); err == nil {
				tenantStatus = t.Status
			}
			if tenantStatus != "active" {
				abortAuth(g, "tenant_disabled")
				return
			}
			sub = auth.Subject{UserID: u.ID, TenantID: u.TenantID, Role: string(u.Role), Email: u.Email,
				APIKeyID: k.ID, APIKeyName: k.Name, TenantStatus: tenantStatus, UserStatus: u.Status,
				RPMLimit: k.RPMLimit, TPMLimit: k.TPMLimit, IPWhitelist: k.IPWhitelist,
				DailyRequestLimit: k.DailyRequestLimit, MonthlyRequestLimit: k.MonthlyRequestLimit,
				DailyTokenLimit: k.DailyTokenLimit, MonthlyTokenLimit: k.MonthlyTokenLimit}
			if rdb != nil {
				if b, err := json.Marshal(sub); err == nil {
					_ = rdb.Set(g.Request.Context(), cacheKey, b, 2*time.Minute).Err()
				}
			}
			safeTouch(s, k.ID)
		}
		// 租户/用户禁用拦截(缓存命中路径):旧缓存无该字段时为空,放行以平滑过渡。
		// 新缓存携带 TenantStatus/UserStatus,使管理员禁用操作在 TTL(≤2min)内对 API 调用生效。
		if sub.TenantStatus == "disabled" {
			abortAuth(g, "tenant_disabled")
			return
		}
		if sub.UserStatus == "disabled" {
			abortAuth(g, "user_disabled")
			return
		}
		// IP 白名单:配置了非空列表时,仅允许列表内来源 IP(缓存命中也需校验)。
		if !ipAllowed(g.ClientIP(), sub.IPWhitelist) {
			abortAuth(g, "ip_not_allowed")
			return
		}
		g.Request = g.Request.WithContext(auth.WithSubject(g.Request.Context(), sub))
		g.Set("subject", sub)
		g.Next()
	}
}

// ipAllowed 判断 clientIP 是否在白名单内。空白名单放行;支持单 IP 与 CIDR。
func ipAllowed(clientIP string, whitelist []string) bool {
	if len(whitelist) == 0 {
		return true
	}
	if clientIP == "" {
		return false
	}
	for _, rule := range whitelist {
		if rule == "" {
			continue
		}
		if strings.Contains(rule, "/") {
			if _, network, err := net.ParseCIDR(rule); err == nil && network.Contains(net.ParseIP(clientIP)) {
				return true
			}
			continue
		}
		if rule == clientIP {
			return true
		}
	}
	return false
}

// JWTAuth 校验 Web JWT。
func JWTAuth(svc *auth.Service) gin.HandlerFunc {
	return func(g *gin.Context) {
		token := auth.Bearer(g.GetHeader("Authorization"))
		if token == "" {
			abortAuth(g, "missing_token")
			return
		}
		claims, err := svc.Parse(token)
		if err != nil {
			abortAuth(g, "invalid_token")
			return
		}
		sub := auth.Subject{UserID: claims.UserID, TenantID: claims.TenantID, Role: claims.Role, Email: claims.Email}
		g.Request = g.Request.WithContext(auth.WithSubject(g.Request.Context(), sub))
		g.Set("subject", sub)
		g.Next()
	}
}

// RequireAdmin 要求主体为任意管理员(平台或租户)。
// 注意: 租户管理员仅对其本租户资源有权限,具体隔离由各 handler 按 sub.TenantID 过滤实现。
func RequireAdmin() gin.HandlerFunc {
	return func(g *gin.Context) {
		sub, ok := auth.FromContext(g.Request.Context())
		if !ok || !sub.IsAdmin() {
			g.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": gin.H{"type": "forbidden", "message": "admin required"}})
			return
		}
		g.Next()
	}
}

// RequirePlatformAdmin 要求主体为平台超级管理员(跨租户管理)。
// 用于全局资源: 租户 CRUD、全局模型定价、全局账目/审计。
func RequirePlatformAdmin() gin.HandlerFunc {
	return func(g *gin.Context) {
		sub, ok := auth.FromContext(g.Request.Context())
		if !ok || !sub.IsPlatformAdmin() {
			g.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": gin.H{"type": "forbidden", "message": "platform admin required"}})
			return
		}
		g.Next()
	}
}

func abortAuth(g *gin.Context, typ string) {
	g.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": gin.H{"type": "authentication_error", "message": typ}})
}
