// Package billing 负责定价计算与计费记账。
package billing

import (
	"context"
	"fmt"
	"time"

	"github.com/aitoys/llm-gateway/internal/canon"
	"github.com/aitoys/llm-gateway/internal/crypto"
	"github.com/aitoys/llm-gateway/internal/logging"
	"github.com/aitoys/llm-gateway/internal/metrics"
	"github.com/aitoys/llm-gateway/internal/model"
	"github.com/aitoys/llm-gateway/internal/store"
)

// Service 计费服务。
type Service struct {
	Store *store.Store
}

func New(s *store.Store) *Service { return &Service{Store: s} }

// QuotePrice 按模型售价(面向用户)分段计算应收(分):普通输入 + 缓存读 + 缓存写 + 输出。
// cacheReadPrice/cacheWritePrice 为 0 时,对应缓存段按 inputPrice(全价),向后兼容。
func QuotePrice(inputPrice, outputPrice, cacheReadPrice, cacheWritePrice int64, usage canon.Usage) int64 {
	return quoteSegment(inputPrice, outputPrice, cacheReadPrice, cacheWritePrice, usage)
}

// QuoteCost 按命中渠道成本单价分段计算实付(分)。语义同 QuotePrice。
func QuoteCost(inputCost, outputCost, cacheReadCost, cacheWriteCost int64, usage canon.Usage) int64 {
	return quoteSegment(inputCost, outputCost, cacheReadCost, cacheWriteCost, usage)
}

// quoteSegment 分段计价: prompt 拆为 normal / cacheRead / cacheWrite。
// 缓存单价为 0 时该段回退 input 单价(全价),保证未配置缓存价的模型行为不变。
func quoteSegment(input, output, cacheRead, cacheWrite int64, usage canon.Usage) int64 {
	normal := usage.PromptTokens - usage.CacheReadTokens - usage.CacheWriteTokens
	if normal < 0 {
		normal = 0
	}
	cr := cacheRead
	if cr == 0 {
		cr = input
	}
	cw := cacheWrite
	if cw == 0 {
		cw = input
	}
	return tokensMillionToCents(normal, input) +
		tokensMillionToCents(usage.CacheReadTokens, cr) +
		tokensMillionToCents(usage.CacheWriteTokens, cw) +
		tokensMillionToCents(usage.CompletionTokens, output)
}

// tokensMillionToCents: tokens * cents_per_million / 1e6。
func tokensMillionToCents(tokens int, centsPerM int64) int64 {
	// 先乘后除,保留精度(结果以分计,足够小)。
	return int64(tokens) * centsPerM / 1_000_000
}

