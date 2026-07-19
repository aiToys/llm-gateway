// Package web 提供用户端与管理端 REST API。
package web

import (
	"github.com/aitoys/llm-gateway/internal/auth"
	"github.com/aitoys/llm-gateway/internal/billing"
	"github.com/aitoys/llm-gateway/internal/crypto"
	"github.com/aitoys/llm-gateway/internal/files"
	"github.com/aitoys/llm-gateway/internal/middleware"
	"github.com/aitoys/llm-gateway/internal/payment"
	"github.com/aitoys/llm-gateway/internal/relay"
	"github.com/aitoys/llm-gateway/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// Server Web API 服务。
type Server struct {
	Store       *store.Store
	Auth        *auth.Service
	Billing     *billing.Service
	Relay       *relay.Service
	Cipher      *crypto.Cipher
	RDB         *redis.Client
	FileSvc     *files.Service
	Payment     *payment.Service // 支付服务(下单/回调/查单);为 nil 时支付入口不可用
	Dev         bool              // 开发模式: 放开模拟充值、注册
	AllowSignup bool
}

// Register 注册全部路由(group 相对于 engine)。
func (s *Server) Register(r *gin.Engine) {
	api := r.Group("/api")
	// 公开(无需登录)
	pub := api.Group("/public")
	{
		pub.GET("/models", s.publicModels)
		pub.GET("/providers", s.publicProviders)
	}
	authg := api.Group("/auth")
	{
		authg.POST("/register", s.register)
		authg.POST("/login", s.login)
	}
	// 支付回调: 公开(无 JWT),靠各 provider 内部验签鉴权。
	{
		api.POST("/payments/:provider/notify", s.paymentNotify)
	}
	// 团队邀请: 公开(凭邀请 token 自鉴)。
	{
		api.GET("/invites/info", s.inviteInfo)
		api.POST("/invites/accept", s.inviteAccept)
	}
	// 需登录
	jwt := api.Group("", middleware.JWTAuth(s.Auth))
	{
		jwt.GET("/me", s.me)
		jwt.GET("/me/models", s.meModelPrefs)
		jwt.PUT("/me/models/:name/enabled", s.meSetModelEnabled)
		jwt.GET("/models", s.listModels)
		jwt.GET("/keys", s.listKeys)
		jwt.POST("/keys", s.createKey)
		jwt.DELETE("/keys/:id", s.revokeKey)
		jwt.GET("/usage/day", s.usageByDay)
		jwt.GET("/usage/ledger", s.usageLedger)
		jwt.GET("/usage/aggregate", s.usageAggregate)
		jwt.POST("/recharge", s.recharge)
		jwt.POST("/recharge/order", s.createRechargeOrder)
		jwt.GET("/recharge/order/:no", s.getOrderStatus)
		// 团队协作: 登录后访问,handler 内按本租户收窄;管理操作 requireTeamAdmin。
		jwt.GET("/team", s.teamInfo)
		jwt.PUT("/team", s.teamUpdate)
		jwt.GET("/team/members", s.teamMembers)
		jwt.POST("/team/transfer", s.teamTransfer)
		jwt.GET("/team/usage", s.teamUsage)
		jwt.GET("/team/channels", s.teamChannels)
		jwt.POST("/team/invites", s.createInvite)
		jwt.GET("/team/invites", s.listInvites)
		jwt.DELETE("/team/invites/:id", s.revokeInvite)
		jwt.POST("/playground/chat", s.playgroundChat)
		jwt.POST("/playground/chat/stream", s.playgroundChatStream)
		jwt.POST("/playground/upload", s.playgroundUpload)
	}
	// 管理端 — 按权限二分:
	//   RequireAdmin        : 平台管理员 + 租户管理员(handler 内按 sub.TenantID 收窄,仅本租户)。
	//   RequirePlatformAdmin: 仅平台管理员(跨租户全局资源: 租户/全局模型定价/账目/审计/统计)。
	admin := api.Group("/admin", middleware.JWTAuth(s.Auth), middleware.RequireAdmin())
	{
		// 用户与渠道: 租户管理员仅可见/可改本租户范围(见 handler 内 adminScope/ensureChannelScope)。
		admin.GET("/users", s.adminListUsers)
		admin.POST("/users", s.adminCreateUser)
		admin.PATCH("/users/:id/status", s.adminSetUserStatus)
		admin.PUT("/users/:id", s.adminUpdateUser)
		admin.POST("/users/:id/password", s.adminResetPassword)
		admin.POST("/users/:id/balance", s.adminAdjustBalance)
		admin.GET("/channels", s.adminListChannels)
		admin.POST("/channels", s.adminCreateChannel)
		admin.PUT("/channels/:id", s.adminUpdateChannel)
		admin.DELETE("/channels/:id", s.adminDeleteChannel)
		admin.PATCH("/channels/:id/status", s.adminSetChannelStatus)
		admin.PATCH("/channels/:id/routing", s.adminUpdateChannelRouting)
		admin.PATCH("/channels/:id/models/:model/status", s.adminSetChannelModelStatus)
		admin.POST("/channels/:id/models/:model", s.adminAddChannelModel)
		admin.DELETE("/channels/:id/models/:model", s.adminRemoveChannelModel)
		admin.POST("/channels/:id/test", s.adminTestChannel)
		admin.GET("/request-logs", s.adminListRequestLogs)
		admin.GET("/request-logs/:id", s.adminGetRequestLog)
		admin.GET("/tenant-keys", s.adminListTenantKeys)
		admin.DELETE("/tenant-keys/:id", s.adminRevokeTenantKey)
		// 模型清单只读: 全局模型对所有管理员可见(定价元数据本身非敏感)。
		admin.GET("/models", s.adminListModels)
	}
	// 平台级管理(跨租户): 租户管理、全局模型定价写入、账目、审计、统计、全局用量。
	platform := api.Group("/admin", middleware.JWTAuth(s.Auth), middleware.RequirePlatformAdmin())
	{
		platform.GET("/stats", s.adminStats)
		platform.GET("/tenants", s.adminListTenants)
		platform.POST("/tenants", s.adminCreateTenant)
		platform.PUT("/tenants/:id", s.adminUpdateTenant)
		platform.PATCH("/tenants/:id/status", s.adminSetTenantStatus)
		platform.POST("/models", s.adminUpsertModel)
		platform.DELETE("/models/:name", s.adminDeleteModel)
		platform.GET("/ledger", s.adminLedger)
		platform.GET("/ledger/export", s.adminLedgerExport)
		platform.GET("/usage/aggregate", s.adminUsageAggregate)
		platform.GET("/audit", s.adminAudit)
	}
}
