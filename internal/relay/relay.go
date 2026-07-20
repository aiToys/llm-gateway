// Package relay 是网关核心: 路由、限流、调用 provider、计费编排。
package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/aitoys/llm-gateway/internal/auth"
	"github.com/aitoys/llm-gateway/internal/billing"
	"github.com/aitoys/llm-gateway/internal/canon"
	"github.com/aitoys/llm-gateway/internal/crypto"
	"github.com/aitoys/llm-gateway/internal/logging"
	"github.com/aitoys/llm-gateway/internal/metrics"
	"github.com/aitoys/llm-gateway/internal/model"
	"github.com/aitoys/llm-gateway/internal/provider"
	"github.com/aitoys/llm-gateway/internal/requestid"
	"github.com/aitoys/llm-gateway/internal/store"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

var (
	ErrModelNotFound   = errors.New("model not found or disabled")
	ErrNoChannel       = errors.New("no available channel for model")
	ErrInsufficientBal = errors.New("insufficient balance")
	ErrQuotaExceeded   = errors.New("quota exceeded")
)

// Service relay 服务。
type Service struct {
	Store                     *store.Store
	Providers                 *provider.Registry
	Billing                   *billing.Service
	Cipher                    *crypto.Cipher
	DefaultProvider           string
	Breaker                   CircuitBreaker
	RDB                       *redis.Client // 可选,round_robin 跨副本游标用;为 nil 时回退 weighted。
	MinBalanceCents           int64         // 余额低于此值拒绝请求(透支防护的第一道闸)
	CharsPerToken             int           // 输入成本估算用: 1 token ≈ N 字符;preflight 据此预估 prompt 成本
	PassthroughUpstreamErrors bool          // 上游 4xx/5xx 原样透传给客户端(默认 true;对外多租户可关防泄露上游内部信息)
	ReqLog                    ReqLogCfg     // 请求/响应原文日志配置
}

// ReqLogCfg 请求日志配置(原生类型,避免 relay→config 循环依赖;由 bootstrap 从 config 映射)。
type ReqLogCfg struct {
	Enabled      bool
	SampleRate   float64
	MaxBodyBytes int
	LogBodies    bool
}

// Meta 一次调用的元信息。
type Meta struct {
	RequestID  string
	Provider   string
	ChannelID  string
	Usage      canon.Usage
	PriceCents int64
	CostCents  int64
}

// resolvedChannel 选定渠道的运行时形态(已解密 + 上游模型名解析)。
type resolvedChannel struct {
	ch             *provider.Channel
	upstream       string // 实际发给上游的模型名
	provider       string // 渠道对应的 provider 名(可能回退到默认)
	inputCost      int64  // 命中渠道(供应商)的输入成本单价,用于真实毛利核算
	outputCost     int64  // 命中渠道(供应商)的输出成本单价
	cacheReadCost  int64  // 缓存命中(读取)成本单价(来自 ModelCosts;0=按 inputCost 核算)
	cacheWriteCost int64  // 缓存写入成本单价
}

