package web

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/aitoys/llm-gateway/internal/canon"
	"github.com/aitoys/llm-gateway/internal/provider"
	"github.com/gin-gonic/gin"
)

// validateBaseURL 校验渠道 base_url,阻止 SSRF:仅允许 http/https scheme,
// 拒绝解析到内网/回环/链路本地地址(租户/平台管理员可填 base_url,若无校验,
// 网关会以自身进程身份+解密后的上游 Key 请求攻击者控制的内网地址)。
// dev 模式放宽(便于本地联调 mock 上游)。
func validateBaseURL(raw string, dev bool) error {
	if raw == "" {
		return nil // 空=用 provider 默认 base_url(已是可信常量)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("base_url 格式非法: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("base_url 必须是 http/https,当前 scheme: %q", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("base_url 缺少 host")
	}
	if dev {
		return nil
	}
	// 解析主机名(域名走 DNS)。任一解析结果命中内网即拒绝。
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("base_url host 解析失败: %w", err)
	}
	for _, ip := range ips {
		if !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
			return fmt.Errorf("base_url 解析到内网/回环地址 %s,已拒绝(SSRF 防护)", ip)
		}
	}
	return nil
}

// validateChannelBaseURL 便捷包装:校验失败返回 true(已写 400 响应)。
func (s *Server) validateChannelBaseURL(g *gin.Context, baseURL string) bool {
	if err := validateBaseURL(baseURL, s.Dev); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"type": "invalid_request_error", "message": err.Error()}})
		return true
	}
	return false
}


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
