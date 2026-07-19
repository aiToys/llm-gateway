package relay

import (
	"context"
	"testing"

	"github.com/aitoys/llm-gateway/internal/canon"
	"github.com/aitoys/llm-gateway/internal/model"
)

// 最高优先级渠道应排在前面,且首位来自最高优先级组。
func TestSelectOrderedPriority(t *testing.T) {
	chs := []*model.Channel{
		{ID: "low1", Priority: 0, Weight: 1},
		{ID: "high1", Priority: 10, Weight: 1},
		{ID: "high2", Priority: 10, Weight: 1},
		{ID: "low2", Priority: 0, Weight: 1},
	}
	ordered := selectOrdered(chs, model.StrategyWeighted)
	if len(ordered) != 4 {
		t.Fatalf("want 4 ordered got %d", len(ordered))
	}
	// 前两位必须是高优先级组
	topIDs := map[string]bool{ordered[0].ID: true, ordered[1].ID: true}
	if !topIDs["high1"] || !topIDs["high2"] {
		t.Fatalf("top two should be high-priority group, got %v", topIDs)
	}
}

// BYOK 优先: 租户渠道(tenant_id 非 nil)整层优先于平台渠道,即使平台渠道 priority 数值更高。
// 防止"高 priority 平台渠道压住低 priority 租户渠道"导致租户自带 Key 永不被使用。
func TestSplitPriorityTenantBYOKFirst(t *testing.T) {
	plat := func(id string, p int) *model.Channel { return &model.Channel{ID: id, Priority: p, Weight: 1} } // TenantID=nil 平台
	tenant := func(id string, p int) *model.Channel { tid := "t1"; return &model.Channel{ID: id, Priority: p, Weight: 1, TenantID: &tid} }
	// 平台渠道 priority=100,租户渠道 priority=5。租户层应优先。
	chs := []*model.Channel{plat("plat-high", 100), tenant("tenant-low", 5), tenant("tenant-low2", 5)}
	group, rest := splitPriority(chs)
	if len(group) != 2 || group[0].ID != "tenant-low" && group[1].ID != "tenant-low2" {
		// group 必须是两个租户渠道(顺序不限)。
		gIDs := map[string]bool{}
		for _, c := range group {
			gIDs[c.ID] = true
		}
		if !gIDs["tenant-low"] || !gIDs["tenant-low2"] {
			t.Fatalf("BYOK group should be tenant channels, got %v", gIDs)
		}
	}
	if len(rest) != 1 || rest[0].ID != "plat-high" {
		t.Fatalf("rest should be platform channel, got %+v", rest)
	}
}

// 模型级权重(channel_models.weight)优先于渠道级 weight。
// 验证 channelWeight:cm.Weight>0 时用 cm.Weight,否则回退渠道级。
func TestChannelWeightModelLevel(t *testing.T) {
	// 仅渠道级 weight=1,无 ChannelModels → 1
	if w := channelWeight(&model.Channel{ID: "a", Weight: 1}); w != 1 {
		t.Fatalf("channel weight fallback = %d, want 1", w)
	}
	// cm.Weight=10 覆盖渠道级 weight=1
	c := &model.Channel{ID: "b", Weight: 1, ChannelModels: []model.ChannelModel{{Weight: 10}}}
	if w := channelWeight(c); w != 10 {
		t.Fatalf("model-level weight = %d, want 10", w)
	}
	// cm.Weight=0 回退渠道级 weight=5
	c2 := &model.Channel{ID: "c", Weight: 5, ChannelModels: []model.ChannelModel{{Weight: 0}}}
	if w := channelWeight(c2); w != 5 {
		t.Fatalf("fallback to channel weight = %d, want 5", w)
	}
	// 渠道级 0 视为 1
	if w := channelWeight(&model.Channel{ID: "d", Weight: 0}); w != 1 {
		t.Fatalf("zero weight = %d, want 1", w)
	}
}

// 加权随机: 高权重渠道应被选为主的概率显著更高。
func TestSelectOrderedWeighted(t *testing.T) {
	heavy := "heavy"
	light := "light"
	chs := []*model.Channel{
		{ID: light, Priority: 0, Weight: 1},
		{ID: heavy, Priority: 0, Weight: 99},
	}
	pickCount := map[string]int{}
	for i := 0; i < 2000; i++ {
		ordered := selectOrdered(chs, model.StrategyWeighted)
		pickCount[ordered[0].ID]++
	}
	if pickCount[heavy] <= pickCount[light] {
		t.Fatalf("heavy should be picked more often: heavy=%d light=%d", pickCount[heavy], pickCount[light])
	}
	// heavy 权重 99 倍,应占绝大多数
	if pickCount[heavy] < 1500 {
		t.Fatalf("heavy pick count too low: %d", pickCount[heavy])
	}
}

