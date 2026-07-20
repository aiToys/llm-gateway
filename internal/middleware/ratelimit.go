package middleware

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/aitoys/llm-gateway/internal/auth"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// PlaygroundLimits 聊天台(JWT 鉴权、无 API Key)的默认每用户限流。
// 0 表示该维度不限。由配置 Web.Playground 注入。
type PlaygroundLimits struct {
	RPMLimit int
	TPMLimit int
}

// RateLimit 按主体身份的 RPM/TPM 限流。
//   - 身份键:走 API Key 鉴权用 APIKeyID;走 Web JWT(聊天台)用 UserID。两类主体都纳入限流,
//     避免 JWT 路径(无 API Key)完全绕过限流无限冲击上游。
//   - 限额来源:API Key 主体用其自身限额(Subject.RPMLimit 等);JWT 主体用 Playground 默认限额。
//   - Redis 可用: 跨副本共享的滑动分钟桶(INCR/IncrBy + TTL)。
//   - Redis 不可用(rdb==nil 或故障): 降级到进程内内存桶(fail-closed)。
//
// RPM: 请求前对当前分钟桶自增,超限返回 429。
// TPM: 请求前用"当前分钟已用"预判;请求后按实际 token(g.Set("rl_tokens"))补登。
// 限额为 0 表示不限。
func RateLimit(rdb *redis.Client, playground PlaygroundLimits) gin.HandlerFunc {
	local := newLocalBuckets()
	return func(g *gin.Context) {
		sub, ok := auth.FromContext(g.Request.Context())
		if !ok {
			g.Next()
			return
		}
		// 身份键:API Key 优先,否则用 UserID(聊天台 JWT)。两者皆空则不限(未鉴权路由)。
		ident := sub.APIKeyID
		isPlayground := ident == ""
		if isPlayground {
			ident = sub.UserID
		}
		if ident == "" {
			g.Next()
			return
		}
		// 限额:API Key 主体用其配额;聊天台用 Playground 默认。
		rpmLimit, tpmLimit := sub.RPMLimit, sub.TPMLimit
		if isPlayground {
			rpmLimit, tpmLimit = playground.RPMLimit, playground.TPMLimit
		}
		ctx := g.Request.Context()
		now := time.Now()
		minute := now.Format("200601021504")
		rpmKey := "rl:rpm:" + ident + ":" + minute
		tpmKey := "rl:tpm:" + ident + ":" + minute

		// RPM 预检
		if rpmLimit > 0 {
			if inc(rdb, local, ctx, rpmKey, 1) > int64(rpmLimit) {
				g.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"error": gin.H{"type": "rate_limit_exceeded", "message": "RPM limit exceeded"},
				})
				return
			}
		}
		// 日/月请求数配额预检(0=不限;仅 API Key 主体有此配额,聊天台无)。桶跨自然日/月对齐。
		if !isPlayground {
			if sub.DailyRequestLimit > 0 {
				k := "quota:req:d:" + ident + ":" + now.Format("20060102")
				if incTTL(rdb, local, ctx, k, 1, 25*time.Hour) > int64(sub.DailyRequestLimit) {
					g.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
						"error": gin.H{"type": "quota_exceeded", "message": "daily request quota exceeded"},
					})
					return
				}
			}
			if sub.MonthlyRequestLimit > 0 {
				k := "quota:req:m:" + ident + ":" + now.Format("200601")
				if incTTL(rdb, local, ctx, k, 1, 32*24*time.Hour) > int64(sub.MonthlyRequestLimit) {
					g.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
						"error": gin.H{"type": "quota_exceeded", "message": "monthly request quota exceeded"},
					})
					return
				}
			}
		}
		// TPM 预检(基于已记录的当前分钟用量;输入未知故仅按已用判断,真值在请求后补登)
		if tpmLimit > 0 {
			if get(rdb, local, ctx, tpmKey) >= int64(tpmLimit) {
				g.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"error": gin.H{"type": "rate_limit_exceeded", "message": "TPM limit exceeded"},
				})
				return
			}
		}
		g.Next()

		// 请求后:把本次 token 计入 TPM 桶(供后续请求预判)。
		if tpmLimit > 0 {
			if used := g.GetInt64("rl_tokens"); used > 0 {
				inc(rdb, local, ctx, tpmKey, used)
			}
		}
	}
}

// inc 自增分钟桶计数(固定 65s TTL);优先 Redis,故障/无 Redis 时回退内存桶。返回自增后的值。
func inc(rdb *redis.Client, local *localBuckets, ctx context.Context, key string, delta int64) int64 {
	return incTTL(rdb, local, ctx, key, delta, 65*time.Second)
}

// incTTL 自增桶计数并按 ttl 设过期(配额日/月桶用长 TTL,分钟桶用 65s)。
func incTTL(rdb *redis.Client, local *localBuckets, ctx context.Context, key string, delta int64, ttl time.Duration) int64 {
	if rdb != nil {
		if n, err := rdb.IncrBy(ctx, key, delta).Result(); err == nil {
			_ = rdb.Expire(ctx, key, ttl).Err() // 幂等设 TTL
			return n
		}
	}
	return local.inc(key, delta, ttl)
}

// get 取桶当前值;优先 Redis,故障回退内存。
func get(rdb *redis.Client, local *localBuckets, ctx context.Context, key string) int64 {
	if rdb != nil {
		if n, err := rdb.Get(ctx, key).Int64(); err == nil {
			return n
		}
	}
	return local.get(key)
}

// localBuckets 进程内分钟桶(Redis 降级时使用)。过期条目惰性回收。
type localBuckets struct {
	mu sync.Mutex
	m  map[string]*localBucket
}

type localBucket struct {
	n       int64
	expires time.Time
}

func newLocalBuckets() *localBuckets {
	b := &localBuckets{m: make(map[string]*localBucket)}
	go b.sweep()
	return b
}

func (b *localBuckets) inc(key string, delta int64, ttl time.Duration) int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	bucket, ok := b.m[key]
	now := time.Now()
	if !ok || now.After(bucket.expires) {
		bucket = &localBucket{expires: now.Add(ttl)}
		b.m[key] = bucket
	}
	bucket.n += delta
	return bucket.n
}

func (b *localBuckets) get(key string) int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	bucket, ok := b.m[key]
	if !ok || time.Now().After(bucket.expires) {
		return 0
	}
	return bucket.n
}

// sweep 周期回收过期桶,防止内存无限增长(降级路径下才有数据)。
func (b *localBuckets) sweep() {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for range t.C {
		now := time.Now()
		b.mu.Lock()
		for k, v := range b.m {
			if now.After(v.expires) {
				delete(b.m, k)
			}
		}
		b.mu.Unlock()
	}
}
