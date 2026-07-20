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
			if _, network, err := net.ParseCIDR(rule); err == nil {
				// net.ParseIP 对异常/污染头返回 nil,network.Contains(nil) 会 panic——必须判空。
				if ip := net.ParseIP(clientIP); ip != nil && network.Contains(ip) {
					return true
				}
			}
			continue
		}
		// IPv4/IPv6 同址不同表示(如 ::1 与 ::ffff:127.0.0.1)用 Equal 归一,而非字符串严格相等。
		if ip := net.ParseIP(clientIP); ip != nil {
			if r := net.ParseIP(rule); r != nil && r.Equal(ip) {
				return true
			}
		}
		if rule == clientIP {
			return true
		}
	}
	return false
}

// JWTAuth 校验 Web JWT,并(经 Redis 缓存)复查用户/租户状态。
//
// 仅校验签名+exp 不足以让管理员"禁用用户/租户"及时生效——已签发的 JWT 在 access_ttl 内仍可用。
// 此处在 JWT 校验后用短 TTL(1min)缓存复查用户/租户状态:禁用操作最多 1 分钟内对 Web 会话生效。
// 缓存命中时不查 DB;Redis 不可用则每次查 DB(降级,保证禁用仍能生效)。
func JWTAuth(svc *auth.Service, s *store.Store, rdb *redis.Client) gin.HandlerFunc {
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
		// 复查用户/租户状态(短缓存)。平台管理员(超管)不复查,避免误锁平台管理员账号后无法恢复。
		if claims.Role != auth.RolePlatformAdmin {
			userStatus, tenantStatus := loadStatusCached(g.Request.Context(), s, rdb, claims.UserID, claims.TenantID)
			if userStatus == "disabled" {
				abortAuth(g, "user_disabled")
				return
			}
			if tenantStatus == "disabled" {
				abortAuth(g, "tenant_disabled")
				return
			}
		}
		sub := auth.Subject{UserID: claims.UserID, TenantID: claims.TenantID, Role: claims.Role, Email: claims.Email,
			TenantStatus: "active", UserStatus: "active"}
		g.Request = g.Request.WithContext(auth.WithSubject(g.Request.Context(), sub))
		g.Set("subject", sub)
		g.Next()
	}
}

// loadStatusCached 带 1min Redis 缓存地查询用户/租户状态。
// Redis 故障时降级为直接查 DB(每次请求),保证禁用操作在无 Redis 时仍生效。
// DB 查询失败时返回 active(故障不伪装成禁用,避免误拒合法用户;与 APIKeyAuth 同原则)。
func loadStatusCached(ctx context.Context, s *store.Store, rdb *redis.Client, userID, tenantID string) (userStatus, tenantStatus string) {
	const ttl = time.Minute
	cacheKey := "jwtstatus:" + userID
	if rdb != nil {
		if b, err := rdb.Get(ctx, cacheKey).Bytes(); err == nil && len(b) > 0 {
			var v struct {
				User   string `json:"user"`
				Tenant string `json:"tenant"`
			}
			if json.Unmarshal(b, &v) == nil {
				return v.User, v.Tenant
			}
		}
	}
	userStatus = "active"
	if u, err := s.GetUser(ctx, userID); err == nil {
		userStatus = u.Status
	}
	tenantStatus = "active"
	if t, err := s.GetTenant(ctx, tenantID); err == nil {
		tenantStatus = t.Status
	}
	if rdb != nil {
		if b, err := json.Marshal(struct {
			User   string `json:"user"`
			Tenant string `json:"tenant"`
		}{User: userStatus, Tenant: tenantStatus}); err == nil {
			_ = rdb.Set(ctx, cacheKey, b, ttl).Err()
		}
	}
	return userStatus, tenantStatus
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
