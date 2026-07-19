package payment

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/aitoys/llm-gateway/internal/billing"
	"github.com/aitoys/llm-gateway/internal/crypto"
	"github.com/aitoys/llm-gateway/internal/logging"
	"github.com/aitoys/llm-gateway/internal/metrics"
	"github.com/aitoys/llm-gateway/internal/model"
	"github.com/aitoys/llm-gateway/internal/store"
)

// Service 编排支付全流程: 下单、回调入账、主动查单兜底、超时关单。
// 幂等核心: 入账走 store.SettlePaymentAtomic 单事务(pending→paid + 加余额 + 写账目),
// 仅当本次完成 pending→paid 才加余额,回调重入/查单重复/重复通知都只加一次。
type Service struct {
	Store      *store.Store
	Billing    *billing.Service
	providers  map[string]Provider // name -> provider
	BaseURL    string             // 站点根 URL,用于拼回调地址
	ExpiresMin int                // 订单有效期(分钟)
}

func New(st *store.Store, b *billing.Service, baseURL string, expiresMin int) *Service {
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	if expiresMin <= 0 {
		expiresMin = 15
	}
	return &Service{Store: st, Billing: b, providers: map[string]Provider{}, BaseURL: baseURL, ExpiresMin: expiresMin}
}

// Register 注册渠道(仅配置启用的渠道才注册,未注册的渠道下单时返回错误)。
func (s *Service) Register(p Provider) {
	if p != nil {
		s.providers[p.Name()] = p
	}
}

// HasAny 是否有可用渠道(决定前端是否展示支付入口)。
func (s *Service) HasAny() bool { return len(s.providers) > 0 }

// AvailableProviders 返回已注册渠道名,供前端渲染可选支付方式。
func (s *Service) AvailableProviders() []string {
	out := make([]string, 0, len(s.providers))
	for name := range s.providers {
		out = append(out, name)
	}
	return out
}

// CreateOrderResult 创建订单返回(前端据此渲染二维码或跳转)。
type CreateOrderResult struct {
	OrderNo    string `json:"order_no"`
	Provider   string `json:"provider"`
	PrepayData string `json:"prepay_data"` // wechat: code_url; alipay: 跳转 URL
	ExpiresAt  int64  `json:"expires_at"`  // unix 秒
}

// CreateOrder 创建支付订单并调用渠道下单。
func (s *Service) CreateOrder(ctx context.Context, tenantID, userID, providerName, subject string, amountCents int64) (*CreateOrderResult, error) {
	if amountCents <= 0 {
		return nil, fmt.Errorf("金额必须大于 0")
	}
	p, ok := s.providers[providerName]
	if !ok {
		return nil, fmt.Errorf("不支持的支付渠道: %s", providerName)
	}
	outTradeNo, err := newOutTradeNo()
	if err != nil {
		return nil, fmt.Errorf("gen order no: %w", err)
	}
	now := time.Now()
	o := &model.PaymentOrder{
		ID:          mustID(),
		TenantID:    tenantID,
		UserID:      userID,
		OutTradeNo:  outTradeNo,
		Provider:    providerName,
		AmountCents: amountCents,
		Status:      model.PaymentStatusPending,
		ExpiresAt:   now.Add(time.Duration(s.ExpiresMin) * time.Minute),
		CreatedAt:   now,
	}
	prepay, err := p.CreateOrder(CreateOrderInput{
		OutTradeNo:  outTradeNo,
		AmountCents: amountCents,
		Subject:     subject,
		NotifyURL:   s.BaseURL + "/api/payments/" + providerName + "/notify",
		ReturnURL:   s.BaseURL + "/console/recharge?order=" + outTradeNo,
	})
	if err != nil {
		return nil, fmt.Errorf("create %s order: %w", providerName, err)
	}
	o.PrepayData = &prepay
	if err := s.Store.CreatePaymentOrder(ctx, o); err != nil {
		return nil, fmt.Errorf("save order: %w", err)
	}
	return &CreateOrderResult{
		OrderNo:    outTradeNo,
		Provider:   providerName,
		PrepayData: prepay,
		ExpiresAt:  o.ExpiresAt.Unix(),
	}, nil
}

// OrderStatus 查询订单状态。顺带主动查单一次(防回调丢失):若渠道侧已支付而本地仍 pending,
// 立即入账。这样前端轮询即兜底,无需依赖回调必达。
func (s *Service) OrderStatus(ctx context.Context, userID, outTradeNo string) (*model.PaymentOrder, error) {
	o, err := s.Store.GetPaymentOrderByTradeNo(ctx, outTradeNo)
	if err != nil {
		return nil, err
	}
	if o == nil {
		return nil, fmt.Errorf("订单不存在")
	}
	if o.UserID != userID {
		return nil, fmt.Errorf("订单不存在") // 越权访问不暴露存在性
	}
	// pending 且未过期时,主动向渠道查一次(兜底入账)。
	if o.Status == model.PaymentStatusPending && time.Now().Before(o.ExpiresAt) {
		if p, ok := s.providers[o.Provider]; ok {
			if paid, txnID, qerr := p.QueryOrder(outTradeNo); qerr == nil && paid {
				_ = s.settle(ctx, o, txnID) // 幂等: 内部 MarkPaid 保证只入账一次
				o, _ = s.Store.GetPaymentOrderByTradeNo(ctx, outTradeNo) // 刷新状态返回
			}
		}
	}
	return o, nil
}

