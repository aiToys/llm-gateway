package payment

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// Mock 端到端: CreateOrder → ParseNotify(模拟支付完成) → QueryOrder 报告已支付。
func TestMockProviderRoundTrip(t *testing.T) {
	m := NewMock()
	in := CreateOrderInput{OutTradeNo: "GW-TEST-1", AmountCents: 9900, Subject: "充值"}
	prepay, err := m.CreateOrder(in)
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if !strings.Contains(prepay, "GW-TEST-1") {
		t.Fatalf("prepay data should contain out_trade_no, got %q", prepay)
	}

	// 支付前查单: 未支付。
	if paid, _, _ := m.QueryOrder("GW-TEST-1"); paid {
		t.Fatalf("should not be paid before notify")
	}

	// 触发支付完成通知(模拟用户扫码付款后的回调)。
	r := makeMockNotifyRequest("GW-TEST-1", 9900)
	out, txn, amt, err := m.ParseNotify(r)
	if err != nil {
		t.Fatalf("parse notify: %v", err)
	}
	if out != "GW-TEST-1" || amt != 9900 || txn == "" {
		t.Fatalf("notify parsed wrong: out=%q txn=%q amt=%d", out, txn, amt)
	}

	// 支付后查单: 已支付。
	if paid, _, _ := m.QueryOrder("GW-TEST-1"); !paid {
		t.Fatalf("should be paid after notify")
	}
}

// 回调重入幂等: 重复 ParseNotify 不会出错,查单仍为已支付(入账幂等由 Service.settle + store.MarkPaid 保证)。
func TestMockNotifyIdempotent(t *testing.T) {
	m := NewMock()
	for i := 0; i < 3; i++ {
		r := makeMockNotifyRequest("GW-TEST-2", 5000)
		if _, _, _, err := m.ParseNotify(r); err != nil {
			t.Fatalf("repeat notify %d: %v", i, err)
		}
	}
	if paid, _, _ := m.QueryOrder("GW-TEST-2"); !paid {
		t.Fatalf("should remain paid after repeated notify")
	}
}

func TestMockNotifyMissingTradeNo(t *testing.T) {
	m := NewMock()
	r := &http.Request{Method: http.MethodPost}
	if _, _, _, err := m.ParseNotify(r); err == nil {
		t.Fatalf("expected error for missing out_trade_no")
	}
}

// alipay 金额换算: 分 ↔ 元 往返,含四舍五入。
func TestCentsYuanRoundTrip(t *testing.T) {
	cases := []int64{1, 99, 100, 9900, 50000, 12345}
	for _, cents := range cases {
		yuan := centsToYuan(cents)
		back, err := yuanToCents(yuan)
		if err != nil {
			t.Fatalf("yuanToCents(%s): %v", yuan, err)
		}
		if back != cents {
			t.Fatalf("round trip cents %d -> %s -> %d", cents, yuan, back)
		}
	}
	// 0.01 元 = 1 分;0.5 元四舍五入到 50 分。
	if c, _ := yuanToCents("0.01"); c != 1 {
		t.Fatalf("0.01 yuan = %d cents, want 1", c)
	}
}

// out_trade_no 在大量生成下应唯一(商户端不重复)。
func TestNewOutTradeNoUnique(t *testing.T) {
	seen := make(map[string]struct{}, 2000)
	for i := 0; i < 2000; i++ {
		no, err := newOutTradeNo()
		if err != nil {
			t.Fatalf("gen: %v", err)
		}
		if !strings.HasPrefix(no, "GW") {
			t.Fatalf("unexpected prefix: %q", no)
		}
		if _, dup := seen[no]; dup {
			t.Fatalf("duplicate out_trade_no: %q", no)
		}
		seen[no] = struct{}{}
	}
}

func makeMockNotifyRequest(outTradeNo string, amountCents int64) *http.Request {
	// 用 URL query 携带参数: ParseForm 会将其并入 Form,FormValue 可读到。
	// (真实 mock notify 通常也以 query 触发: POST /api/payments/mock/notify?out_trade_no=...)
	r := &http.Request{
		Method: http.MethodPost,
		URL: &url.URL{
			RawQuery: url.Values{
				"out_trade_no": {outTradeNo},
				"amount_cents": {itoa(amountCents)},
			}.Encode(),
		},
		Header: make(http.Header),
	}
	return r
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
