package billing

import (
	"testing"
	"time"
)

// 退避调度: N²×10s,封顶 10min。
func TestPendingBackoff(t *testing.T) {
	cases := []struct {
		attempts int
		want     time.Duration
	}{
		{1, 10 * time.Second},
		{2, 40 * time.Second},
		{3, 90 * time.Second},
		{5, 250 * time.Second},
		{30, 10 * time.Minute}, // 超上限封顶
	}
	for _, c := range cases {
		if got := pendingBackoff(c.attempts); got != c.want {
			t.Fatalf("pendingBackoff(%d) = %v, want %v", c.attempts, got, c.want)
		}
	}
}

// 放弃阈值: attempts+1 达到上限即转 abandoned。
func TestMaxPendingAttempts(t *testing.T) {
	if maxPendingAttempts != 20 {
		t.Fatalf("maxPendingAttempts want 20 got %d", maxPendingAttempts)
	}
}