// HandleNotify 处理渠道异步回调(HTTP 入口调用)。成功入账返回 nil;验签失败/状态非成功返回错误。
// 调用方据此返回渠道要求的 ACK(微信 JSON / 支付宝 "success")。
//
// 入账用独立超时上下文: 微信/支付宝回调 5~10s 超时,若用 request ctx,客户端(支付平台)断连
// 会取消正在进行的 PG 事务 → 资金不一致。故 settle 走 detached ctx。
func (s *Service) HandleNotify(ctx context.Context, providerName string, r *http.Request) error {
	p, ok := s.providers[providerName]
	if !ok {
		return fmt.Errorf("unknown provider: %s", providerName)
	}
	outTradeNo, txnID, amount, err := p.ParseNotify(r)
	if err != nil {
		if errors.Is(err, errNotifyIgnored) {
			return nil // 状态非成功,正常 ACK,不入账
		}
		return err // 验签失败等,调用方不应 ACK,促使渠道重试
	}
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	o, err := s.Store.GetPaymentOrderByTradeNo(sctx, outTradeNo)
	if err != nil {
		return err
	}
	if o == nil {
		logging.From(sctx).Warn("payment notify for unknown order", "out_trade_no", outTradeNo, "provider", providerName)
		return nil // 订单不存在仍 ACK,避免无限重试(可能是测试/脏数据)
	}
	// 金额校验: 渠道返回金额必须与订单完全一致。amount=0(渠道字段缺失 / 攻击者构造无金额回调)
	// 同样拒绝——否则会按订单原价全额入账,构成白嫖(mock 渠道或上游字段缺失场景)。
	if amount != o.AmountCents {
		logging.From(sctx).Error("payment notify amount mismatch, refuse to settle",
			"out_trade_no", outTradeNo, "order_cents", o.AmountCents, "notify_cents", amount)
		return fmt.Errorf("amount mismatch: order %d notify %d", o.AmountCents, amount)
	}
	return s.settle(sctx, o, txnID)
}

// settle 幂等入账: 单事务完成"订单 pending→paid + 加余额 + 写 recharges + 写 ledger"。
// 重复回调/查单重入时 SettlePaymentAtomic 返回 newlyPaid=false,余额只加一次。
func (s *Service) settle(ctx context.Context, o *model.PaymentOrder, txnID string) error {
	newlyPaid, _, err := s.Store.SettlePaymentAtomic(ctx, o.OutTradeNo, txnID, time.Now())
	if err != nil {
		logging.From(ctx).Error("settle payment failed (order still pending, will retry on next notify/sweep)",
			"out_trade_no", o.OutTradeNo, "amount_cents", o.AmountCents, "err", err)
		return fmt.Errorf("settle payment: %w", err)
	}
	if !newlyPaid {
		return nil // 已入账(重复回调),幂等
	}
	metrics.ObserveCharge("recharge", o.AmountCents)
	logging.From(ctx).Info("payment settled", "out_trade_no", o.OutTradeNo, "provider", o.Provider, "amount_cents", o.AmountCents)
	return nil
}

// CloseExpired 扫描已过期仍 pending 的订单:先向渠道查单确认未付,再关单(双重确认避免误关已付单)。
// 由后台 goroutine 周期调用。
func (s *Service) CloseExpired(ctx context.Context) {
	expired, err := s.Store.ListPendingBefore(ctx, time.Now())
	if err != nil {
		logging.From(ctx).Warn("list expired payment orders failed", "err", err)
		return
	}
	for _, o := range expired {
		// 先查渠道:若已支付则补入账,避免把已付款订单关掉。
		if p, ok := s.providers[o.Provider]; ok {
			paid, txnID, qerr := p.QueryOrder(o.OutTradeNo)
			if qerr != nil {
				// 查单失败(网络抖动等):本轮跳过关单,留下次再确认,绝不在未确认时关单(防漏账)。
				logging.From(ctx).Warn("skip closing order: query provider failed", "out_trade_no", o.OutTradeNo, "err", qerr)
				continue
			}
			if paid {
				_ = s.settle(ctx, o, txnID)
				continue
			}
		}
		if err := s.Store.MarkClosed(ctx, o.OutTradeNo); err != nil {
			logging.From(ctx).Warn("close expired order failed", "out_trade_no", o.OutTradeNo, "err", err)
		}
	}
}

// newOutTradeNo 商户订单号: 时间戳 + 随机,需在商户端全局唯一。
func newOutTradeNo() (string, error) {
	r, err := crypto.RandomHex(8) // 16 hex 字符
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("GW%d%s", time.Now().UnixNano(), r), nil
}

func mustID() string {
	id, err := crypto.RandomHex(16)
	if err != nil {
		return fmt.Sprintf("id-%d", time.Now().UnixNano())
	}
	return id
}
