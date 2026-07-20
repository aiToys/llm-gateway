package web

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/aitoys/llm-gateway/internal/canon"
	"github.com/aitoys/llm-gateway/internal/model"
	"github.com/aitoys/llm-gateway/internal/provider"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
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

// --- channels ---

func (s *Server) adminListChannels(g *gin.Context) {
	sub := mustSub(g)
	cs, err := s.Store.ListChannels(g.Request.Context(), adminScope(sub))
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	// 不回传加密密钥原文。
	// 不回传加密密钥原文;附加实时熔断状态(不触发上游探测,直接读熔断器)。
	out := make([]gin.H, 0, len(cs))
	for _, c := range cs {
		out = append(out, gin.H{
			"id": c.ID, "tenant_id": c.TenantID, "provider": c.Provider, "name": c.Name,
			"base_url": c.BaseURL,
			"priority": c.Priority, "weight": c.Weight,
			"input_cost_cents_per_m": c.InputCostCentsPerM, "output_cost_cents_per_m": c.OutputCostCentsPerM,
			"channel_models": c.ChannelModels,
			"status": c.Status, "created_at": c.CreatedAt, "has_key": c.APIKeyEnc != "",
			"breaker_open": c.Status == "active" && !s.Relay.ChannelOpen(g.Request.Context(), c.ID),
		})
	}
	g.JSON(http.StatusOK, gin.H{"data": out})
}

// channelModelReq 渠道×模型配置(前端表格每行一个)。
type channelModelReq struct {
	ModelName               string `json:"model_name" binding:"required"`
	UpstreamModel           string `json:"upstream_model"` // 空=同名直通
	InputCostCentsPerM      int64  `json:"input_cost_cents_per_m"`
	OutputCostCentsPerM     int64  `json:"output_cost_cents_per_m"`
	CacheReadCostCentsPerM  int64  `json:"cache_read_cost_cents_per_m"`
	CacheWriteCostCentsPerM int64  `json:"cache_write_cost_cents_per_m"`
	Weight                  int    `json:"weight"`  // 0=继承渠道 weight
	Status                  string `json:"status"`  // 空=active
}

// buildChannelModels 把请求体模型行转为 model.ChannelModel(生成 id/默认 status)。
func buildChannelModels(rows []channelModelReq) []model.ChannelModel {
	now := time.Now()
	out := make([]model.ChannelModel, 0, len(rows))
	for _, r := range rows {
		if r.ModelName == "" {
			continue
		}
		st := r.Status
		if st == "" {
			st = "active"
		}
		out = append(out, model.ChannelModel{
			ID: uuid.NewString(), ModelName: r.ModelName, UpstreamModel: r.UpstreamModel,
			InputCostCentsPerM: r.InputCostCentsPerM, OutputCostCentsPerM: r.OutputCostCentsPerM,
			CacheReadCostCentsPerM: r.CacheReadCostCentsPerM, CacheWriteCostCentsPerM: r.CacheWriteCostCentsPerM,
			Weight: r.Weight, Status: st, CreatedAt: now,
		})
	}
	return out
}

type createChannelReq struct {
	Provider            string            `json:"provider" binding:"required"`
	Name                string            `json:"name" binding:"required"`
	BaseURL             string            `json:"base_url"`
	APIKey              string            `json:"api_key"`
	ChannelModels       []channelModelReq `json:"channel_models" binding:"required"`
	Priority            int               `json:"priority"`
	Weight              int               `json:"weight"`
	TenantID            string            `json:"tenant_id"`
	InputCostCentsPerM  int64             `json:"input_cost_cents_per_m"`
	OutputCostCentsPerM int64             `json:"output_cost_cents_per_m"`
}

func (s *Server) adminCreateChannel(g *gin.Context) {
	var req createChannelReq
	if !s.bindJSON(g, &req) {
		return
	}
	if s.validateChannelBaseURL(g, req.BaseURL) {
		return
	}
	enc := ""
	if req.APIKey != "" {
		var err error
		enc, err = s.Cipher.Encrypt(req.APIKey)
		if err != nil {
			s.respondInternal(g, err)
			return
		}
	}
	if req.Weight <= 0 {
		req.Weight = 1
	}
	c := &model.Channel{
		ID: uuid.NewString(), Provider: req.Provider, Name: req.Name, BaseURL: req.BaseURL,
		APIKeyEnc: enc, ChannelModels: buildChannelModels(req.ChannelModels),
		Priority: req.Priority, Weight: req.Weight,
		InputCostCentsPerM: req.InputCostCentsPerM, OutputCostCentsPerM: req.OutputCostCentsPerM,
		Status: "active", CreatedAt: time.Now(),
	}
	if req.TenantID != "" {
		t := req.TenantID
		c.TenantID = &t
	}
	// 租户管理员只能创建本租户渠道(忽略请求体里的 tenant_id,防越权)。
	sub := mustSub(g)
	if !sub.IsPlatformAdmin() {
		t := sub.TenantID
		c.TenantID = &t
	}
	if err := s.Store.CreateChannel(g.Request.Context(), c); err != nil {
		s.respondInternal(g, err)
		return
	}
	s.audit(g, "channel.create", c.ID+" / "+req.Provider)
	s.okData(g, gin.H{"id": c.ID})
}

