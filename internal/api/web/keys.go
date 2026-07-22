package web

import (
	"net/http"
	"strings"
	"time"

	"github.com/aitoys/llm-gateway/internal/apikey"
	"github.com/aitoys/llm-gateway/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type createKeyReq struct {
	Name     string   `json:"name"`
	Models   []string `json:"models"`
	RPMLimit int      `json:"rpm_limit"`
	TPMLimit int      `json:"tpm_limit"`
	// 日/月用量配额(0=不限):请求数与 token 上限。
	DailyRequestLimit   int        `json:"daily_request_limit"`
	MonthlyRequestLimit int        `json:"monthly_request_limit"`
	DailyTokenLimit     int        `json:"daily_token_limit"`
	MonthlyTokenLimit   int        `json:"monthly_token_limit"`
	IPWhitelist         []string   `json:"ip_whitelist"`
	ExpiresAt           *time.Time `json:"expires_at,omitempty"` // 空=永不过期
}

func (s *Server) listKeys(g *gin.Context) {
	sub := mustSub(g)
	ks, err := s.Store.ListAPIKeys(g.Request.Context(), sub.UserID)
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	out := make([]gin.H, 0, len(ks))
	for _, k := range ks {
		out = append(out, gin.H{
			"id": k.ID, "name": k.Name, "prefix": k.KeyPrefix, "models": k.Models,
			"rpm_limit": k.RPMLimit, "tpm_limit": k.TPMLimit,
			"daily_request_limit": k.DailyRequestLimit, "monthly_request_limit": k.MonthlyRequestLimit,
			"daily_token_limit": k.DailyTokenLimit, "monthly_token_limit": k.MonthlyTokenLimit,
			"ip_whitelist": k.IPWhitelist,
			"status":       k.Status, "last_used_at": k.LastUsedAt, "created_at": k.CreatedAt,
		})
	}
	g.JSON(http.StatusOK, gin.H{"data": out})
}

func (s *Server) createKey(g *gin.Context) {
	sub := mustSub(g)
	var req createKeyReq
	_ = g.ShouldBindJSON(&req)
	if strings.TrimSpace(req.Name) == "" {
		req.Name = "default"
	}
	plain, hash, prefix, err := apikey.Generate()
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	k := &model.APIKey{
		ID: uuid.NewString(), TenantID: sub.TenantID, UserID: sub.UserID,
		KeyPrefix: prefix, KeyHash: hash, Name: req.Name, Scopes: []string{"chat"},
		Models: req.Models, RPMLimit: req.RPMLimit, TPMLimit: req.TPMLimit,
		DailyRequestLimit: req.DailyRequestLimit, MonthlyRequestLimit: req.MonthlyRequestLimit,
		DailyTokenLimit: req.DailyTokenLimit, MonthlyTokenLimit: req.MonthlyTokenLimit,
		IPWhitelist: req.IPWhitelist, ExpiresAt: req.ExpiresAt, Status: "active", CreatedAt: time.Now(),
	}
	if err := s.Store.CreateAPIKey(g.Request.Context(), k); err != nil {
		s.respondInternal(g, err)
		return
	}
	g.JSON(http.StatusOK, gin.H{
		"id": k.ID, "key": plain, "prefix": prefix, "name": k.Name,
		"note": "请妥善保存,密钥仅在创建时显示一次",
	})
}

func (s *Server) revokeKey(g *gin.Context) {
	sub := mustSub(g)
	id := g.Param("id")
	hash, err := s.Store.RevokeAPIKey(g.Request.Context(), id, sub.UserID)
	if err != nil {
		g.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	// 立即清理鉴权缓存,避免被吊销的密钥在缓存 TTL(2 分钟)内仍可调用。
	if s.RDB != nil && hash != "" {
		_ = s.RDB.Del(g.Request.Context(), "apikey:"+hash).Err()
	}
	s.audit(g, "user.apikey_revoke", id)
	s.ok(g)
}