// route 解析定价 + 选择渠道序列(含负载均衡顺序)。
func (s *Service) route(ctx context.Context, sub auth.Subject, modelName string) (*model.ModelDef, []resolvedChannel, error) {
	p, err := s.Store.EffectivePrice(ctx, sub.TenantID, modelName)
	if err != nil {
		// 区分"模型不存在/已禁用"(404)与底层 DB 错误(500):后者原先一律映射为
		// ErrModelNotFound,PG 抖动时用户看到"模型不存在",排障困难。
		if errors.Is(err, store.ErrNotFound) {
			return nil, nil, ErrModelNotFound
		}
		return nil, nil, err
	}
	if !p.Enabled {
		return nil, nil, ErrModelNotFound
	}

	chs, err := s.Store.ChannelsForModel(ctx, sub.TenantID, modelName)
	if err != nil {
		return nil, nil, err
	}
	// 过滤熔断中(打开)的渠道(新建切片,避免复用调用方底层数组造成污染)。
	// 用只读 IsOpen 而非 Allow:Allow 在 Redis 实现里有副作用(半开 SetNX 抢占探测名额),
	// 遍历 N 个候选会占满所有探测名额,多副本下冷却结束后恢复极慢。探测名额只在真正
	// 决定调用某渠道前(下方 Chat/Embeddings/ChatStream)由调用链消费。
	if s.Breaker != nil {
		filtered := make([]*model.Channel, 0, len(chs))
		for _, c := range chs {
			if !s.Breaker.IsOpen(ctx, c.ID) {
				filtered = append(filtered, c)
			}
		}
		chs = filtered
	}
	if len(chs) == 0 {
		if s.DefaultProvider == "" {
			return nil, nil, ErrNoChannel
		}
		// fallback 渠道 ID 带上模型名,使熔断器按模型隔离——否则一个模型把 fallback 打挂会牵连所有走默认 provider 的模型。
		return p, []resolvedChannel{{
			ch:       &provider.Channel{ID: "fallback:" + modelName, TenantID: sub.TenantID, Provider: s.DefaultProvider},
			upstream: modelName, provider: s.DefaultProvider,
		}}, nil
	}

	ordered := s.selectOrdered(ctx, chs, model.NormalizedRoutingStrategy(p.RoutingStrategy), sub.TenantID, modelName, p.PinnedChannelID)
	out := make([]resolvedChannel, 0, len(ordered))
	for _, c := range ordered {
		pv := c.Provider
		if s.Providers.Get(pv) == nil {
			// 渠道声明的 provider 未注册(真实供应商未配置),回退默认 provider。
			pv = s.DefaultProvider
			if pv == "" {
				continue
			}
		}
		keyPlain, derr := s.Cipher.Decrypt(c.APIKeyEnc)
		if derr != nil {
			// 密文损坏或密钥轮换未同步: 跳过该渠道,避免把空 key 送给上游触发无意义 401。
			logging.L().Warn("channel key decrypt failed, skipping channel",
				"channel_id", c.ID, "model", modelName, "err", derr.Error())
			continue
		}
		tenantID := ""
		if c.TenantID != nil {
			tenantID = *c.TenantID
		}
		// ChannelsForModel 的 JOIN 结果: ChannelModels[0] 即该 (渠道,模型) 的配置。
		if len(c.ChannelModels) == 0 {
			continue // 防御: JOIN 应保证有,缺则跳过
		}
		cm := c.ChannelModels[0]
		upstream := cm.UpstreamModel
		if upstream == "" {
			upstream = modelName // 空=同名直通
		}
		// 模型级成本(0 表示回退渠道级默认成本);缓存成本仅模型级有(渠道级无缓存默认)。
		inCost := costOr(cm.InputCostCentsPerM, c.InputCostCentsPerM)
		outCost := costOr(cm.OutputCostCentsPerM, c.OutputCostCentsPerM)
		out = append(out, resolvedChannel{
			ch: &provider.Channel{
				ID: c.ID, TenantID: tenantID, Provider: pv, BaseURL: c.BaseURL, APIKey: keyPlain,
			},
			upstream: upstream, provider: pv,
			inputCost: inCost, outputCost: outCost,
			cacheReadCost: cm.CacheReadCostCentsPerM, cacheWriteCost: cm.CacheWriteCostCentsPerM,
		})
	}
	if len(out) == 0 {
		return nil, nil, ErrNoChannel
	}
	return p, out, nil
}

// splitPriority 把渠道分为最高优先级组与其余。
//
//	优先级层次:租户 BYOK 渠道(tenant_id != nil)整层优先于平台默认渠道——租户自带 Key 必先被使用。
//	top 层 = 若存在租户渠道则取租户层,否则平台层;层内取最大 priority 作为 top 组,同层同 priority 归为一组。
//
// 必须用"(租户层, priority)"复合键而非纯 priority:否则高 priority 的平台渠道会压住低 priority 的
// 租户 BYOK 渠道,导致租户自带 Key 永不被使用(违反 BYOK 优先语义),反而消耗平台 Key。
// 不依赖调用方传入顺序(自行计算 top 层与 max priority),与 ChannelsForModel 的 ORDER BY 互为兜底。
func splitPriority(chs []*model.Channel) (group, rest []*model.Channel) {
	if len(chs) == 0 {
		return nil, nil
	}
	hasTenant := false
	for _, c := range chs {
		if c.TenantID != nil {
			hasTenant = true
			break
		}
	}
	const minInt = -1 << 62
	topPrio := minInt
	for _, c := range chs {
		if (c.TenantID != nil) == hasTenant && c.Priority > topPrio {
			topPrio = c.Priority
		}
	}
	for _, c := range chs {
		if (c.TenantID != nil) == hasTenant && c.Priority == topPrio {
			group = append(group, c)
		} else {
			rest = append(rest, c)
		}
	}
	return group, rest
}

// channelWeight 取渠道在该模型下的有效权重:模型级(ChannelModels[0].Weight)优先,
// 0 回退渠道级 weight;渠道级 0 视为 1。使 channel_models 上配置的模型级流量倾斜真正生效。
func channelWeight(c *model.Channel) int {
	if len(c.ChannelModels) > 0 && c.ChannelModels[0].Weight > 0 {
		return c.ChannelModels[0].Weight
	}
	w := c.Weight
	if w <= 0 {
		w = 1
	}
	return w
}

// weightedPick 组内按 weight 加权随机选主(权重取模型级,回退渠道级)。
func weightedPick(group []*model.Channel) *model.Channel {
	total := 0
	for _, c := range group {
		total += channelWeight(c)
	}
	pick := group[0]
	if total > 0 {
		r := rand.Intn(total) //nolint:gosec // 加权负载均衡选渠道,非安全场景,math/rand 性能合适
		for _, c := range group {
			r -= channelWeight(c)
			if r < 0 {
				pick = c
				break
			}
		}
	}
	return pick
}

