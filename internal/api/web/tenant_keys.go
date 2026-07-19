package web

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// --- 租户级 API Key 统一管理(租户管理员可见/吊销本租户任意成员的密钥) ---
// 解决痛点: 成员离职后其 API Key 此前只能逐用户禁用间接清理;现支持租户管理员直接吊销。

// adminListTenantKeys 列出本租户全部密钥(platform 管理员可按 tenant_id 查询任意租户)。
func (s *Server) adminListTenantKeys(g *gin.Context) {
	sub := mustSub(g)
	tenant := ""
	if !sub.IsPlatformAdmin() {
		tenant = sub.TenantID // 租户管理员强制限定本租户
	} else if q := g.Query("tenant_id"); q != "" {
		tenant = q
	}
	keys, err := s.Store.ListAPIKeysByTenant(g.Request.Context(), tenant)
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	g.JSON(http.StatusOK, gin.H{"data": keys})
}

// adminRevokeTenantKey 吊销本租户内任意成员的密钥(scope 校验 + 清鉴权缓存即时生效)。
func (s *Server) adminRevokeTenantKey(g *gin.Context) {
	sub := mustSub(g)
	id := g.Param("id")
	var hash string
	var err error
	if sub.IsPlatformAdmin() {
		hash, err = s.Store.RevokeAPIKeyAny(g.Request.Context(), id)
	} else {
		hash, err = s.Store.RevokeAPIKeyScoped(g.Request.Context(), id, sub.TenantID)
	}
	if err != nil {
		g.JSON(http.StatusNotFound, gin.H{"error": "api key not found"})
		return
	}
	// 立即清理鉴权缓存,被吊销的密钥在缓存 TTL(2min)外不再有效。
	if s.RDB != nil && hash != "" {
		_ = s.RDB.Del(g.Request.Context(), "apikey:"+hash).Err()
	}
	s.audit(g, "tenantkey.revoke", id)
	s.ok(g)
}