// adminUpdateChannel 全量编辑渠道(基本信息 + 路由 + 成本)。API Key 留空则保留原密钥。
func (s *Server) adminUpdateChannel(g *gin.Context) {
	var req createChannelReq
	if !s.bindJSON(g, &req) {
		return
	}
	if s.validateChannelBaseURL(g, req.BaseURL) {
		return
	}
	id := g.Param("id")
	cur, err := s.Store.GetChannel(g.Request.Context(), id)
	if err != nil {
		g.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
		return
	}
	if !s.ensureChannelScope(g, cur) {
		return
	}
	enc := cur.APIKeyEnc
	if req.APIKey != "" {
		enc, err = s.Cipher.Encrypt(req.APIKey)
		if err != nil {
			s.respondInternal(g, err)
			return
		}
	}
	if req.Weight <= 0 {
		req.Weight = 1
	}
	c := &model.Channel{
		ID: id, Provider: req.Provider, Name: req.Name, BaseURL: req.BaseURL,
		APIKeyEnc: enc, ChannelModels: buildChannelModels(req.ChannelModels),
		Priority: req.Priority, Weight: req.Weight,
		InputCostCentsPerM: req.InputCostCentsPerM, OutputCostCentsPerM: req.OutputCostCentsPerM,
		Status: cur.Status,
	}
	if err := s.Store.UpdateChannel(g.Request.Context(), c); err != nil {
		s.respondInternal(g, err)
		return
	}
	s.audit(g, "channel.update", id)
	s.ok(g)
}

func (s *Server) adminDeleteChannel(g *gin.Context) {
	if s.loadChannelScoped(g, g.Param("id")) == nil {
		return
	}
	if err := s.Store.DeleteChannel(g.Request.Context(), g.Param("id")); err != nil {
		g.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	s.audit(g, "channel.delete", g.Param("id"))
	s.ok(g)
}

func (s *Server) adminSetChannelStatus(g *gin.Context) {
	var req setStatusReq
	if !s.bindJSON(g, &req) {
		return
	}
	if s.loadChannelScoped(g, g.Param("id")) == nil {
		return
	}
	if err := s.Store.SetChannelStatus(g.Request.Context(), g.Param("id"), req.Status); err != nil {
		s.respondInternal(g, err)
		return
	}
	s.ok(g)
}

// adminSetChannelModelStatus 单模型级启停(禁用某渠道的单个模型,不影响同渠道其他模型)。
func (s *Server) adminSetChannelModelStatus(g *gin.Context) {
	var req setStatusReq
	if !s.bindJSON(g, &req) {
		return
	}
	if s.loadChannelScoped(g, g.Param("id")) == nil {
		return
	}
	if err := s.Store.SetChannelModelStatus(g.Request.Context(), g.Param("id"), g.Param("model"), req.Status); err != nil {
		g.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	s.audit(g, "channel.model_status", g.Param("id")+":"+g.Param("model")+"="+req.Status)
	s.ok(g)
}

// adminAddChannelModel 把单个模型挂载到渠道(默认成本回退渠道级)。已存在则幂等返回 ok。
func (s *Server) adminAddChannelModel(g *gin.Context) {
	if s.loadChannelScoped(g, g.Param("id")) == nil {
		return
	}
	model := g.Param("model")
	if err := s.Store.AddChannelModel(g.Request.Context(), g.Param("id"), model); err != nil {
		// UNIQUE 冲突 = 已挂载,幂等成功。
		if isUniqueViolation(err) {
			s.ok(g)
			return
		}
		s.respondInternal(g, err)
		return
	}
	s.audit(g, "channel.model_add", g.Param("id")+":"+model)
	s.ok(g)
}

// adminRemoveChannelModel 从渠道移除单个模型挂载。
func (s *Server) adminRemoveChannelModel(g *gin.Context) {
	if s.loadChannelScoped(g, g.Param("id")) == nil {
		return
	}
	model := g.Param("model")
	if err := s.Store.DeleteChannelModel(g.Request.Context(), g.Param("id"), model); err != nil {
		g.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	s.audit(g, "channel.model_remove", g.Param("id")+":"+model)
	s.ok(g)
}

// isUniqueViolation 判断是否为 PG 唯一约束冲突(SQLSTATE 23505),用于挂载模型幂等处理。
// 用 errors.As(*pgconn.PgError) 精确匹配 SQLSTATE,避免依赖错误消息文本(被包装/本地化即失效)。
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// updateChannelRoutingReq 更新渠道级路由配置(优先级/权重/供应商默认成本)。

// 模型清单的增删改由 UpdateChannel(全量)或单模型启停接口承担。
type updateChannelRoutingReq struct {
	Priority            *int   `json:"priority"`
	Weight              *int   `json:"weight"`
	InputCostCentsPerM  *int64 `json:"input_cost_cents_per_m"`
	OutputCostCentsPerM *int64 `json:"output_cost_cents_per_m"`
}

// adminUpdateChannelRouting 一站式调整某渠道在负载均衡中的角色(主备/权重)及供应商默认成本。
func (s *Server) adminUpdateChannelRouting(g *gin.Context) {
	var req updateChannelRoutingReq
	if !s.bindJSON(g, &req) {
		return
	}
	cur, err := s.Store.GetChannel(g.Request.Context(), g.Param("id"))
	if err != nil {
		g.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
		return
	}
	if !s.ensureChannelScope(g, cur) {
		return
	}
	priority := cur.Priority
	if req.Priority != nil {
		priority = *req.Priority
	}
	weight := cur.Weight
	if req.Weight != nil {
		weight = *req.Weight
	}
	inCost := cur.InputCostCentsPerM
	if req.InputCostCentsPerM != nil {
		inCost = *req.InputCostCentsPerM
	}
	outCost := cur.OutputCostCentsPerM
	if req.OutputCostCentsPerM != nil {
		outCost = *req.OutputCostCentsPerM
	}
	if weight < 1 {
		weight = 1
	}
	if err := s.Store.UpdateChannelRouting(g.Request.Context(), g.Param("id"), priority, weight, inCost, outCost); err != nil {
		s.respondInternal(g, err)
		return
	}
	s.audit(g, "channel.routing", g.Param("id"))
	s.ok(g)
}