// arrange 把选中的主放在首位,组内其余跟后,最后接低优先级 rest,作为故障转移序列。
func arrange(pick *model.Channel, group, rest []*model.Channel) []*model.Channel {
	out := []*model.Channel{pick}
	seen := map[string]bool{pick.ID: true}
	for _, c := range group {
		if !seen[c.ID] {
			out = append(out, c)
			seen[c.ID] = true
		}
	}
	out = append(out, rest...)
	return out
}

// selectOrdered 按策略选择渠道主备序列(round_robin 的跨副本游标由 Service 方法处理)。
//
//	weighted  组内加权随机(默认)
//	random    组内纯随机(忽略权重)
//	failover  组内固定第一个(主备,不随机)
func selectOrdered(chs []*model.Channel, strategy string) []*model.Channel {
	if len(chs) == 0 {
		return nil
	}
	group, rest := splitPriority(chs)
	if len(group) == 0 {
		return rest
	}
	var pick *model.Channel
	switch strategy {
	case model.StrategyRandom:
		pick = group[rand.Intn(len(group))] //nolint:gosec // 随机路由负载均衡,非安全随机
	case model.StrategyFailover:
		pick = group[0] // 固定首位,纯主备
	default: // weighted
		pick = weightedPick(group)
	}
	return arrange(pick, group, rest)
}

// selectOrdered (Service) 在包级 selectOrdered 基础上处理 round_robin 与 pinned。
//
//	round_robin  用 Redis 跨副本游标轮询(rdb 为 nil / Redis 故障时回退 weighted)
//	            游标键按 tenantID+model 维度,避免不同租户的同名模型互相串扰轮询起点。
//	pinned       固定到指定渠道,其余按优先级顺序作故障转移候选(pinned 不存在时回退 failover)
func (s *Service) selectOrdered(ctx context.Context, chs []*model.Channel, strategy, tenantID, modelName, pinnedID string) []*model.Channel {
	// pinned: 把指定渠道置于首位,其余保持 priority 降序作为故障转移候选。
	if strategy == model.StrategyPinned {
		if pinnedID == "" {
			return selectOrdered(chs, model.StrategyFailover) // 未指定则回退主备
		}
		var head, tail []*model.Channel
		for _, c := range chs {
			if c.ID == pinnedID && len(head) == 0 {
				head = []*model.Channel{c}
			} else {
				tail = append(tail, c)
			}
		}
		if len(head) == 0 {
			return selectOrdered(chs, model.StrategyFailover) // 渠道不存在则回退
		}
		return append(head, tail...)
	}
	if strategy != model.StrategyRoundRobin || s.RDB == nil {
		if strategy == model.StrategyRoundRobin {
			strategy = model.StrategyWeighted // 无 rdb 回退
		}
		return selectOrdered(chs, strategy)
	}
	group, rest := splitPriority(chs)
	if len(group) == 0 {
		return rest
	}
	// INCR 取全局递增游标,跨副本一致;游标键带 tenantID 维度,避免跨租户串扰。失败则回退加权随机。
	n, err := s.RDB.Incr(ctx, "rr:"+tenantID+":"+modelName).Result()
	if err != nil {
		return arrange(weightedPick(group), group, rest)
	}
	start := int((n - 1) % int64(len(group)))
	// 以 start 为起点轮转,其余按轮转顺序作为故障转移候选。
	out := make([]*model.Channel, 0, len(group)+len(rest))
	for i := 0; i < len(group); i++ {
		out = append(out, group[(start+i)%len(group)])
	}
	out = append(out, rest...)
	return out
}

// chargePostCompletion 在响应/向量已生成后用独立超时 ctx 扣费,避免请求 ctx 因客户端早断取消
// 导致漏扣(上游已消费 token,本地必须扣款)。统一 Chat/Embeddings/ChatStream 三路径的扣费编排:
// 5s 超时 ctx + 失败记日志供对账补扣;返回 price/cost 由调用方写入 meta。
// Charge 内部失败会落 pending_charges 由 billing retry worker 兜底。
// cacheReadPrice/cacheWritePrice 由调用方传入: chat/stream 传模型缓存价,embeddings 传 0(向量无缓存计费)。
func (s *Service) chargePostCompletion(sub auth.Subject, requestID, model string, usage canon.Usage,
	p *model.ModelDef, cacheReadPrice, cacheWritePrice int64, rc resolvedChannel, logTag string) (price, cost int64) {
	bctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, price, cost, cerr := s.Billing.Charge(bctx, sub.TenantID, sub.UserID, requestID, model, usage,
		p.InputPriceCentsPerM, p.OutputPriceCentsPerM, cacheReadPrice, cacheWritePrice,
		rc.inputCost, rc.outputCost, rc.cacheReadCost, rc.cacheWriteCost)
	if cerr != nil {
		logging.L().Error(logTag+" charge failed (post-completion, may need reconciliation)",
			"request_id", requestID, "user_id", sub.UserID, "model", model, "err", cerr.Error())
	}
	return price, cost
}

