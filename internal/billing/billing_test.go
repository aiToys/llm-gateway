package billing

import (
	"testing"

	"github.com/aitoys/llm-gateway/internal/canon"
)

func TestQuotePrice(t *testing.T) {
	// 售价:输入 1 分/token,输出 2 分/token。
	u := canon.Usage{PromptTokens: 100, CompletionTokens: 50}
	price := QuotePrice(1_000_000, 2_000_000, 0, 0, u)
	// 100*1e6/1e6 + 50*2e6/1e6 = 100 + 100 = 200 分
	if price != 200 {
		t.Fatalf("price want 200 got %d", price)
	}
}

func TestQuoteCost(t *testing.T) {
	// 命中渠道成本:输入 0.2 分/token,输出 0.4 分/token。
	u := canon.Usage{PromptTokens: 100, CompletionTokens: 50}
	cost := QuoteCost(200_000, 400_000, 0, 0, u)
	// 100*2e5/1e6 + 50*4e5/1e6 = 20 + 20 = 40 分
	if cost != 40 {
		t.Fatalf("cost want 40 got %d", cost)
	}
}

func TestQuoteFlooringSmall(t *testing.T) {
	// gpt-4o-mini: 150/600 分每百万。小请求 floor 为 0。
	u := canon.Usage{PromptTokens: 14, CompletionTokens: 115}
	if price := QuotePrice(150, 600, 0, 0, u); price != 0 {
		t.Fatalf("small request price want 0 got %d", price)
	}
}

func TestMarginNonNegative(t *testing.T) {
	// 售价高于渠道成本 -> 毛利非负。
	u := canon.Usage{PromptTokens: 10000, CompletionTokens: 5000}
	price := QuotePrice(2500, 10000, 0, 0, u)
	cost := QuoteCost(1000, 4000, 0, 0, u)
	if price <= cost {
		t.Fatalf("price %d should exceed cost %d", price, cost)
	}
}

// TestQuoteCacheSegment 验证缓存分段计价: cacheRead 段按 cacheReadPrice(折扣),normal 段按 inputPrice。
func TestQuoteCacheSegment(t *testing.T) {
	// 100 prompt = 60 normal + 40 cache_read。输入 1 分/token,缓存读 0.1 分/token。
	u := canon.Usage{PromptTokens: 100, CacheReadTokens: 40, CompletionTokens: 0}
	price := QuotePrice(1_000_000, 2_000_000, 100_000, 0, u)
	// 60*1e6/1e6 + 40*1e5/1e6 = 60 + 4 = 64 分
	if price != 64 {
		t.Fatalf("cache price want 64 got %d", price)
	}
}

// TestQuoteCacheFallback 验证缓存价为 0 时,缓存 token 按输入全价(向后兼容,与无缓存等价)。
func TestQuoteCacheFallback(t *testing.T) {
	u := canon.Usage{PromptTokens: 100, CacheReadTokens: 40}
	price := QuotePrice(1_000_000, 2_000_000, 0, 0, u)
	// 60 normal + 40 cache(回退 input 1 分/token) = 100
	if price != 100 {
		t.Fatalf("fallback price want 100 got %d", price)
	}
}
