package web

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type rechargeReq struct {
	AmountCents int64 `json:"amount_cents" binding:"required"`
}

// recharge 开发期模拟支付: 直接给当前用户加余额。
// 生产环境(Dev=false)禁用——必须接入真实支付回调,否则任意用户可无限自充值绕过计费。
func (s *Server) recharge(g *gin.Context) {
	if !s.Dev {
		g.JSON(http.StatusForbidden, gin.H{"error": "模拟充值仅在开发模式(dev)开启;生产环境请通过支付回调充值"})
		return
	}
	sub := mustSub(g)
	var req rechargeReq
	if err := g.ShouldBindJSON(&req); err != nil || req.AmountCents <= 0 {
		g.JSON(http.StatusBadRequest, gin.H{"error": "amount_cents must be positive"})
		return
	}
	nb, err := s.Billing.Recharge(g.Request.Context(), sub.TenantID, sub.UserID, req.AmountCents)
	if err != nil {
		s.respondInternal(g, err)
		return
	}
	g.JSON(http.StatusOK, gin.H{"balance_cents": nb})
}
