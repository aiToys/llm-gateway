package web

import (
	"net/http"

	"github.com/aitoys/llm-gateway/internal/auth"
	"github.com/aitoys/llm-gateway/internal/model"
	"github.com/gin-gonic/gin"
)

// adminScope 返回管理数据范围: 平台管理员返回 ""(全局);租户管理员返回其 TenantID。
// 用于 List* 类查询按租户收窄,实现租户间隔离。
func adminScope(sub auth.Subject) string {
	if sub.IsPlatformAdmin() {
		return ""
	}
	return sub.TenantID
}

// ensureChannelScope 校验主体对某渠道的管理权。
// 租户管理员仅能操作本租户渠道;平台管理员可操作任意渠道。无权时写 403 并返回 false。
func (s *Server) ensureChannelScope(g *gin.Context, ch *model.Channel) bool {
	sub := mustSub(g)
	if sub.IsPlatformAdmin() {
		return true
	}
	if ch.TenantID == nil || *ch.TenantID != sub.TenantID {
		g.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "无权操作该租户的渠道"})
		return false
	}
	return true
}

// ensureUserScope 校验主体对某用户的管理权(租户管理员仅能管理本租户用户)。
func (s *Server) ensureUserScope(g *gin.Context, u *model.User) bool {
	sub := mustSub(g)
	if sub.IsPlatformAdmin() {
		return true
	}
	if u.TenantID != sub.TenantID {
		g.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "无权操作该租户的用户"})
		return false
	}
	return true
}

// loadChannelScoped 取渠道并校验管理权;无权或不存在时写响应并返回 nil。
func (s *Server) loadChannelScoped(g *gin.Context, id string) *model.Channel {
	ch, err := s.Store.GetChannel(g.Request.Context(), id)
	if err != nil {
		g.JSON(http.StatusNotFound, gin.H{"error": "channel not found"})
		return nil
	}
	if !s.ensureChannelScope(g, ch) {
		return nil
	}
	return ch
}

// --- stats ---

func (s *Server) adminStats(g *gin.Context) {
	st, err := s.Store.AdminStats(g.Request.Context())
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	g.JSON(http.StatusOK, st)
}

type setStatusReq struct {
	Status string `json:"status" binding:"required"`
}

// --- models / pricing ---

func (s *Server) adminListModels(g *gin.Context) {
	ms, err := s.Store.ListModels(g.Request.Context(), false)
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	if chs, err := s.Store.ListChannels(g.Request.Context(), ""); err == nil {
		attachProviders(ms, chs)
	}
	g.JSON(http.StatusOK, gin.H{"data": ms})
}

type upsertModelReq struct {
	ModelName                string   `json:"model_name" binding:"required"`
	InputPriceCentsPerM      int64    `json:"input_price_cents_per_m"`
	OutputPriceCentsPerM     int64    `json:"output_price_cents_per_m"`
	CacheReadPriceCentsPerM  int64    `json:"cache_read_price_cents_per_m"`
	CacheWritePriceCentsPerM int64    `json:"cache_write_price_cents_per_m"`
	Enabled                  bool     `json:"enabled"`
	Description              string   `json:"description"`
	LongDesc                 string   `json:"long_desc"`
	Tags                     []string `json:"tags"`
	Capabilities             []string `json:"capabilities"`
	ContextLength            int      `json:"context_length"`
	RoutingStrategy          string   `json:"routing_strategy"`
	PinnedChannelID          string   `json:"pinned_channel_id"`
}

func (s *Server) adminUpsertModel(g *gin.Context) {
	var req upsertModelReq
	if !s.bindJSON(g, &req) {
		return
	}
	m := &model.ModelDef{
		ModelName:           req.ModelName,
		InputPriceCentsPerM: req.InputPriceCentsPerM, OutputPriceCentsPerM: req.OutputPriceCentsPerM,
		CacheReadPriceCentsPerM: req.CacheReadPriceCentsPerM, CacheWritePriceCentsPerM: req.CacheWritePriceCentsPerM,
		Enabled: req.Enabled, Description: req.Description, LongDesc: req.LongDesc,
		Tags: req.Tags, Capabilities: req.Capabilities, ContextLength: req.ContextLength,
		RoutingStrategy: req.RoutingStrategy, PinnedChannelID: req.PinnedChannelID,
	}
	if err := s.Store.UpsertModel(g.Request.Context(), m); err != nil {
		s.respondInternal(g, err)
		return
	}
	s.audit(g, "model.upsert", req.ModelName)
	s.ok(g)
}

func (s *Server) adminDeleteModel(g *gin.Context) {
	if err := s.Store.DeleteModel(g.Request.Context(), g.Param("name")); err != nil {
		s.respondInternal(g, err)
		return
	}
	s.audit(g, "model.delete", g.Param("name"))
	s.ok(g)
}

// --- ledger ---

func (s *Server) adminLedger(g *gin.Context) {
	rows, err := s.Store.AdminLedgerRecent(g.Request.Context(), 100)
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	g.JSON(http.StatusOK, gin.H{"data": rows})
}