// Chat 非流式(含多渠道故障转移)。
func (s *Service) Chat(ctx context.Context, sub auth.Subject, req *canon.Request) (*canon.Response, *Meta, error) {
	p, channels, err := s.route(ctx, sub, req.Model)
	if err != nil {
		return nil, nil, err
	}
	start := time.Now()
	if err := s.preflight(ctx, sub, p, req, start); err != nil {
		return nil, nil, err
	}
	reqID := resolveReqID(ctx)
	metrics.Inflight.Inc()
	defer metrics.Inflight.Dec()
	reqJSON, _ := json.Marshal(req) // 请求原文(供请求日志;marshal 失败则记空)
	var lastErr error
	var lastTried resolvedChannel
	if len(channels) > 0 {
		lastTried = channels[0]
	}
	for _, rc := range channels {
		lastTried = rc
		pv := s.Providers.Get(rc.provider)
		if pv == nil {
			lastErr = fmt.Errorf("provider %s not registered", rc.provider)
			continue
		}
		tryReq := *req
		tryReq.Model = rc.upstream
		resp, err := pv.Chat(ctx, rc.ch, &tryReq)
		// 瞬时错误(超时/连接重置/EOF)在同一渠道内退避重试一次,再决定是否故障转移。
		if err != nil && isTransient(err) {
			select {
			case <-time.After(200 * time.Millisecond):
			case <-ctx.Done():
			}
			resp, err = pv.Chat(ctx, rc.ch, &tryReq)
		}
		if err != nil {
			if s.Breaker != nil {
				s.Breaker.OnFailure(ctx, rc.ch.ID)
			}
			lastErr = err
			continue // 故障转移
		}
		if s.Breaker != nil {
			s.Breaker.OnSuccess(ctx, rc.ch.ID)
		}
		meta := &Meta{RequestID: reqID, Provider: rc.provider, ChannelID: rc.ch.ID, Usage: resp.Usage}
		meta.PriceCents, meta.CostCents = s.chargePostCompletion(sub, meta.RequestID, req.Model, resp.Usage, p,
			p.CacheReadPriceCentsPerM, p.CacheWritePriceCentsPerM, rc, "chat")
		respJSON, _ := json.Marshal(resp)
		// recordUsage 用独立脱离请求的 ctx 落库(防客户端早断取消);不复用 charge 的内部 bctx(已 cancel)。
		dbctx, dbcancel := context.WithTimeout(context.Background(), 5*time.Second)
		s.recordUsage(dbctx, sub, reqID, req.Model, rc, start, resp.Usage, meta.PriceCents, meta.CostCents, "ok", "", reqJSON, respJSON)
		dbcancel()
		resp.ID = meta.RequestID
		return resp, meta, nil
	}
	// 全部渠道失败: 错误用量记到最后一次实际尝试的渠道,而非序列首位,避免污染可用性统计。
	s.recordUsage(ctx, sub, reqID, req.Model, lastTried, start, canon.Usage{}, 0, 0, "error", errCompact(lastErr), reqJSON, nil)
	if lastErr == nil {
		lastErr = ErrNoChannel
	}
	return nil, nil, lastErr
}

// Embeddings 文本向量(/v1/embeddings)。路由 + 预检 + 渠道故障转移,按输入 token 计费。
// 向量请求无输出 token,按 input 价计费(usage.PromptTokens)。
func (s *Service) Embeddings(ctx context.Context, sub auth.Subject, modelName string, input []string) ([][]float32, *Meta, error) {
	p, channels, err := s.route(ctx, sub, modelName)
	if err != nil {
		return nil, nil, err
	}
	start := time.Now()
	// 构造估算用 request:把全部 input 文本拼成一条消息,供 preflight 估出真实输入 token,
	// 否则空 Messages → estimatePromptTokens 返回 1,余额/配额预检被绕过(可传海量 input 白嫖)。
	var estSB strings.Builder
	for _, t := range input {
		estSB.WriteString(t)
	}
	estReq := &canon.Request{Model: modelName, Messages: []canon.Message{{Role: "user", Content: estSB.String()}}}
	if err := s.preflight(ctx, sub, p, estReq, start); err != nil {
		return nil, nil, err
	}
	reqID := resolveReqID(ctx)
	metrics.Inflight.Inc()
	defer metrics.Inflight.Dec()
	var lastErr error
	var lastTried resolvedChannel
	if len(channels) > 0 {
		lastTried = channels[0]
	}
	for _, rc := range channels {
		lastTried = rc
		pv := s.Providers.Get(rc.provider)
		if pv == nil {
			lastErr = fmt.Errorf("provider %s not registered", rc.provider)
			continue
		}
		vecs, usage, err := pv.Embeddings(ctx, rc.ch, input, rc.upstream)
		if err != nil {
			if s.Breaker != nil {
				s.Breaker.OnFailure(ctx, rc.ch.ID)
			}
			lastErr = err
			continue
		}
		if s.Breaker != nil {
			s.Breaker.OnSuccess(ctx, rc.ch.ID)
		}
		meta := &Meta{RequestID: reqID, Provider: rc.provider, ChannelID: rc.ch.ID, Usage: *usage}
		meta.PriceCents, meta.CostCents = s.chargePostCompletion(sub, meta.RequestID, modelName, *usage, p, 0, 0, rc, "embeddings")
		dbctx, dbcancel := context.WithTimeout(context.Background(), 5*time.Second)
		s.recordUsage(dbctx, sub, reqID, modelName, rc, start, *usage, meta.PriceCents, meta.CostCents, "ok", "", nil, nil)
		dbcancel()
		return vecs, meta, nil
	}
	s.recordUsage(ctx, sub, reqID, modelName, lastTried, start, canon.Usage{}, 0, 0, "error", errCompact(lastErr), nil, nil)
	if lastErr == nil {
		lastErr = ErrNoChannel
	}
	return nil, nil, lastErr
}