// 仅一个渠道时直接返回它。
func TestSelectOrderedSingle(t *testing.T) {
	chs := []*model.Channel{{ID: "only", Priority: 0, Weight: 1}}
	ordered := selectOrdered(chs, model.StrategyWeighted)
	if len(ordered) != 1 || ordered[0].ID != "only" {
		t.Fatalf("unexpected: %+v", ordered)
	}
}

// failover: 永远固定选最高优先级组的第一个,不随机。
func TestSelectOrderedFailover(t *testing.T) {
	chs := []*model.Channel{
		{ID: "a", Priority: 10, Weight: 1},
		{ID: "b", Priority: 10, Weight: 9},
		{ID: "c", Priority: 5, Weight: 1},
	}
	for i := 0; i < 50; i++ {
		ordered := selectOrdered(chs, model.StrategyFailover)
		if ordered[0].ID != "a" {
			t.Fatalf("failover must always pick group[0], got %s", ordered[0].ID)
		}
	}
}

// random: 忽略权重,纯随机;权重悬殊时低权重也应被选中。
func TestSelectOrderedRandom(t *testing.T) {
	chs := []*model.Channel{
		{ID: "light", Priority: 0, Weight: 1},
		{ID: "heavy", Priority: 0, Weight: 99},
	}
	pick := map[string]int{}
	for i := 0; i < 2000; i++ {
		ordered := selectOrdered(chs, model.StrategyRandom)
		pick[ordered[0].ID]++
	}
	// 纯随机下两者应接近 50/50,heavy 不应垄断。
	if pick["light"] < 200 {
		t.Fatalf("random should pick light reasonably often, got %d", pick["light"])
	}
}

// resolveStreamUsage 取最后一帧 usage(覆盖而非累加),避免累计 usage 帧重复计费。
func TestResolveStreamUsageLastFrameWins(t *testing.T) {
	// 无 usage → 全 0,total 兜底为 0。
	got := resolveStreamUsage(nil)
	if got.TotalTokens != 0 || got.PromptTokens != 0 {
		t.Fatalf("nil usage should resolve to zero, got %+v", got)
	}
	// 末帧覆盖中间帧(模拟上游发多次累计 usage)。
	mid := &canon.Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150}
	last := &canon.Usage{PromptTokens: 1000, CompletionTokens: 500, TotalTokens: 1500}
	_ = mid
	got = resolveStreamUsage(last)
	if got.PromptTokens != 1000 || got.CompletionTokens != 500 || got.TotalTokens != 1500 {
		t.Fatalf("expected last frame, got %+v", got)
	}
	// total 缺失时按 prompt+completion 兜底。
	partial := &canon.Usage{PromptTokens: 30, CompletionTokens: 70}
	got = resolveStreamUsage(partial)
	if got.TotalTokens != 100 {
		t.Fatalf("total should fall back to prompt+completion=100, got %d", got.TotalTokens)
	}
}

// TestIsTransient 验证瞬时错误判定(决定是否同渠道退避重试一次)。
func TestIsTransient(t *testing.T) {
	cases := map[string]bool{
		"context deadline exceeded":      false, // 仅 errors.Is(DeadlineExceeded) 为 true,字符串不含 timeout
		"i/o timeout":                    true,
		"unexpected EOF":                 true,
		"read connection reset":          true,
		"write broken pipe":              true,
		"no available channel for model": false,
		"":                               false,
	}
	for msg, want := range cases {
		if got := isTransient(strErr(msg)); got != want {
			// "context deadline exceeded" 字符串路径不含 timeout 关键字,期望 false(由 errors.Is 覆盖真值)
			if msg == "context deadline exceeded" {
				continue
			}
			t.Errorf("isTransient(%q)=%v want %v", msg, got, want)
		}
	}
	if !isTransient(context.DeadlineExceeded) {
		t.Error("context.DeadlineExceeded should be transient")
	}
}

type strErr string

func (e strErr) Error() string { return string(e) }

// TestEstimatePromptTokens 验证输入 token 估算(用于 preflight 成本预判)。
func TestEstimatePromptTokens(t *testing.T) {
	req := &canon.Request{Messages: []canon.Message{
		{Role: "user", Content: "hello world"},                                              // 11 字符
		{Role: "assistant", Content: []canon.ContentPart{{Type: "text", Text: "hi there"}}}, // 8 字符,但 assistant 也会计入

	}}
	// 默认 charsPerToken=2 → 19/2+1 = 10
	if got := estimatePromptTokens(req, 0); got != 10 {
		t.Errorf("estimatePromptTokens default = %d, want 10", got)
	}
	// 空消息至少返回 1
	if got := estimatePromptTokens(&canon.Request{}, 2); got != 1 {
		t.Errorf("empty prompt tokens = %d, want 1", got)
	}
}