// Charge 扣费并写账。售价来自模型(含缓存售价),成本来自实际命中的渠道(含模型级缓存成本)。
// 返回新余额与售价/成本。
//
// 失败保护: relay 在响应完成后才计费,若失败会漏账(上游已消费 token、平台已付成本)。
// 故此处对 ChargeAtomic 做有限次内联重试(覆盖瞬时抖动);仍失败则把应扣项落 pending_charges,
// 由后台 worker 幂等重试。ChargeAtomic 自身按 request_id 幂等,重试不会双扣。
func (s *Service) Charge(ctx context.Context, tenantID, userID, requestID, modelName string,
	usage canon.Usage,
	inputPrice, outputPrice, cacheReadPrice, cacheWritePrice,
	inputCost, outputCost, cacheReadCost, cacheWriteCost int64) (newBalance, priceCents, costCents int64, err error) {
	priceCents = QuotePrice(inputPrice, outputPrice, cacheReadPrice, cacheWritePrice, usage)
	costCents = QuoteCost(inputCost, outputCost, cacheReadCost, cacheWriteCost, usage)
	build := func() *model.BillingLedger {
		return &model.BillingLedger{
			ID: mustID(), TenantID: tenantID, UserID: userID, RequestID: requestID, Model: modelName,
			InputTokens: usage.PromptTokens, OutputTokens: usage.CompletionTokens,
			CostCents: costCents, PriceCents: priceCents, MarginCents: priceCents - costCents,
			Type: model.LedgerUsage, CreatedAt: time.Now(),
		}
	}
	// 内联重试: 指数退避,覆盖瞬时 PG 抖动(tx 冲突/连接掉线)。响应已发,延迟用户无感。
	// 共尝试 len(backoffs)+1 次(初次 + 每次退避后再试)。注意: select 内的 break 只能跳出
	// select 而非外层 for,故 ctx 取消的判定放在 select 之外,确保能真正中止重试转入入队。
	backoffs := []time.Duration{50 * time.Millisecond, 200 * time.Millisecond, 500 * time.Millisecond}
	for attempt := 0; attempt <= len(backoffs); attempt++ {
		newBalance, err = s.Store.ChargeAtomic(ctx, build())
		if err == nil {
			metrics.ObserveCharge("usage", priceCents)
			return newBalance, priceCents, costCents, nil
		}
		if attempt >= len(backoffs) {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(backoffs[attempt]):
		}
		if ctx.Err() != nil {
			break // ctx 取消,停止重试,转入入队兜底
		}
	}
	// 内联重试仍失败: best-effort 入队,交后台 worker 兜底(幂等,不会双扣)。
	s.enqueueFailedCharge(ctx, tenantID, userID, requestID, modelName, usage, priceCents, costCents, err)
	return 0, priceCents, costCents, fmt.Errorf("charge atomic (enqueued for retry): %w", err)
}

// enqueueFailedCharge 把一次失败的计费落库待重试。best-effort: PG 全域不可达时也会失败,
// 这种边界无外部持久 store 可兜,靠 abandoned 指标发现。
func (s *Service) enqueueFailedCharge(ctx context.Context, tenantID, userID, requestID, modelName string,
	usage canon.Usage, priceCents, costCents int64, cause error) {
	// 用独立短超时上下文,避免 relay 的 ctx 已被取消时连入队都做不了。
	ectx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = ctx
	causeStr := cause.Error()
	pc := &model.PendingCharge{
		ID: mustID(), TenantID: tenantID, UserID: userID, RequestID: requestID, Model: modelName,
		InputTokens: usage.PromptTokens, OutputTokens: usage.CompletionTokens,
		CacheReadTokens: usage.CacheReadTokens, CacheWriteTokens: usage.CacheWriteTokens,
		PriceCents: priceCents, CostCents: costCents,
		Attempts: 0, Status: model.PendingChargePending, LastError: &causeStr,
		NextRetryAt: time.Now(), CreatedAt: time.Now(),
	}
	if err := s.Store.EnqueuePendingCharge(ectx, pc); err != nil {
		metrics.ObserveChargeEnqueueFail()
		logging.From(ectx).Error("enqueue pending charge failed (money may leak; PG unreachable?)",
			"request_id", requestID, "price_cents", priceCents, "err", err)
	}
}

// RetryPendingCharges 扫描到期 pending 项并幂等结算。由后台 worker 周期调用。
// 每项: 成功→done; 失败→attempts++ 与退避下次重试; 超过上限→abandoned + 告警指标。
func (s *Service) RetryPendingCharges(ctx context.Context) {
	items, err := s.Store.ListPendingForRetry(ctx, time.Now(), 100)
	if err != nil {
		logging.From(ctx).Warn("list pending charges failed", "err", err)
		return
	}
	for _, p := range items {
		s.settlePending(ctx, p)
	}
}

const maxPendingAttempts = 20

