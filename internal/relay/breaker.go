package relay

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/aitoys/llm-gateway/internal/metrics"
	"github.com/redis/go-redis/v9"
)

// CircuitBreaker 渠道熔断器抽象。
// 多 edge 副本部署时应使用 Redis 实现,使熔断状态跨副本共享。
type CircuitBreaker interface {
	// IsOpen 只读查询当前是否处于熔断打开态(不消费半开探测名额)。
	// 用于路由过滤、管理端健康展示等"只看不动"场景。
	IsOpen(ctx context.Context, id string) bool
	// Allow 是否放行(关闭/半开→放行;打开中→拒绝)。
	// 注意:Redis 实现在冷却到期后的半开端点会以 SetNX 原子抢占"探测名额",有副作用——
	// 仅在真正决定调用该渠道之前调用,禁止用于遍历过滤多个候选渠道。
	Allow(ctx context.Context, id string) bool
	// OnSuccess 调用成功,复位。
	OnSuccess(ctx context.Context, id string)
	// OnFailure 调用失败,累计;达阈值则打开。
	OnFailure(ctx context.Context, id string)
}

// --- 内存实现(单实例 / 测试用) ---

type memBreaker struct {
	mu        sync.Mutex
	threshold int
	cooldown  time.Duration
	state     map[string]*memState
}

type memState struct {
	failures  int
	openUntil time.Time
}

// NewBreaker 内存熔断器(连续失败 threshold 次打开,冷却 cooldown)。
func NewBreaker(threshold int, cooldown time.Duration) CircuitBreaker {
	if threshold <= 0 {
		threshold = 3
	}
	if cooldown <= 0 {
		cooldown = 60 * time.Second
	}
	return &memBreaker{threshold: threshold, cooldown: cooldown, state: make(map[string]*memState)}
}

func (b *memBreaker) IsOpen(_ context.Context, id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.state[id]
	if s == nil {
		return false
	}
	return time.Now().Before(s.openUntil)
}

func (b *memBreaker) Allow(_ context.Context, id string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.state[id]
	if s == nil {
		return true
	}
	return !time.Now().Before(s.openUntil)
}

func (b *memBreaker) OnSuccess(_ context.Context, id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.state, id)
	metrics.ChannelUp.WithLabelValues(id).Set(1)
}

func (b *memBreaker) OnFailure(_ context.Context, id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s := b.state[id]
	if s == nil {
		s = &memState{}
		b.state[id] = s
	}
	// 已打开:不再累加(避免冷却期内反复续期 openUntil,与 redisBreaker 行为一致)。
	if !time.Now().Before(s.openUntil) {
		s.failures++
	}
	if s.failures >= b.threshold {
		s.openUntil = time.Now().Add(b.cooldown)
		s.failures = 0 // trip 时清零,冷却结束后从 0 重新计数
		metrics.ChannelUp.WithLabelValues(id).Set(0)
	}
}

// --- Redis 实现(多副本共享) ---
//
// 状态键:
//   cb:{id}:fail  — 失败计数(INCR),带窗口 TTL 使计数自然衰减
//   cb:{id}:open  — 打开标记,带 cooldown TTL;过期即自动进入"半开",允许试探
//
// 行为:
//   Allow    — 仅当 open 键存在时拒绝
//   OnSuccess— 清除 fail 与 open(半开试探成功 → 关闭)
//   OnFailure— fail+1;首次设窗口 TTL;累计达阈值则置 open(cooldown TTL)并清零 fail

type redisBreaker struct {
	rdb         *redis.Client
	threshold   int
	cooldown    time.Duration
	failWindow  time.Duration
	probeWindow time.Duration
}

// NewRedisBreaker Redis 共享熔断器(多 edge 副本一致)。
func NewRedisBreaker(rdb *redis.Client, threshold int, cooldown time.Duration) CircuitBreaker {
	if threshold <= 0 {
		threshold = 3
	}
	if cooldown <= 0 {
		cooldown = 60 * time.Second
	}
	// 半开探测窗口: 冷却到期后,仅允许 1 个请求试探上游是否恢复,避免所有副本瞬间涌入。
	probe := cooldown / 6
	if probe < 5*time.Second {
		probe = 5 * time.Second
	}
	return &redisBreaker{rdb: rdb, threshold: threshold, cooldown: cooldown, failWindow: cooldown, probeWindow: probe}
}

