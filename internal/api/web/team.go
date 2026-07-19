package web

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/aitoys/llm-gateway/internal/crypto"
	"github.com/aitoys/llm-gateway/internal/model"
	"github.com/aitoys/llm-gateway/internal/store"
	"github.com/gin-gonic/gin"
)

// requireTeamAdmin 团队管理操作(邀请/转账/改名)要求租户管理员或平台管理员,且本租户为启用态。
// (被禁用租户的 admin 不应再发邀请/转账/改名。)
func (s *Server) requireTeamAdmin(g *gin.Context) bool {
	sub := mustSub(g)
	if !sub.IsPlatformAdmin() && !sub.IsTenantAdmin() {
		g.JSON(http.StatusForbidden, gin.H{"error": "需要团队管理员权限"})
		return false
	}
	if !sub.IsPlatformAdmin() {
		if t, err := s.Store.GetTenant(g.Request.Context(), sub.TenantID); err != nil || t == nil || t.Status != "active" {
			g.JSON(http.StatusForbidden, gin.H{"error": "团队已被禁用"})
			return false
		}
	}
	return true
}

// teamInfo 团队信息 + 我的角色。
func (s *Server) teamInfo(g *gin.Context) {
	sub := mustSub(g)
	t, err := s.Store.GetTenant(g.Request.Context(), sub.TenantID)
	if err != nil || t == nil {
		g.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "团队不存在"}})
		return
	}
	g.JSON(http.StatusOK, gin.H{"data": gin.H{
		"id": t.ID, "name": t.Name, "slug": t.Slug, "status": t.Status,
		"my_role": sub.Role, "is_admin": sub.IsPlatformAdmin() || sub.IsTenantAdmin(),
	}})
}

type teamUpdateReq struct {
	Name string `json:"name" binding:"required"`
}

func (s *Server) teamUpdate(g *gin.Context) {
	if !s.requireTeamAdmin(g) {
		return
	}
	sub := mustSub(g)
	var req teamUpdateReq
	if err := g.ShouldBindJSON(&req); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "name 必填"}})
		return
	}
	if err := s.Store.UpdateTenant(g.Request.Context(), sub.TenantID, req.Name, ""); err != nil {
		s.respondInternal(g, err)
		return
	}
	s.audit(g, "team.update", sub.TenantID)
	g.JSON(http.StatusOK, gin.H{"data": gin.H{"ok": true}})
}

// teamMembers 团队成员列表(本租户)。
func (s *Server) teamMembers(g *gin.Context) {
	sub := mustSub(g)
	us, err := s.Store.ListUsers(g.Request.Context(), sub.TenantID)
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	out := make([]gin.H, 0, len(us))
	for _, u := range us {
		out = append(out, gin.H{"id": u.ID, "email": u.Email, "role": string(u.Role),
			"status": u.Status, "balance_cents": u.BalanceCents, "created_at": u.CreatedAt,
			"is_me": u.ID == sub.UserID})
	}
	g.JSON(http.StatusOK, gin.H{"data": out})
}

type transferReq struct {
	ToUserID   string `json:"to_user_id" binding:"required"`
	AmountCents int64 `json:"amount_cents" binding:"required"`
}

// teamTransfer 团长把余额转给本团队成员(原子,不动计费核心)。
func (s *Server) teamTransfer(g *gin.Context) {
	if !s.requireTeamAdmin(g) {
		return
	}
	sub := mustSub(g)
	var req transferReq
	if err := g.ShouldBindJSON(&req); err != nil || req.AmountCents <= 0 {
		g.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "to_user_id 必填且 amount_cents 须为正"}})
		return
	}
	to, err := s.Store.GetUser(g.Request.Context(), req.ToUserID)
	if err != nil || to == nil || to.TenantID != sub.TenantID {
		g.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "目标用户不在本团队"}})
		return
	}
	fromBal, toBal, err := s.Store.TransferAtomic(g.Request.Context(), sub.TenantID, sub.UserID, req.ToUserID, req.AmountCents)
	if err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}
	s.audit(g, "team.transfer", req.ToUserID)
	g.JSON(http.StatusOK, gin.H{"data": gin.H{"from_balance_cents": fromBal, "to_balance_cents": toBal}})
}

// teamUsage 团队用量聚合(按模型,近 30 天)。
func (s *Server) teamUsage(g *gin.Context) {
	sub := mustSub(g)
	rows, err := s.Store.Aggregate(g.Request.Context(), store.AggParams{
		Scope: "tenant", ScopeID: sub.TenantID, GroupBy: "model", Bucket: "day",
	})
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	g.JSON(http.StatusOK, gin.H{"data": rows})
}

// teamChannels 团队 BYOK + 平台默认渠道(只读视图)。
func (s *Server) teamChannels(g *gin.Context) {
	sub := mustSub(g)
	chs, err := s.Store.ListChannels(g.Request.Context(), sub.TenantID)
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	out := make([]gin.H, 0, len(chs))
	for _, c := range chs {
		own := "platform"
		if c.TenantID != nil {
			own = "team"
		}
		out = append(out, gin.H{"id": c.ID, "name": c.Name, "provider": c.Provider,
			"owner": own, "status": c.Status, "models": c.ModelNames()})
	}
	g.JSON(http.StatusOK, gin.H{"data": out})
}

type createInviteReq struct {
	Role string `json:"role"` // member | admin,默认 member
}