// ChatStream 流式(故障转移仅在连接建立前)。返回 chunk 通道。
func (s *Service) ChatStream(ctx context.Context, sub auth.Subject, req *canon.Request) (<-chan *canon.StreamChunk, *Meta, error) {
	p, channels, err := s.route(ctx, sub, req.Model)
	if err != nil {
		return nil, nil, err
	}
	start := time.Now()
	if err := s.preflight(ctx, sub, p, req, start); err != nil {
		return nil, nil, err
	}
	reqID := resolveReqID(ctx)
	// 流式强制要求上游返回 usage 帧(OpenAI 兼容上游默认不带,会导致 0 token 计费)。
	// 客户端未显式开启时由网关补齐;客户端已设则尊重其选择。
	if req.StreamOptions == nil {
		req.StreamOptions = &canon.StreamOptions{IncludeUsage: true}
	}
	reqJSON, _ := json.Marshal(req) // 请求原文(供请求日志)
	metrics.Inflight.Inc()
	// 尝试建立流,失败则转移到下一渠道。
	var src <-chan *canon.StreamChunk
	var picked resolvedChannel
	var lastErr error
	var lastTried resolvedChannel
	if len(channels) > 0 {
		lastTried = channels[0]
	}
	for _, rc := range channels {
		lastTried = rc
		pv := s.Providers.Get(rc.provider)
		if pv == nil {
			lastErr = fmt.Errorf("provider %s not registered", rc.provider)
			continue
		}
		tryReq := *req
		tryReq.Model = rc.upstream
		ch, err := pv.ChatStream(ctx, rc.ch, &tryReq)
		if err != nil {
			if s.Breaker != nil {
				s.Breaker.OnFailure(ctx, rc.ch.ID)
			}
			lastErr = err
			continue
		}
		if s.Breaker != nil {
			s.Breaker.OnSuccess(ctx, rc.ch.ID)
		}
		src = ch
		picked = rc
		lastErr = nil
		break
	}
	if src == nil {
		s.recordUsage(ctx, sub, reqID, req.Model, lastTried, start, canon.Usage{}, 0, 0, "error", errCompact(lastErr), reqJSON, nil)
		metrics.Inflight.Dec()
		if lastErr == nil {
			lastErr = ErrNoChannel
		}
		return nil, nil, lastErr
	}

	out := make(chan *canon.StreamChunk, 16)
	var respText strings.Builder // 累积流式 delta 文本,finalize 时 marshal 为响应体落请求日志
	meta := &Meta{RequestID: reqID, Provider: picked.provider, ChannelID: picked.ch.ID}
	// finalStatus 由各结束分支设置,finalize 落 usage_records 时使用。默认 ok;
	// 上游中途出错记 partial(否则失败的流被统计为成功,污染 SLA 与计费对账)。
	finalStatus := "ok"
	finalErrMsg := ""

	// finalize 无论流如何结束(正常完成 / 客户端断连 / 上游出错 / goroutine panic),都基于已观测到的
	// usage 完成扣费与记账。用独立带超时的 context,避免请求 ctx 取消后计费丢失
	// (流式已生成 token,上游已计费,本地必须扣款,否则被薅羊毛)。
	finalize := func(pending canon.Usage) {
		defer func() { _ = recover() }() // 计费 goroutine 不应拖垮进程
		defer metrics.Inflight.Dec()
		meta.Usage = pending
		meta.PriceCents, meta.CostCents = s.chargePostCompletion(sub, meta.RequestID, req.Model, pending, p,
			p.CacheReadPriceCentsPerM, p.CacheWritePriceCentsPerM, picked, "stream")
		// 流式无完整响应对象,用累积的 delta 文本 + usage 构造响应体快照供请求日志。
		var respJSON []byte
		if s.ReqLog.Enabled && s.ReqLog.LogBodies {
			respJSON, _ = json.Marshal(map[string]any{"content": respText.String(), "usage": pending})
		}
		// recordUsage/recordTPM 用独立脱离请求的 ctx 落库(防客户端早断取消)。
		dbctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.recordUsage(dbctx, sub, reqID, req.Model, picked, start, pending, meta.PriceCents, meta.CostCents, finalStatus, finalErrMsg, reqJSON, respJSON)
		// 流式: 中间件在 g.Next() 时还拿不到 token,这里补登 TPM 桶,使流式也纳入限流统计。
		s.recordTPM(dbctx, sub, pending)
	}

	go func() { //nolint:gosec // G118: finalize 必须用 context.Background 脱离请求 ctx,否则客户端早断会取消计费落库(已生成 token 须扣款)。
		defer close(out)
		// panic 兜底:chunk 处理逻辑(类型断言/反序列化/字符串拼接)若 panic,外层 recover 只吞错,
		// finalize 永不触发 → 计费与 Inflight 双漏。defer finish() 保证任何退出路径(含 panic)都扣费。
		var finishOnce sync.Once
		var lastUsage *canon.Usage
		finish := func() {
			finishOnce.Do(func() { finalize(resolveStreamUsage(lastUsage)) })
		}
		defer finish()
		defer func() { _ = recover() }() // 流式转发 goroutine 不应拖垮进程
		for {
			select {
			case chunk, ok := <-src:
				if !ok {
					return // 上游流结束 → defer finish() 兜底
				}
				if chunk.StreamError != "" {
					// 上游流中途出错(网络断流/读超时/行超长):触发熔断,避免 200 OK 后截断的
					// 病态渠道持续漏流量;仍按已观测到的 usage 计费(已生成 token 不可回收)。
					if s.Breaker != nil {
						dbctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
						s.Breaker.OnFailure(dbctx, picked.ch.ID) // 用独立 ctx,客户端已断时请求 ctx 已 cancel
						cancel()
					}
					logging.L().Warn("upstream stream error mid-flight",
						"request_id", reqID, "channel_id", picked.ch.ID, "err", chunk.StreamError)
					finalStatus = "partial"
					finalErrMsg = chunk.StreamError
					return
				}
				if chunk.Usage != nil {
					lastUsage = chunk.Usage // 以最后一个 usage 帧为准(见 resolveStreamUsage)
				}
				// 累积首选项的 delta 文本,供请求日志记录响应体(仅文本;tool_call 增量不计)。
				if len(chunk.Choices) > 0 {
					respText.WriteString(canon.TextContent(chunk.Choices[0].Delta))
				}
				chunk.ID = meta.RequestID
				select {
				case out <- chunk:
				case <-ctx.Done(): // 客户端断连: 停止转发,但仍按已生成 token 计费。
					return
				}
			case <-ctx.Done(): // 客户端断连
				return
			}
		}
	}()
	return out, meta, nil
}

