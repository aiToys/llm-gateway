// Package auth 提供 JWT 签发/校验与请求上下文中的主体信息。
package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// 角色字符串常量(与 internal/model.Role 的字符串值保持一致,避免循环依赖)。
const (
	RolePlatformAdmin = "platform_admin"
	RoleTenantAdmin   = "admin"
	RoleMember        = "member"
)

// Subject 上下文中的主体。
type Subject struct {
	UserID      string
	TenantID    string
	Role        string
	Email       string
	APIKeyID    string // 走 API Key 鉴权时有值(Web JWT 无)
	APIKeyName  string
	TenantStatus string // 租户状态(active|disabled):API Key 鉴权时拦截已禁用租户(随 Subject 缓存,禁用后最多受 TTL 延迟)
	UserStatus   string // 用户状态(active|disabled):同上,使管理员禁用用户在缓存 TTL 内生效
	RPMLimit    int      // API Key 的 RPM 限额(0=不限)
	TPMLimit    int      // API Key 的 TPM 限额(0=不限)
	IPWhitelist []string // API Key 的 IP 白名单(空=不限)
	// 日/月用量配额(0=不限):与 RPM/TPM 同源,由 APIKeyAuth 注入,preflight/中间件据此拦截超额。
	DailyRequestLimit   int
	MonthlyRequestLimit int
	DailyTokenLimit     int
	MonthlyTokenLimit   int
	// API Key 过期时间(缓存命中时复查;nil=永不过期)。使 expires_at 在缓存 TTL 内也能及时失效。
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// IsPlatformAdmin 是否平台超级管理员(跨租户)。
func (s Subject) IsPlatformAdmin() bool { return s.Role == RolePlatformAdmin }

// IsTenantAdmin 是否租户管理员(仅本租户)。
func (s Subject) IsTenantAdmin() bool { return s.Role == RoleTenantAdmin }

// IsAdmin 是否任意管理员(平台或租户)。租户管理员仅能访问其本租户范围。
func (s Subject) IsAdmin() bool { return s.IsPlatformAdmin() || s.IsTenantAdmin() }

type ctxKey struct{}

// Claims JWT 声明。
type Claims struct {
	UserID   string `json:"uid"`
	TenantID string `json:"tid"`
	Role     string `json:"role"`
	Email    string `json:"email"`
	jwt.RegisteredClaims
}

// Service JWT 服务。
type Service struct {
	secret    []byte
	accessTTL time.Duration
}

func New(secret string, accessTTL time.Duration) *Service {
	return &Service{secret: []byte(secret), accessTTL: accessTTL}
}

// Issue 签发 access token。
func (s *Service) Issue(sub Subject) (string, time.Time, error) {
	exp := time.Now().Add(s.accessTTL)
	claims := Claims{
		UserID:   sub.UserID,
		TenantID: sub.TenantID,
		Role:     sub.Role,
		Email:    sub.Email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Subject:   sub.UserID,
		},
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	str, err := tok.SignedString(s.secret)
	return str, exp, err
}

// Parse 校验并解析 token。
func (s *Service) Parse(tokenStr string) (*Claims, error) {
	var claims Claims
	tok, err := jwt.ParseWithClaims(tokenStr, &claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return s.secret, nil
	})
	if err != nil {
		return nil, err
	}
	if !tok.Valid {
		return nil, errors.New("invalid token")
	}
	return &claims, nil
}

// Bearer 从 "Bearer xxx" 提取 token。
func Bearer(header string) string {
	header = strings.TrimSpace(header)
	if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return ""
	}
	return strings.TrimSpace(header[7:])
}

// WithSubject 注入上下文。
func WithSubject(ctx context.Context, sub Subject) context.Context {
	return context.WithValue(ctx, ctxKey{}, sub)
}

// FromContext 取主体。
func FromContext(ctx context.Context) (Subject, bool) {
	s, ok := ctx.Value(ctxKey{}).(Subject)
	return s, ok
}

// ParseTTL 解析时长字符串(简单支持: 15m / 2h / 168h / 30m)。
func ParseTTL(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}
