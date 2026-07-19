package payment

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Mock 用于开发/测试: 不依赖任何商户资质即可端到端验证"下单→支付→入账"全链路。
//   - CreateOrder 返回一个伪 code_url,前端二维码可正常渲染。
//   - 支付动作由外部显式触发: POST /api/payments/mock/notify?out_trade_no=xxx
//     (生产关闭 mock,避免被白嫖)。
//   - 状态记在进程内(开发足够);真实支付状态在 DB + 支付平台。
type Mock struct {
	mu     sync.Mutex
	paid   map[string]bool // outTradeNo -> paid
	txnSeq int
}

func NewMock() *Mock { return &Mock{paid: make(map[string]bool)} }

func (m *Mock) Name() string { return "mock" }

func (m *Mock) CreateOrder(in CreateOrderInput) (string, error) {
	// code_url 风格的占位串,二维码内容本身无意义,仅供前端渲染。
	return fmt.Sprintf("wxpay://mock/%s?amount=%d&ts=%d", in.OutTradeNo, in.AmountCents, time.Now().Unix()), nil
}

func (m *Mock) QueryOrder(outTradeNo string) (bool, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.paid[outTradeNo], mockTxnID(outTradeNo), nil
}

// ParseNotify 接受 ?out_trade_no= 触发的"支付完成"通知,并登记为已支付。
// 参数来源兼容 query / form body(开发触发方式不固定)。
func (m *Mock) ParseNotify(r *http.Request) (string, string, int64, error) {
	out := notifyValue(r, "out_trade_no")
	if out == "" {
		return "", "", 0, fmt.Errorf("missing out_trade_no")
	}
	amount, _ := parseInt64(notifyValue(r, "amount_cents"))
	m.mu.Lock()
	m.paid[out] = true
	m.txnSeq++
	seq := m.txnSeq
	m.mu.Unlock()
	return out, fmt.Sprintf("mock-txn-%d", seq), amount, nil
}

// notifyValue 从请求中取一个字段: 优先 query,再 form body(best-effort 解析)。
func notifyValue(r *http.Request, key string) string {
	if r.URL != nil {
		if v := r.URL.Query().Get(key); v != "" {
			return v
		}
	}
	_ = r.ParseForm() // 容错: POST 无 body 时可能返回 error,忽略
	if v := r.PostForm.Get(key); v != "" {
		return v
	}
	return r.Form.Get(key)
}

func mockTxnID(out string) string {
	if out == "" {
		return ""
	}
	return "mock-txn-" + out // 占位;mock 流水号本身无业务意义
}
