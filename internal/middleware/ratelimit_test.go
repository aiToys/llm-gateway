package middleware

import (
	"testing"
	"time"
)

// TestLocalBucketsIncGet 验证进程内限流桶(Redis 降级路径)的计数与读取。
func TestLocalBucketsIncGet(t *testing.T) {
	b := newLocalBuckets()
	// 不存在的 key 读为 0。
	if got := b.get("k1"); got != 0 {
		t.Fatalf("absent key = %d, want 0", got)
	}
	// 自增累计。
	if got := b.inc("k1", 1, 65*time.Second); got != 1 {
		t.Fatalf("inc1 = %d, want 1", got)
	}
	if got := b.inc("k1", 5, 65*time.Second); got != 6 {
		t.Fatalf("inc2 = %d, want 6", got)
	}
	if got := b.get("k1"); got != 6 {
		t.Fatalf("get = %d, want 6", got)
	}
}

// TestLocalBucketsKeyIsolation 不同 key 互不干扰。
func TestLocalBucketsKeyIsolation(t *testing.T) {
	b := newLocalBuckets()
	b.inc("a", 3, 65*time.Second)
	b.inc("b", 10, 65*time.Second)
	if b.get("a") != 3 || b.get("b") != 10 {
		t.Fatal("keys leaked into each other")
	}
}

// TestIPAllowed 验证 API Key 的 IP 白名单匹配(含 CIDR)。
func TestIPAllowed(t *testing.T) {
	if !ipAllowed("1.2.3.4", nil) {
		t.Error("empty whitelist should allow all")
	}
	if !ipAllowed("1.2.3.4", []string{"1.2.3.4"}) {
		t.Error("exact match should allow")
	}
	if ipAllowed("1.2.3.5", []string{"1.2.3.4"}) {
		t.Error("non-listed IP should be denied")
	}
	if !ipAllowed("10.0.0.5", []string{"10.0.0.0/8"}) {
		t.Error("CIDR match should allow")
	}
	if ipAllowed("192.168.0.1", []string{"10.0.0.0/8"}) {
		t.Error("out-of-CIDR should be denied")
	}
	if ipAllowed("", []string{"1.2.3.4"}) {
		t.Error("empty client IP with non-empty whitelist should be denied")
	}
}