// resolveStreamUsage 把流中观测到的最后一个 usage 帧解析为最终用量。
// 采用"覆盖而非累加": 多数供应商仅在末帧带 usage;少数供应商发累计 usage 帧。
// 两种情况下"取最后一帧"都是正确的,而累加会在累计帧场景下重复计费。
// total 缺失时按 prompt+completion 兜底。
func resolveStreamUsage(last *canon.Usage) canon.Usage {
	var u canon.Usage
	if last != nil {
		u = *last
	}
	if u.TotalTokens == 0 {
		u.TotalTokens = u.PromptTokens + u.CompletionTokens
	}
	return u
}

// preflight 余额预检: ① 余额低于 MinBalanceCents 拒绝;② 估算本次输入(prompt)成本,
// 余额不足以覆盖"最小成本 + MinBalanceCents"时直接拒绝,挡住大 prompt 透支。
// 属 TOCTOU 性质预检(并发下仍可能小幅超出),真正的下界由 ChargeAtomic 的 min(余额,应收) + DB CHECK 兜底。
func (s *Service) preflight(ctx context.Context, sub auth.Subject, p *model.ModelDef, req *canon.Request, start time.Time) error {
	u, err := s.Store.GetUser(ctx, sub.UserID)
	if err != nil {
		return err
	}
	if u.BalanceCents < s.MinBalanceCents {
		return ErrInsufficientBal
	}
	if p != nil && req != nil {
		estInput := estimatePromptTokens(req, s.CharsPerToken)
		estCost := int64(estInput) * p.InputPriceCentsPerM / 1_000_000
		if u.BalanceCents < s.MinBalanceCents+estCost {
			return ErrInsufficientBal
		}
		// 日/月 token 配额预检:用估算输入 token + 桶累计预判,超限直接拒。
		// TOCTOU 性质(并发下可能小幅超出),最终一致由 recordUsage 补登真实 token 兜底。
		if err := s.checkTokenQuota(ctx, sub, estInput, start); err != nil {
			return err
		}
	}
	return nil
}

