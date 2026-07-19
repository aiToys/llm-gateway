package web

import (
	"context"
	"net/http"
	"time"

	"github.com/aitoys/llm-gateway/internal/canon"
	"github.com/aitoys/llm-gateway/internal/provider"
	"github.com/gin-gonic/gin"
)

// adminTestChannel 测试渠道连通性: 用该渠道首个模型发一条极小探针请求。
func (s *Server) adminTestChannel(g *gin.Context) {
	id := g.Param("id")
	ch, err := s.Store.GetChannel(g.Request.Context(), id)
	if err != nil {
		g.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
		return
	}
	if !s.ensureChannelScope(g, ch) {
		return
	}
	pv := s.Relay.Providers.Get(ch.Provider)
	if pv == nil {
		g.JSON(http.StatusOK, gin.H{"ok": false, "error": "供应商未注册: " + ch.Provider})
		return
	}
	if len(ch.ChannelModels) == 0 {
		g.JSON(http.StatusOK, gin.H{"ok": false, "error": "渠道未配置模型"})
		return
	}
	// 取首个启用模型;探针用其上游真实模型名(若有映射)。
	first := ch.ChannelModels[0]
	model := first.ModelName
	if first.UpstreamModel != "" {
		model = first.UpstreamModel
	}
	key, _ := s.Cipher.Decrypt(ch.APIKeyEnc)
	tenantID := ""
	if ch.TenantID != nil {
		tenantID = *ch.TenantID
	}
	pch := &provider.Channel{ID: ch.ID, TenantID: tenantID, Provider: ch.Provider, BaseURL: ch.BaseURL, APIKey: key}

	ctx, cancel := context.WithTimeout(g.Request.Context(), 15*time.Second)
	defer cancel()
	start := time.Now()
	_, err = pv.Chat(ctx, pch, &canon.Request{
		Model:    model,
		Messages: []canon.Message{{Role: "user", Content: "ping"}},
	})
	latency := time.Since(start).Milliseconds()
	if err != nil {
		msg := err.Error()
		if len(msg) > 300 {
			msg = msg[:300]
		}
		g.JSON(http.StatusOK, gin.H{"ok": false, "latency_ms": latency, "error": msg})
		return
	}
	g.JSON(http.StatusOK, gin.H{"ok": true, "latency_ms": latency, "model": model, "provider": ch.Provider})
}

// --- 审计日志 ---

func (s *Server) adminAudit(g *gin.Context) {
	rows, err := s.Store.ListAudit(g.Request.Context(), 200)
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	g.JSON(http.StatusOK, gin.H{"data": rows})
}

// audit 在管理动作后记录一条审计(带来源 IP;失败不阻断主流程)。
func (s *Server) audit(g *gin.Context, action, target string) {
	sub := mustSub(g)
	_ = s.Store.InsertAudit(g.Request.Context(), sub.UserID, action, target, g.ClientIP(), nil)
}
