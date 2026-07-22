package web

import (
	"net/http"

	"github.com/aitoys/llm-gateway/internal/model"
	"github.com/gin-gonic/gin"
)

// strPtr 解引用可空字符串,nil 时返回空串(前端渲染二维码用,空即隐藏)。
func strPtr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

type createOrderReq struct {
	AmountCents int64  `json:"amount_cents" binding:"required"`
	Provider    string `json:"provider" binding:"required"` // wechat | alipay | mock
}

// createRechargeOrder 创建支付订单并下单,返回预支付数据。
//   - 微信: prepay_data 为 code_url,前端渲染二维码。
//   - 支付宝: prepay_data 为跳转 URL,前端 location.href。
func (s *Server) createRechargeOrder(g *gin.Context) {
	if s.Payment == nil || !s.Payment.HasAny() {
		g.JSON(http.StatusServiceUnavailable, gin.H{"error": "未配置支付渠道"})
		return
	}
	sub := mustSub(g)
	var req createOrderReq
	if err := g.ShouldBindJSON(&req); err != nil || req.AmountCents <= 0 {
		g.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "amount_cents 必须为正数、provider 必填"}})
		return
	}
	res, err := s.Payment.CreateOrder(g.Request.Context(), sub.TenantID, sub.UserID, req.Provider, "账户充值", req.AmountCents)
	if err != nil {
		g.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	g.JSON(http.StatusOK, gin.H{"data": res})
}

// getOrderStatus 查询订单状态(前端轮询用)。后端顺带主动查单兜底,防回调丢失。
func (s *Server) getOrderStatus(g *gin.Context) {
	if s.Payment == nil {
		g.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "未配置支付渠道"}})
		return
	}
	sub := mustSub(g)
	no := g.Param("no")
	o, err := s.Payment.OrderStatus(g.Request.Context(), sub.UserID, no)
	if err != nil {
		g.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}
	// 余额: paid 后前端需刷新;若刚入账则附带新余额,省一次 /me 往返。
	var bal *int64
	if o.Status == model.PaymentStatusPaid {
		if u, err := s.Store.GetUser(g.Request.Context(), sub.UserID); err == nil {
			b := u.BalanceCents
			bal = &b
		}
	}
	g.JSON(http.StatusOK, gin.H{"data": gin.H{
		"order_no":      o.OutTradeNo,
		"provider":      o.Provider,
		"amount_cents":  o.AmountCents,
		"status":        o.Status,
		"prepay_data":   strPtr(o.PrepayData),
		"expires_at":    o.ExpiresAt.Unix(),
		"balance_cents": bal,
	}})
}

// paymentNotify 渠道异步回调(公开,靠各 provider 内部验签)。
// 成功/已入账均返回渠道要求的 ACK;验签失败返回非 2xx 促使渠道重试。
func (s *Server) paymentNotify(g *gin.Context) {
	if s.Payment == nil {
		g.JSON(http.StatusServiceUnavailable, gin.H{"message": "未配置支付渠道"})
		return
	}
	provider := g.Param("provider")
	if err := s.Payment.HandleNotify(g.Request.Context(), provider, g.Request); err != nil {
		// 验签失败等: 返回 400,微信/支付宝会重试通知。
		g.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	// 微信要求返回 200 + JSON {"code":"SUCCESS"};支付宝要求纯文本 "success"。
	if provider == model.ProviderAlipay {
		g.String(http.StatusOK, "success")
		return
	}
	g.JSON(http.StatusOK, gin.H{"code": "SUCCESS", "message": "成功"})
}