// checkTokenQuota 日/月 token 配额预检(仅 API Key 鉴权且配额>0 时生效)。
// 取当前桶累计 + 估算输入,超过限额返回 ErrQuotaExceeded。Redis 不可用时跳过(降级为只限余额)。
func (s *Service) checkTokenQuota(ctx context.Context, sub auth.Subject, estInput int, now time.Time) error {
	if s.RDB == nil || sub.APIKeyID == "" {
		return nil
	}
	est := int64(estInput)
	if sub.DailyTokenLimit > 0 {
		k := "quota:tok:d:" + sub.APIKeyID + ":" + now.Format("20060102")
		if cur, _ := s.RDB.Get(ctx, k).Int64(); cur+est > int64(sub.DailyTokenLimit) {
			return ErrQuotaExceeded
		}
	}
	if sub.MonthlyTokenLimit > 0 {
		k := "quota:tok:m:" + sub.APIKeyID + ":" + now.Format("200601")
		if cur, _ := s.RDB.Get(ctx, k).Int64(); cur+est > int64(sub.MonthlyTokenLimit) {
			return ErrQuotaExceeded
		}
	}
	return nil
}

// recordQuotaTokens 把本次真实 token 用量补登到日/月 token 桶(供后续 preflight 预判)。
// 在 recordUsage 路径旁挂;失败仅忽略(配额容忍最终一致)。
func (s *Service) recordQuotaTokens(ctx context.Context, sub auth.Subject, usage canon.Usage, now time.Time) {
	if s.RDB == nil || sub.APIKeyID == "" {
		return
	}
	tokens := int64(usage.PromptTokens + usage.CompletionTokens)
	if tokens <= 0 {
		return
	}
	if sub.DailyTokenLimit > 0 {
		k := "quota:tok:d:" + sub.APIKeyID + ":" + now.Format("20060102")
		if _, err := s.RDB.IncrBy(ctx, k, tokens).Result(); err == nil {
			_ = s.RDB.Expire(ctx, k, 25*time.Hour).Err()
		}
	}
	if sub.MonthlyTokenLimit > 0 {
		k := "quota:tok:m:" + sub.APIKeyID + ":" + now.Format("200601")
		if _, err := s.RDB.IncrBy(ctx, k, tokens).Result(); err == nil {
			_ = s.RDB.Expire(ctx, k, 32*24*time.Hour).Err()
		}
	}
}

// estimatePromptTokens 粗估 prompt token 数(按字符/CharsPerToken,缺省按 2 字符/token)。
func estimatePromptTokens(req *canon.Request, charsPerToken int) int {
	if charsPerToken <= 0 {
		charsPerToken = 2
	}
	n := 0
	for _, m := range req.Messages {
		n += len(canon.TextContent(m))
	}
	if n == 0 {
		return 1
	}
	return n/charsPerToken + 1
}

func (s *Service) recordUsage(ctx context.Context, sub auth.Subject, requestID, logicalModel string, rc resolvedChannel, start time.Time, usage canon.Usage, price, cost int64, status, errMsg string, reqJSON, respJSON []byte) {
	if requestID == "" {
		requestID = resolveReqID(ctx)
	}
	// 错误信息截断脱敏,避免把上游长堆栈写库。
	if len(errMsg) > 200 {
		errMsg = errMsg[:200]
	}
	s.recordRequestLog(ctx, sub, requestID, logicalModel, rc, start, usage, price, status, errMsg, reqJSON, respJSON)
	_ = s.Store.InsertUsage(ctx, &model.UsageRecord{
		ID: mustID(), TenantID: sub.TenantID, UserID: sub.UserID,
		APIKeyID: sub.APIKeyID, APIKeyName: sub.APIKeyName,
		RequestID: requestID, Model: logicalModel, Provider: rc.provider, ChannelID: rc.ch.ID,
		InputTokens: usage.PromptTokens, OutputTokens: usage.CompletionTokens,
		PriceCents: price, CostCents: cost,
		LatencyMs: int(time.Since(start).Milliseconds()), Status: status, ErrorMessage: errMsg, CreatedAt: time.Now(),
	})
	// 同步 Prometheus 指标(延迟/计数/token)。
	metrics.ObserveRequest(sub.TenantID, logicalModel, rc.provider, status, start, usage.PromptTokens, usage.CompletionTokens)
	// 日/月 token 配额桶补登真实用量(供 preflight 预判;流式/非流式统一收口于此)。
	s.recordQuotaTokens(ctx, sub, usage, start)
	// 每次调用产出一条结构化完成日志(等价于 LLM 访问日志),便于 ELK/grep 按请求排障。
	logging.L().Info("llm.request",
		"req_id", requestID,
		"tenant_id", sub.TenantID, "user_id", sub.UserID,
		"api_key_id", sub.APIKeyID,
		"model", logicalModel, "provider", rc.provider, "channel_id", rc.ch.ID,
		"input_tokens", usage.PromptTokens, "output_tokens", usage.CompletionTokens,
		"price_cents", price, "cost_cents", cost,
		"latency_ms", time.Since(start).Milliseconds(),
		"status", status, "err", errMsg)
}

