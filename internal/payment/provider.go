// Package payment 封装第三方支付(微信支付 Native / 支付宝电脑网站支付 / mock)。
//
// 设计: Provider 抽象屏蔽各渠道差异;Service 编排"下单 → 回调入账 → 查单兜底 → 超时关单"。
// 入账统一委托 billing.Service(复用 AdjustAtomic: 加余额 + 写 recharges + 写 ledger),
// 幂等由 store.MarkPaid 的 pending→paid 状态机保证: 回调/查单重入时余额只加一次。
package payment

import "net/http"

// CreateOrderInput 创建订单入参(渠道无关)。
type CreateOrderInput struct {
	OutTradeNo  string // 商户订单号(幂等键)
	AmountCents int64  // 金额,整数分
	Subject     string // 订单标题/商品描述
	NotifyURL   string // 异步回调地址(完整 URL)
	ReturnURL   string // 支付完成前端回跳地址(支付宝用)
}

// Provider 支付渠道抽象。各方法内含签名/验签,调用方无需关心。
type Provider interface {
	// Name 渠道标识(wechat | alipay | mock)。
	Name() string
	// CreateOrder 下单,返回预支付数据: 微信=code_url(二维码内容), 支付宝=跳转 URL。
	CreateOrder(in CreateOrderInput) (prepayData string, err error)
	// QueryOrder 主动查单(防回调丢失),返回是否已支付及支付平台流水号。
	QueryOrder(outTradeNo string) (paid bool, txnID string, err error)
	// ParseNotify 解析并验签异步回调,返回商户订单号/流水号/实付金额(分)。
	ParseNotify(r *http.Request) (outTradeNo, txnID string, amountCents int64, err error)
}
