package relay

import (
	"context"
	"testing"
	"time"
)

// TestMemBreakerIsOpenMatchesAllow 验证只读 IsOpen 与有副作用的 Allow 在"是否打开"判定上语义一致。
// route 过滤改用 IsOpen(Allow 在 RedisBreaker 有半开探测名额副作用,遍历候选会占满)。
func TestMemBreakerIsOpenMatchesAllow(t *testing.T) {
	ctx := context.Background()
	b := NewBreaker(3, 200*time.Millisecond)
	id := "ch-1"

	// 初始:关闭 → IsOpen=false, Allow=true。
	if b.IsOpen(ctx, id) {
		t.Fatal("新渠道不应处于打开态")
	}
	if !b.Allow(ctx, id) {
		t.Fatal("关闭态应放行")
	}

	// 累计失败到阈值 → 打开。
	b.OnFailure(ctx, id)
	b.OnFailure(ctx, id)
	b.OnFailure(ctx, id)
	if !b.IsOpen(ctx, id) {
		t.Fatal("达阈值应打开,IsOpen=true")
	}
	if b.Allow(ctx, id) {
		t.Fatal("打开态应拒绝")
	}

	// 冷却结束 → 关闭(注意:打开时 OnFailure 不再累加,不会续期)。
	time.Sleep(250 * time.Millisecond)
	if b.IsOpen(ctx, id) {
		t.Fatal("冷却结束应自动关闭")
	}
	if !b.Allow(ctx, id) {
		t.Fatal("冷却结束后应放行")
	}
}

// TestMemBreakerOnFailureNoAccrueWhileOpen 验证打开态下 OnFailure 不再累加/续期,
// 与 redisBreaker 行为一致(否则冷却期内反复失败会无限续期 openUntil,永不恢复)。
func TestMemBreakerOnFailureNoAccrueWhileOpen(t *testing.T) {
	ctx := context.Background()
	b := NewBreaker(2, 100*time.Millisecond)
	id := "ch-2"

	b.OnFailure(ctx, id)
	b.OnFailure(ctx, id) // 打开
	if !b.IsOpen(ctx, id) {
		t.Fatal("应打开")
	}
	// 冷却期内再失败若干次,不应延长 openUntil。
	for i := 0; i < 5; i++ {
		b.OnFailure(ctx, id)
	}
	time.Sleep(120 * time.Millisecond)
	if b.IsOpen(ctx, id) {
		t.Fatal("冷却期内失败不应续期,到期应关闭")
	}
}

// TestMemBreakerIsOpenIsReadOnly 验证 IsOpen 不会改变状态(连续调用结果稳定,
// 且不影响后续 Allow 的打开/关闭判定)。
func TestMemBreakerIsOpenIsReadOnly(t *testing.T) {
	ctx := context.Background()
	b := NewBreaker(1, 100*time.Millisecond)
	id := "ch-3"

	b.OnFailure(ctx, id) // threshold=1 → 打开
	for i := 0; i < 10; i++ {
		if !b.IsOpen(ctx, id) {
			t.Fatal("打开态下 IsOpen 应稳定返回 true(只读不改状态)")
		}
	}
}
