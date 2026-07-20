package web

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aitoys/llm-gateway/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// --- tenants ---

func (s *Server) adminListTenants(g *gin.Context) {
	ts, err := s.Store.ListTenants(g.Request.Context())
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	g.JSON(http.StatusOK, gin.H{"data": ts})
}

type createTenantReq struct {
	Name string `json:"name" binding:"required"`
	Slug string `json:"slug"`
}

func (s *Server) adminCreateTenant(g *gin.Context) {
	var req createTenantReq
	if !s.bindJSON(g, &req) {
		return
	}
	id := uuid.NewString()
	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		slug = "t-" + id[:8]
	}
	if err := s.Store.CreateTenant(g.Request.Context(), &model.Tenant{
		ID: id, Name: req.Name, Slug: slug, Status: "active", CreatedAt: time.Now(),
	}); err != nil {
		s.respondInternal(g, err)
		return
	}
	s.audit(g, "tenant.create", id)
	g.JSON(http.StatusOK, gin.H{"id": id, "name": req.Name, "slug": slug})
}

type updateTenantReq struct {
	Name string `json:"name" binding:"required"`
	Slug string `json:"slug"`
}

// adminUpdateTenant 编辑租户名称/Slug。平台内置租户(tenant-platform)受保护不可改。
func (s *Server) adminUpdateTenant(g *gin.Context) {
	id := g.Param("id")
	if id == model.PlatformTenantID {
		g.JSON(http.StatusBadRequest, gin.H{"error": "平台内置租户不可修改"})
		return
	}
	var req updateTenantReq
	if !s.bindJSON(g, &req) {
		return
	}
	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		slug = "t-" + id[:8]
	}
	if err := s.Store.UpdateTenant(g.Request.Context(), id, strings.TrimSpace(req.Name), slug); err != nil {
		s.respondInternal(g, err)
		return
	}
	s.audit(g, "tenant.update", id)
	g.JSON(http.StatusOK, gin.H{"id": id})
}

// adminSetTenantStatus 启用/禁用租户。平台内置租户受保护不可禁用。
func (s *Server) adminSetTenantStatus(g *gin.Context) {
	id := g.Param("id")
	if id == model.PlatformTenantID {
		g.JSON(http.StatusBadRequest, gin.H{"error": "平台内置租户不可禁用"})
		return
	}
	var req setStatusReq
	if !s.bindJSON(g, &req) {
		return
	}
	if req.Status != "active" && req.Status != "disabled" {
		g.JSON(http.StatusBadRequest, gin.H{"error": "invalid status"})
		return
	}
	if err := s.Store.SetTenantStatus(g.Request.Context(), id, req.Status); err != nil {
		s.respondInternal(g, err)
		return
	}
	s.audit(g, "tenant.status", id+" -> "+req.Status)
	g.JSON(http.StatusOK, gin.H{"id": id, "status": req.Status})
}

// --- 平台账本导出 ---

// adminLedgerExport 导出全局账本为 CSV(最多 10000 条,管理端对账/归档/报销用)。
// 写入 UTF-8 BOM 以便 Excel 正确识别中文列名。仅平台管理员(platform 组)可访问。
func (s *Server) adminLedgerExport(g *gin.Context) {
	rows, err := s.Store.AdminLedgerRecent(g.Request.Context(), 10000)
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	g.Header("Content-Type", "text/csv; charset=utf-8")
	g.Header("Content-Disposition", `attachment; filename="ledger.csv"`)
	// UTF-8 BOM: 否则 Excel 打开中文列名乱码。
	_, _ = g.Writer.Write([]byte{0xEF, 0xBB, 0xBF})
	w := csv.NewWriter(g.Writer)
	_ = w.Write([]string{"时间", "租户ID", "用户ID", "请求ID", "模型", "输入token", "输出token",
		"成本(分)", "售价(分)", "毛利(分)", "类型", "记账后余额(分)"})
	for _, r := range rows {
		_ = w.Write([]string{
			r.CreatedAt.Format("2006-01-02 15:04:05"), r.TenantID, r.UserID, r.RequestID, r.Model,
			strconv.Itoa(r.InputTokens), strconv.Itoa(r.OutputTokens),
			strconv.FormatInt(r.CostCents, 10), strconv.FormatInt(r.PriceCents, 10),
			strconv.FormatInt(r.MarginCents, 10), string(r.Type), strconv.FormatInt(r.BalanceAfter, 10),
		})
	}
	w.Flush()
}