// recordRequestLog 按采样策略落库一条请求/响应原文日志(生产排障/合规审计)。
// 默认关闭(隐私与存储考量);开启后按 SampleRate 采样,LogBodies=false 时只记元信息。
// 单条异步 INSERT,失败仅记日志(请求日志非资金,容忍偶丢)。
func (s *Service) recordRequestLog(ctx context.Context, sub auth.Subject, requestID, logicalModel string, rc resolvedChannel, start time.Time, usage canon.Usage, price int64, status, errMsg string, reqJSON, respJSON []byte) {
	if !s.ReqLog.Enabled {
		return
	}
	// SampleRate>=1 全量;否则按随机采样。失败请求一律落库(排障价值高于成功请求)。
	if status == "ok" && s.ReqLog.SampleRate < 1.0 && rand.Float64() >= s.ReqLog.SampleRate { //nolint:gosec // 采样非安全随机
		return
	}
	var reqBody, respBody *string
	if s.ReqLog.LogBodies {
		reqBody = truncPtr(reqJSON, s.ReqLog.MaxBodyBytes)
		respBody = truncPtr(respJSON, s.ReqLog.MaxBodyBytes)
	}
	var errPtr *string
	if errMsg != "" {
		e := errMsg
		errPtr = &e
	}
	httpStatus := 200
	if status != "ok" {
		httpStatus = 502
	}
	if err := s.Store.InsertRequestLog(ctx, &model.RequestLog{
		ID: mustID(), RequestID: requestID, TenantID: sub.TenantID, UserID: sub.UserID, APIKeyID: sub.APIKeyID,
		Model: logicalModel, Provider: rc.provider, ChannelID: rc.ch.ID,
		Status: httpStatus, LatencyMs: int(time.Since(start).Milliseconds()),
		InputTokens: usage.PromptTokens, OutputTokens: usage.CompletionTokens,
		PriceCents: price, RequestBody: reqBody, ResponseBody: respBody, Error: errPtr, CreatedAt: time.Now(),
	}); err != nil {
		logging.L().Warn("insert request log failed", "request_id", requestID, "err", err.Error())
	}
}

// truncPtr 把 body 字节截断到 max 后转为可空字符串(空 body 返回 nil,不占库空间)。
func truncPtr(b []byte, max int) *string {
	if len(b) == 0 {
		return nil
	}
	s := string(b)
	if max > 0 && len(s) > max {
		s = s[:max]
	}
	return &s
}

// recordTPM 把本次用量计入 API Key 的 TPM 分钟桶(流式补登;非流式由中间件直接计数)。
func (s *Service) recordTPM(ctx context.Context, sub auth.Subject, usage canon.Usage) {
	if s.RDB == nil || sub.APIKeyID == "" || sub.TPMLimit <= 0 {
		return
	}
	tokens := int64(usage.PromptTokens + usage.CompletionTokens)
	if tokens <= 0 {
		return
	}
	key := "rl:tpm:" + sub.APIKeyID + ":" + time.Now().Format("200601021504")
	if _, err := s.RDB.IncrBy(ctx, key, tokens).Result(); err == nil {
		_ = s.RDB.Expire(ctx, key, 65*time.Second).Err()
	}
}

func newReqID() string { return "req-" + uuid.NewString() }

// resolveReqID 优先用 requestid 中间件注入的 id(使请求日志/usage/billing/日志共享同一链路 ID),
// 否则回退到自生成。中间件未挂载(如单元测试)时也能工作。
func resolveReqID(ctx context.Context) string {
	if id := requestid.FromContext(ctx); id != "" {
		return id
	}
	return newReqID()
}

// errCompact 把 error 压缩为适合写库/打日志的短字符串。
func errCompact(err error) string {
	if err == nil {
		return ""
	}
	m := strings.TrimSpace(err.Error())
	if len(m) > 200 {
		m = m[:200]
	}
	return m
}

// ChannelOpen 查询某渠道当前熔断状态(供管理端健康展示;不触发上游探测)。
// 返回 true=放行中(关闭/半开),false=熔断打开中。
// 用只读 IsOpen:Allow 会消费半开探测名额,管理端轮询健康会反复占用探测窗口。
func (s *Service) ChannelOpen(ctx context.Context, id string) bool {
	if s.Breaker == nil {
		return true
	}
	return !s.Breaker.IsOpen(ctx, id)
}

// isTransient 判断是否为可重试的瞬时错误(超时/连接重置/EOF)。
// 用于单渠道内退避重试一次,再决定是否跨渠道故障转移。
func isTransient(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "timeout") || strings.Contains(m, "eof") ||
		strings.Contains(m, "connection reset") || strings.Contains(m, "broken pipe") ||
		strings.Contains(m, "temporary")
}

func mustID() string { return crypto.NewID() }

// costOr 模型级成本为 0 时回退到渠道级默认成本(与原 CostForAll 语义一致:未单独配置=用渠道级)。
func costOr(modelLevel, channelLevel int64) int64 {
	if modelLevel != 0 {
		return modelLevel
	}
	return channelLevel
}
