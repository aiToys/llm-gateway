package web

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/aitoys/llm-gateway/internal/metrics"
	"github.com/aitoys/llm-gateway/internal/store"
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

// redeem 凭兑换码(卡密)充值: 原子消耗卡密(active→used)并加余额,单事务保证账实一致。
// 与模拟 recharge 不同,生产环境也开放——卡密是真实付款凭证,等同已支付。
func (s *Server) redeem(g *gin.Context) {
	sub := mustSub(g)
	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if !s.bindJSON(g, &req) {
		return
	}
	code := strings.TrimSpace(req.Code)
	if code == "" {
		g.JSON(http.StatusBadRequest, gin.H{"error": "兑换码不能为空"})
		return
	}
	amount, nb, err := s.Store.RedeemCodeAtomic(g.Request.Context(), code, sub.UserID, sub.TenantID)
	if err != nil {
		if errors.Is(err, store.ErrInvalidRedeemCode) {
			g.JSON(http.StatusNotFound, gin.H{"error": "兑换码无效或已使用"})
			return
		}
		s.respondInternal(g, err)
		return
	}
	metrics.ObserveCharge("recharge", amount)
	s.audit(g, "user.redeem", fmt.Sprintf("%d cents", amount))
	g.JSON(http.StatusOK, gin.H{"balance_cents": nb, "amount_cents": amount})
}
