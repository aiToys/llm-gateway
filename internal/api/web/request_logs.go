package web

import (
	"net/http"
	"strconv"

	"github.com/aitoys/llm-gateway/internal/store"
	"github.com/gin-gonic/gin"
)

// --- 请求/响应原文日志(生产排障/合规审计) ---

// adminListRequestLogs 列表(不返回 body,详情用 adminGetRequestLog)。
// 租户管理员仅可见本租户;平台管理员可按 tenant_id 过滤或全局查看。
func (s *Server) adminListRequestLogs(g *gin.Context) {
	sub := mustSub(g)
	f := store.ReqLogFilter{
		UserID:   g.Query("user_id"),
		APIKeyID: g.Query("api_key_id"),
		Model:    g.Query("model"),
	}
	if sub.IsPlatformAdmin() {
		f.TenantID = g.Query("tenant_id") // 平台管理员可按租户过滤(空=全局)
	} else {
		f.TenantID = sub.TenantID // 租户管理员强制限定本租户
	}
	if st := g.Query("status"); st != "" {
		if v, err := strconv.Atoi(st); err == nil {
			f.Status = v
		}
	}
	limit, _ := strconv.Atoi(g.Query("limit"))
	rows, err := s.Store.ListRequestLogs(g.Request.Context(), f, limit)
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	g.JSON(http.StatusOK, gin.H{"data": rows})
}

// adminGetRequestLog 单条详情(含 request_body/response_body 原文)。
func (s *Server) adminGetRequestLog(g *gin.Context) {
	l, err := s.Store.GetRequestLog(g.Request.Context(), g.Param("id"))
	if err != nil {
		g.JSON(http.StatusNotFound, gin.H{"error": "request log not found"})
		return
	}
	// 租户隔离: 非平台管理员仅可查本租户记录。
	sub := mustSub(g)
	if !sub.IsPlatformAdmin() && l.TenantID != sub.TenantID {
		g.JSON(http.StatusNotFound, gin.H{"error": "request log not found"})
		return
	}
	g.JSON(http.StatusOK, gin.H{"data": l})
}
