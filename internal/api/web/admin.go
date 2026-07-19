package web

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aitoys/llm-gateway/internal/auth"
	"github.com/aitoys/llm-gateway/internal/crypto"
	"github.com/aitoys/llm-gateway/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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

// --- users ---

func (s *Server) adminListUsers(g *gin.Context) {
	sub := mustSub(g)
	var (
		us  []*model.User
		err error
	)
	if sub.IsPlatformAdmin() {
		us, err = s.Store.ListAllUsers(g.Request.Context())
	} else {
		us, err = s.Store.ListUsers(g.Request.Context(), sub.TenantID)
	}
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	g.JSON(http.StatusOK, gin.H{"data": us})
}

type createUserReq struct {
	TenantID string `json:"tenant_id" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
	Role     string `json:"role"`
}

func (s *Server) adminCreateUser(g *gin.Context) {
	sub := mustSub(g)
	var req createUserReq
	if !s.bindJSON(g, &req) {
		return
	}
	// 租户管理员只能在其本租户下创建用户;平台管理员可指定任意租户。
	if !sub.IsPlatformAdmin() {
		req.TenantID = sub.TenantID
	}
	role := req.Role
	// 仅平台管理员可授予 platform_admin;其余一律降级为 member/admin。
	if role == string(model.RolePlatformAdmin) {
		if !sub.IsPlatformAdmin() {
			role = string(model.RoleMember)
		}
	} else if role != string(model.RoleAdmin) {
		role = string(model.RoleMember)
	}
	hash, err := crypto.HashPassword(req.Password)
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	id := uuid.NewString()
	if err := s.Store.CreateUser(g.Request.Context(), &model.User{
		ID: id, TenantID: req.TenantID, Email: strings.ToLower(req.Email), PasswordHash: hash,
		Role: model.Role(role), Status: "active", CreatedAt: time.Now(),
	}); err != nil {
		s.respondInternal(g, err)
		return
	}
	s.audit(g, "user.create", id+" / "+req.Email)
	g.JSON(http.StatusOK, gin.H{"id": id})
}

type setStatusReq struct {
	Status string `json:"status" binding:"required"`
}

func (s *Server) adminSetUserStatus(g *gin.Context) {
	var req setStatusReq
	if !s.bindJSON(g, &req) {
		return
	}
	uid := g.Param("id")
	// 租户管理员仅能操作本租户用户;平台管理员可操作任意用户。
	if u, err := s.Store.GetUser(g.Request.Context(), uid); err == nil {
		if !s.ensureUserScope(g, u) {
			return
		}
	} else {
		g.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	if err := s.Store.SetUserStatus(g.Request.Context(), uid, req.Status); err != nil {
		g.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	// 禁用用户时,清理其全部 API Key 鉴权缓存,使吊销即时生效。
	if req.Status == "disabled" && s.RDB != nil {
		if hashes, err := s.Store.APIKeyHashesByUser(g.Request.Context(), uid); err == nil {
			for _, h := range hashes {
				_ = s.RDB.Del(g.Request.Context(), "apikey:"+h).Err()
			}
		}
	}
	s.audit(g, "user.status", uid+"="+req.Status)
	s.ok(g)
}

type updateUserReq struct {
	Email string `json:"email" binding:"required,email"`
	Role  string `json:"role"`
}

// adminUpdateUser 编辑用户邮箱/角色。角色授予规则同创建:非平台管理员不可授 platform_admin。
func (s *Server) adminUpdateUser(g *gin.Context) {
	uid := g.Param("id")
	u, err := s.Store.GetUser(g.Request.Context(), uid)
	if err != nil {
		g.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	if !s.ensureUserScope(g, u) {
		return
	}
	var req updateUserReq
	if !s.bindJSON(g, &req) {
		return
	}
	role := req.Role
	switch role {
	case string(model.RolePlatformAdmin):
		if !mustSub(g).IsPlatformAdmin() {
			role = string(model.RoleMember)
		}
	case string(model.RoleAdmin), string(model.RoleMember):
		// 允许
	default:
		role = string(model.RoleMember)
	}
	if err := s.Store.UpdateUser(g.Request.Context(), uid, strings.ToLower(req.Email), role); err != nil {
		s.respondInternal(g, err)
		return
	}
	s.audit(g, "user.update", uid)
	s.ok(g)
}

type resetPasswordReq struct {
	Password string `json:"password" binding:"required,min=6"`
}

// adminResetPassword 管理员重置用户密码。
func (s *Server) adminResetPassword(g *gin.Context) {
	uid := g.Param("id")
	u, err := s.Store.GetUser(g.Request.Context(), uid)
	if err != nil {
		g.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	if !s.ensureUserScope(g, u) {
		return
	}
	var req resetPasswordReq
	if !s.bindJSON(g, &req) {
		return
	}
	hash, err := crypto.HashPassword(req.Password)
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	if err := s.Store.SetUserPassword(g.Request.Context(), uid, hash); err != nil {
		s.respondInternal(g, err)
		return
	}
	s.audit(g, "user.password_reset", uid)
	s.ok(g)
}

type adjustBalanceReq struct {
	DeltaCents int64  `json:"delta_cents"`
	Reason     string `json:"reason"`
}

// adminAdjustBalance 手动调整用户余额(增减,分),走账本记账。
// 必须经 billing.Adjust(写 billing_ledger type='adjust'),否则账实不一致、无法对账追溯。
// 扣减导致负余额时由 DB CHECK 拒绝,返回 409。reason 记入审计便于事后追溯。
func (s *Server) adminAdjustBalance(g *gin.Context) {
	uid := g.Param("id")
	u, err := s.Store.GetUser(g.Request.Context(), uid)
	if err != nil {
		g.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	if !s.ensureUserScope(g, u) {
		return
	}
	var req adjustBalanceReq
	if !s.bindJSON(g, &req) {
		return
	}
	nb, err := s.Billing.Adjust(g.Request.Context(), u.TenantID, uid, req.DeltaCents)
	if err != nil {
		// CHECK 约束失败(扣到负)等资金约束归为 409,与其他业务校验区分。
		g.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	s.audit(g, "user.balance", uid+" "+strings.TrimSpace(req.Reason))
	g.JSON(http.StatusOK, gin.H{"balance_cents": nb})
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
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "23505")
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
	ModelName            string   `json:"model_name" binding:"required"`
	InputPriceCentsPerM      int64    `json:"input_price_cents_per_m"`
	OutputPriceCentsPerM     int64    `json:"output_price_cents_per_m"`
	CacheReadPriceCentsPerM  int64    `json:"cache_read_price_cents_per_m"`
	CacheWritePriceCentsPerM int64    `json:"cache_write_price_cents_per_m"`
	Enabled                  bool     `json:"enabled"`
	Description          string   `json:"description"`
	LongDesc             string   `json:"long_desc"`
	Tags                 []string `json:"tags"`
	Capabilities         []string `json:"capabilities"`
	ContextLength        int      `json:"context_length"`
	RoutingStrategy      string   `json:"routing_strategy"`
	PinnedChannelID      string   `json:"pinned_channel_id"`
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