// createInvite 生成签名邀请链接(明文 token 只返回一次)。
func (s *Server) createInvite(g *gin.Context) {
	if !s.requireTeamAdmin(g) {
		return
	}
	sub := mustSub(g)
	var req createInviteReq
	_ = g.ShouldBindJSON(&req)
	role := strings.TrimSpace(req.Role)
	if role != string(model.RoleAdmin) && role != string(model.RoleMember) {
		role = string(model.RoleMember)
	}
	plain, err := crypto.RandomHex(16)
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	t := &model.InviteToken{
		ID: cryptoID(), TokenHash: crypto.APIKeyHash(plain), TenantID: sub.TenantID,
		Role: role, CreatedBy: sub.UserID, ExpiresAt: time.Now().Add(7 * 24 * time.Hour), CreatedAt: time.Now(),
	}
	if err := s.Store.CreateInvite(g.Request.Context(), t); err != nil {
		s.respondInternal(g, err)
		return
	}
	s.audit(g, "team.invite_create", t.ID)
	g.JSON(http.StatusOK, gin.H{"data": gin.H{
		"id": t.ID, "token": plain,
		"link": originURL(g) + "/#/invite?token=" + plain,
		"role": role, "expires_at": t.ExpiresAt.Unix(),
	}})
}

// listInvites 团队邀请列表。
func (s *Server) listInvites(g *gin.Context) {
	if !s.requireTeamAdmin(g) {
		return
	}
	sub := mustSub(g)
	ts, err := s.Store.ListInvites(g.Request.Context(), sub.TenantID)
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	out := make([]gin.H, 0, len(ts))
	for _, t := range ts {
		out = append(out, gin.H{"id": t.ID, "role": t.Role, "expires_at": t.ExpiresAt.Unix(),
			"used": t.UsedAt != nil, "used_by": t.UsedBy, "created_at": t.CreatedAt.Unix()})
	}
	g.JSON(http.StatusOK, gin.H{"data": out})
}

// revokeInvite 吊销(删除)邀请。
func (s *Server) revokeInvite(g *gin.Context) {
	if !s.requireTeamAdmin(g) {
		return
	}
	sub := mustSub(g)
	id := g.Param("id")
	if err := s.Store.RevokeInvite(g.Request.Context(), id, sub.TenantID); err != nil {
		g.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}
	s.audit(g, "team.invite_revoke", id)
	g.JSON(http.StatusOK, gin.H{"data": gin.H{"ok": true}})
}

// inviteInfo 公开:凭 token 查邀请信息(供邀请页展示团队名),不泄露成员。
func (s *Server) inviteInfo(g *gin.Context) {
	token := g.Query("token")
	if token == "" {
		g.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "missing token"}})
		return
	}
	t, err := s.Store.GetInviteByToken(g.Request.Context(), crypto.APIKeyHash(token))
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	valid := t != nil && t.UsedAt == nil && time.Now().Before(t.ExpiresAt)
	name := ""
	if t != nil {
		if tn, e := s.Store.GetTenant(g.Request.Context(), t.TenantID); e == nil && tn != nil {
			name = tn.Name
		}
	}
	g.JSON(http.StatusOK, gin.H{"data": gin.H{"valid": valid, "tenant_name": name,
		"role": ternaryStr(t != nil, t.Role, ""), "expires_at": ternaryInt64(t != nil, t.ExpiresAt.Unix(), 0)}})
}

type acceptInviteReq struct {
	Token    string `json:"token" binding:"required"`
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// inviteAccept 公开:凭邀请 token 注册并加入目标租户(保 1 用户 1 租户)。
// 邀请认领与建账号在同一事务(CAS),杜绝并发重复接受/越权入伙。
func (s *Server) inviteAccept(g *gin.Context) {
	var req acceptInviteReq
	if err := g.ShouldBindJSON(&req); err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "token/email/password 必填"}})
		return
	}
	ctx := g.Request.Context()
	email := strings.ToLower(strings.TrimSpace(req.Email))

	// 预检(best-effort,真正一致性由 AcceptInviteAtomic 的 CAS + 唯一约束保证)。
	if existing, _ := s.tenantsByEmail(g, email); len(existing) > 0 {
		g.JSON(http.StatusConflict, gin.H{"error": gin.H{"message": "该邮箱已注册,如需加入请用原账号"}})
		return
	}
	// 租户须启用(禁用团队不再接纳成员)。
	if t, err := s.Store.GetInviteByToken(ctx, crypto.APIKeyHash(req.Token)); err == nil && t != nil {
		if tn, e := s.Store.GetTenant(ctx, t.TenantID); e != nil || tn == nil || tn.Status != "active" {
			g.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "团队不可用"}})
			return
		}
	} else if err != nil {
		s.respondInternal(g, err)
		return
	}

	pwHash, err := crypto.HashPassword(req.Password)
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	inv, userID, err := s.Store.AcceptInviteAtomic(ctx, crypto.APIKeyHash(req.Token), email, pwHash, time.Now())
	if err != nil {
		if errors.Is(err, store.ErrInviteUnavailable) {
			g.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "邀请无效或已过期"}})
			return
		}
		s.respondInternal(g, err)
		return
	}
	_ = s.Store.InsertAudit(ctx, userID, "team.invite_accept", inv.TenantID, g.ClientIP(), nil)
	s.issueAndReturn(g, userID, inv.TenantID, inv.Role, req.Email)
}

// originURL 取请求来源(scheme://host),用于拼邀请链接,无需新配置。
func originURL(g *gin.Context) string {
	scheme := "http"
	if g.Request.TLS != nil {
		scheme = "https"
	} else if p := g.GetHeader("X-Forwarded-Proto"); p != "" {
		scheme = p
	}
	return scheme + "://" + g.Request.Host
}

func cryptoID() string {
	id, err := crypto.RandomHex(16)
	if err != nil {
		return "id-" + time.Now().Format("20060102150405")
	}
	return id
}

func ternaryStr(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}
func ternaryInt64(cond bool, a, b int64) int64 {
	if cond {
		return a
	}
	return b
}
