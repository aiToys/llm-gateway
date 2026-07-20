package web

import (
	"net/http"
	"strings"
	"time"

	"github.com/aitoys/llm-gateway/internal/auth"
	"github.com/aitoys/llm-gateway/internal/crypto"
	"github.com/aitoys/llm-gateway/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type registerReq struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
	Tenant   string `json:"tenant,omitempty"`
}

// register 自助注册: 创建个人租户 + admin 用户(该 admin 仅对其自有租户有管理权)。
// 默认关闭(auth.allow_signup=false);开放注册的部署需显式配置,且应评估滥用风险。
func (s *Server) register(g *gin.Context) {
	if !s.AllowSignup {
		g.JSON(http.StatusForbidden, gin.H{"error": "自助注册未开放;请联系管理员开通账号"})
		return
	}
	var req registerReq
	if !s.bindJSON(g, &req) {
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	tenantName := req.Tenant
	if tenantName == "" {
		tenantName = req.Email + "'s workspace"
	}
	tenantID := uuid.NewString()
	now := time.Now()
	if err := s.Store.CreateTenant(g.Request.Context(), &model.Tenant{
		ID: tenantID, Name: tenantName, Slug: "t-" + tenantID[:8], Status: "active", CreatedAt: now,
	}); err != nil {
		s.respondInternal(g, err)
		return
	}
	pwHash, err := crypto.HashPassword(req.Password)
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	userID := uuid.NewString()
	if err := s.Store.CreateUser(g.Request.Context(), &model.User{
		ID: userID, TenantID: tenantID, Email: req.Email, PasswordHash: pwHash,
		Role: model.RoleAdmin, Status: "active", BalanceCents: 0, CreatedAt: now,
	}); err != nil {
		s.respondInternal(g, err)
		return
	}
	s.audit(g, "user.register", req.Email)
	s.issueAndReturn(g, userID, tenantID, string(model.RoleAdmin), req.Email)
}

type loginReq struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

func (s *Server) login(g *gin.Context) {
	var req loginReq
	if !s.bindJSON(g, &req) {
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	// login 需要租户;允许跨租户查询(取第一个匹配 email 的用户)。
	tenantIDs, err := s.tenantsByEmail(g, req.Email)
	if err != nil {
		// DB 不可用时不应伪装成"凭据无效":否则用户无论怎么试都 401,运维也看不到 5xx 重试信号。
		s.respondInternal(g, err)
		return
	}
	var u *model.User
	for _, tid := range tenantIDs {
		if got, err := s.Store.GetUserByEmail(g.Request.Context(), tid, req.Email); err == nil {
			u = got
			break
		}
	}
	if u == nil {
		s.audit(g, "user.login_failed", req.Email+":not_found")
		g.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	if !crypto.VerifyPassword(u.PasswordHash, req.Password) {
		s.audit(g, "user.login_failed", req.Email+":bad_password")
		g.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	if u.Status != "active" {
		s.audit(g, "user.login_failed", req.Email+":disabled")
		g.JSON(http.StatusForbidden, gin.H{"error": "user disabled"})
		return
	}
	s.audit(g, "user.login", u.Email)
	s.issueAndReturn(g, u.ID, u.TenantID, string(u.Role), u.Email)
}

func (s *Server) issueAndReturn(g *gin.Context, uid, tid, role, email string) {
	tok, exp, err := s.Auth.Issue(auth.Subject{UserID: uid, TenantID: tid, Role: role, Email: email})
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	g.JSON(http.StatusOK, gin.H{
		"token":      tok,
		"expires_at": exp.Unix(),
		"user":       gin.H{"id": uid, "tenant_id": tid, "role": role, "email": email},
	})
}

func (s *Server) me(g *gin.Context) {
	sub := mustSub(g)
	u, err := s.Store.GetUser(g.Request.Context(), sub.UserID)
	if err != nil {
		g.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{
		"id":            u.ID,
		"tenant_id":     u.TenantID,
		"email":         u.Email,
		"role":          u.Role,
		"balance_cents": u.BalanceCents,
	})
}

// meModelPrefs 返回平台启用的模型清单,并标注本租户是否启用(默认启用)。
// 租户可自主关闭某模型,关闭后该租户调用被拒绝(见 EffectivePrice)。
func (s *Server) meModelPrefs(g *gin.Context) {
	sub := mustSub(g)
	ms, err := s.Store.ListModels(g.Request.Context(), true)
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	ovs, _ := s.Store.ListTenantOverrides(g.Request.Context(), sub.TenantID)
	enabledMap := map[string]bool{}
	for _, o := range ovs {
		enabledMap[o.ModelName] = o.Enabled
	}
	if chs, err := s.Store.ListChannels(g.Request.Context(), ""); err == nil {
		attachProviders(ms, chs)
	}
	out := make([]gin.H, 0, len(ms))
	for _, m := range ms {
		_, overridden := enabledMap[m.ModelName]
		tenantEnabled := true
		if overridden {
			tenantEnabled = enabledMap[m.ModelName]
		}
		out = append(out, gin.H{
			"model_name":               m.ModelName,
			"description":              m.Description,
			"providers":                m.Providers,
			"capabilities":             m.Capabilities,
			"input_price_cents_per_m":  m.InputPriceCentsPerM,
			"output_price_cents_per_m": m.OutputPriceCentsPerM,
			"tenant_enabled":           tenantEnabled,
		})
	}
	g.JSON(http.StatusOK, gin.H{"data": out})
}

// meSetModelEnabled 租户启停某模型(写入 tenant_model_overrides)。
func (s *Server) meSetModelEnabled(g *gin.Context) {
	sub := mustSub(g)
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !s.bindJSON(g, &req) {
		return
	}
	name := g.Param("name")
	// 取平台价作为首次写入的兜底(已存在 override 时不会覆盖价格)。
	gm, err := s.Store.GetModel(g.Request.Context(), name)
	if err != nil {
		g.JSON(http.StatusNotFound, gin.H{"error": "model not found"})
		return
	}
	if err := s.Store.SetTenantModelEnabled(g.Request.Context(), sub.TenantID, name, req.Enabled, gm.InputPriceCentsPerM, gm.OutputPriceCentsPerM); err != nil {
		s.respondInternal(g, err)
		return
	}
	s.audit(g, "tenant.model_toggle", name+"="+boolStr(req.Enabled))
	s.ok(g)
}

func boolStr(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// tenantsByEmail 取包含某 email 的租户 id 列表(login 用)。
func (s *Server) tenantsByEmail(g *gin.Context, email string) ([]string, error) {
	rows, err := s.Store.Pool.Query(g.Request.Context(),
		`SELECT tenant_id FROM users WHERE email=$1`, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// mustSub 取出注入的主体。用类型断言双判定,避免中间件链异常未注入 subject 时 panic。
func mustSub(g *gin.Context) auth.Subject {
	if v, ok := g.Get("subject"); ok {
		if sub, ok := v.(auth.Subject); ok {
			return sub
		}
	}
	return auth.Subject{}
}