func (b *redisBreaker) openKey(id string) string  { return "cb:" + id + ":open" }
func (b *redisBreaker) failKey(id string) string  { return "cb:" + id + ":fail" }
func (b *redisBreaker) probeKey(id string) string { return "cb:" + id + ":probe" }

// IsOpen 只读查询熔断是否打开(仅 EXISTS openKey,不消费半开探测名额)。
// route 过滤候选渠道、ChannelOpen 管理端展示健康时使用;避免遍历多个候选时
// 把每个渠道的探测名额都占满导致冷却结束后恢复极慢。
func (b *redisBreaker) IsOpen(ctx context.Context, id string) bool {
	n, err := b.rdb.Exists(ctx, b.openKey(id)).Result()
	if err != nil {
		return false // Redis 故障视为未熔断(放行),由故障转移兜底
	}
	return n > 0
}

func (b *redisBreaker) Allow(ctx context.Context, id string) bool {
	openK, probeK := b.openKey(id), b.probeKey(id)
	n, err := b.rdb.Exists(ctx, openK).Result()
	if err != nil {
		return true // Redis 故障时放行(故障转移链路自行兜底),避免误杀
	}
	if n > 0 {
		return false // 仍在熔断冷却期
	}
	// 冷却已过期 → 进入半开: 仅放行首个试探请求(SetNX 原子抢占),其余副本在探测窗口内拒绝。
	ok, err := b.rdb.SetNX(ctx, probeK, "1", b.probeWindow).Result()
	if err != nil {
		return true
	}
	return ok
}

func (b *redisBreaker) OnSuccess(ctx context.Context, id string) {
	_ = b.rdb.Del(ctx, b.failKey(id), b.openKey(id), b.probeKey(id)).Err()
	metrics.ChannelUp.WithLabelValues(id).Set(1)
}

func (b *redisBreaker) OnFailure(ctx context.Context, id string) {
	failKey, openKey, probeKey := b.failKey(id), b.openKey(id), b.probeKey(id)
	// 已打开则不再累加(避免无意义刷新)
	if n, _ := b.rdb.Exists(ctx, openKey).Result(); n > 0 {
		return
	}
	// 半开探测失败:立即重新打开熔断。冷却刚过期时 Allow 放行单个试探请求(SetNX probe),
	// 若该请求仍失败,说明上游未恢复——必须立刻回到冷却态,而非累积 threshold 次失败才跳闸
	// (否则每轮探测都把流量漏给不健康的上游,放大故障)。
	if n, _ := b.rdb.Exists(ctx, probeKey).Result(); n > 0 {
		_ = b.rdb.Set(ctx, openKey, "1", b.cooldown).Err()
		_ = b.rdb.Del(ctx, failKey, probeKey).Err()
		metrics.ChannelUp.WithLabelValues(id).Set(0)
		return
	}
	n, err := b.rdb.Incr(ctx, failKey).Result()
	if err != nil {
		return
	}
	if n == 1 {
		// 首次失败:给计数一个衰减窗口
		_ = b.rdb.Expire(ctx, failKey, b.failWindow).Err()
	}
	if int(n) >= b.threshold {
		// 打开熔断 + 清零计数(冷却结束后从 0 开始);清除半开探测标记。
		_ = b.rdb.Set(ctx, openKey, "1", b.cooldown).Err()
		_ = b.rdb.Del(ctx, failKey, probeKey).Err()
		metrics.ChannelUp.WithLabelValues(id).Set(0)
	}
}

// String 便于日志/调试。
func (b *redisBreaker) String() string {
	return fmt.Sprintf("redisBreaker(threshold=%d,cooldown=%s)", b.threshold, b.cooldown)
}
