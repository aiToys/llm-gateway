package web

import (
	"net/http"
	"strconv"

	"github.com/aitoys/llm-gateway/internal/model"
	"github.com/aitoys/llm-gateway/internal/store"
	"github.com/gin-gonic/gin"
)

func (s *Server) usageByDay(g *gin.Context) {
	sub := mustSub(g)
	days, _ := strconv.Atoi(g.DefaultQuery("days", "30"))
	if days <= 0 || days > 365 {
		days = 30
	}
	agg, err := s.Store.UsageByDay(g.Request.Context(), sub.UserID, days)
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	g.JSON(http.StatusOK, gin.H{"data": agg})
}

func (s *Server) usageLedger(g *gin.Context) {
	sub := mustSub(g)
	limit, _ := strconv.Atoi(g.DefaultQuery("limit", "50"))
	limit = store.ClampLimit(limit, 50, 500)
	rows, err := s.Store.LedgerRecent(g.Request.Context(), sub.UserID, limit)
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	g.JSON(http.StatusOK, gin.H{"data": rows})
}

func (s *Server) listModels(g *gin.Context) {
	ms, err := s.Store.ListModels(g.Request.Context(), true)
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	g.JSON(http.StatusOK, gin.H{"data": ms})
}

// attachProviders 为每个模型填充 Providers 字段(由挂载的活跃渠道推导去重的供应商标识)。
// 一个逻辑模型可能由多家供应商提供,故为集合而非单值。
// 优先级: 模型自身显式声明的 Providers 优先(便于按真实供应商展示,即便统一由 mock 渠道兜底);
// 仅当模型未声明时,才由挂载渠道推导。
func attachProviders(ms []*model.ModelDef, chs []*model.Channel) {
	idx := map[string]map[string]struct{}{}
	for _, c := range chs {
		if c.Status != "active" {
			continue
		}
		for _, cm := range c.ChannelModels {
			if cm.Status != "active" {
				continue
			}
			if idx[cm.ModelName] == nil {
				idx[cm.ModelName] = map[string]struct{}{}
			}
			idx[cm.ModelName][c.Provider] = struct{}{}
		}
	}
	for _, m := range ms {
		if len(m.Providers) > 0 {
			continue // 模型已显式声明供应商,保留
		}
		set := idx[m.ModelName]
		out := make([]string, 0, len(set))
		for p := range set {
			out = append(out, p)
		}
		m.Providers = out
	}
}

// publicProviders 返回已注册的供应商标识列表(单一数据源,供前端渲染渠道/广场下拉,
// 避免前后端各自硬编码导致新增供应商时漂移)。展示文案(label)由前端维护。
func (s *Server) publicProviders(g *gin.Context) {
	names := []string{}
	if s.Relay != nil && s.Relay.Providers != nil {
		names = s.Relay.Providers.Names()
	}
	g.JSON(http.StatusOK, gin.H{"data": names})
}

// publicModels 公开的模型广场数据(无需登录,仅返回展示字段,不含成本)。
func (s *Server) publicModels(g *gin.Context) {
	ms, err := s.Store.ListModels(g.Request.Context(), true)
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	if chs, err := s.Store.ListChannels(g.Request.Context(), ""); err == nil {
		attachProviders(ms, chs)
	}
	out := make([]gin.H, 0, len(ms))
	for _, m := range ms {
		out = append(out, gin.H{
			"model_name":               m.ModelName,
			"providers":                m.Providers,
			"description":              m.Description,
			"long_desc":                m.LongDesc,
			"tags":                     m.Tags,
			"capabilities":             m.Capabilities,
			"context_length":           m.ContextLength,
			"input_price_cents_per_m":  m.InputPriceCentsPerM,
			"output_price_cents_per_m": m.OutputPriceCentsPerM,
		})
	}
	g.JSON(http.StatusOK, gin.H{"data": out})
}