func (s *Service) settlePending(ctx context.Context, p *model.PendingCharge) {
	// 复用幂等 ChargeAtomic: 若原路径其实已成功(账目已存在),这里会跳过扣减并视作 done。
	l := &model.BillingLedger{
		ID: mustID(), TenantID: p.TenantID, UserID: p.UserID, RequestID: p.RequestID, Model: p.Model,
		InputTokens: p.InputTokens, OutputTokens: p.OutputTokens,
		CostCents: p.CostCents, PriceCents: p.PriceCents, MarginCents: p.PriceCents - p.CostCents,
		Type: model.LedgerUsage, CreatedAt: time.Now(),
	}
	if _, err := s.Store.ChargeAtomic(ctx, l); err != nil {
		attempts := p.Attempts + 1
		if attempts >= maxPendingAttempts {
			_ = s.Store.MarkPendingAbandoned(ctx, p.ID, err.Error())
			metrics.ObserveChargeAbandoned()
			logging.From(ctx).Error("pending charge abandoned after max attempts (needs manual reconciliation)",
				"id", p.ID, "request_id", p.RequestID, "price_cents", p.PriceCents, "err", err)
			return
		}
		backoff := pendingBackoff(attempts)
		_ = s.Store.MarkPendingRetry(ctx, p.ID, err.Error(), attempts, time.Now().Add(backoff))
		return
	}
	_ = s.Store.MarkPendingDone(ctx, p.ID)
	metrics.ObserveCharge("usage", p.PriceCents)
}

// pendingBackoff 第 N 次失败后的下次重试退避:N²×10s,上限 10 分钟。
// 纯函数,便于单测。
func pendingBackoff(attempts int) time.Duration {
	d := time.Duration(attempts*attempts) * 10 * time.Second
	if d > 10*time.Minute {
		d = 10 * time.Minute
	}
	return d
}

// Recharge 充值: 加余额 + 写充值记录 + 写账目,单事务保证账实一致。
func (s *Service) Recharge(ctx context.Context, tenantID, userID string, amountCents int64) (int64, error) {
	if amountCents <= 0 {
		return 0, fmt.Errorf("amount must be positive")
	}
	rec := &model.Recharge{
		ID: mustID(), TenantID: tenantID, UserID: userID, AmountCents: amountCents, Status: "success", CreatedAt: time.Now(),
	}
	leg := &model.BillingLedger{
		ID: mustID(), TenantID: tenantID, UserID: userID, RequestID: "recharge", Model: "-",
		CostCents: 0, PriceCents: -amountCents, Type: model.LedgerRecharge, CreatedAt: time.Now(),
	}
	nb, err := s.Store.AdjustAtomic(ctx, leg, rec)
	if err == nil {
		metrics.ObserveCharge("recharge", amountCents)
	}
	return nb, err
}

// Adjust 管理员手动调整用户余额: deltaCents>0 加余额,<0 扣余额。
// 必须走账本(AdjustAtomic)以保证"账本=余额唯一真相"不变量——裸改 users.balance_cents
// 会让账实不一致、对账断裂、无法追溯。扣到负数时由 users.balance_cents>=0 CHECK 拒绝。
func (s *Service) Adjust(ctx context.Context, tenantID, userID string, deltaCents int64) (int64, error) {
	if deltaCents == 0 {
		// 0 调整无意义,直接返回当前余额,不写空账目。
		u, err := s.Store.GetUser(ctx, userID)
		if err != nil {
			return 0, err
		}
		return u.BalanceCents, nil
	}
	leg := &model.BillingLedger{
		ID: mustID(), TenantID: tenantID, UserID: userID, RequestID: "admin-adjust", Model: "-",
		// AdjustAtomic: balance += -PriceCents。故 delta>0(加钱)时 PriceCents 取负。
		PriceCents: -deltaCents, Type: model.LedgerAdjust, CreatedAt: time.Now(),
	}
	nb, err := s.Store.AdjustAtomic(ctx, leg, nil)
	if err == nil {
		if deltaCents > 0 {
			metrics.ObserveCharge("adjust_in", deltaCents)
		} else {
			metrics.ObserveCharge("adjust_out", -deltaCents)
		}
	}
	return nb, err
}

// mustID 生成随机 hex 主键(逻辑集中在 crypto.NewID,统一兜底)。
func mustID() string { return crypto.NewID() }
