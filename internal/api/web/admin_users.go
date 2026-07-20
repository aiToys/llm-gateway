package web

import (
	"net/http"
	"strings"
	"time"

	"github.com/aitoys/llm-gateway/internal/crypto"
	"github.com/aitoys/llm-gateway/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

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
