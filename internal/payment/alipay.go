package payment

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/smartwalle/alipay/v3"
)

// Alipay 基于电脑网站支付(alipay.trade.page.pay)。
//   - 下单返回跳转 URL,前端 location.href 跳到支付宝收银台。
//   - 回调为 form POST,DecodeNotification 内含验签。
type Alipay struct {
	client *alipay.Client
}

// NewAlipay 构造支付宝客户端。privateKey/alipayPublicKey 为 PEM 文本(含 BEGIN/END 头);
// production=false 时走沙箱(openapi.alipaydev.com)。
func NewAlipay(appID, privateKey, alipayPublicKey string, production bool) (*Alipay, error) {
	if appID == "" || privateKey == "" || alipayPublicKey == "" {
		return nil, fmt.Errorf("alipay: appid/private_key/alipay_public_key 均不能为空")
	}
	c, err := alipay.New(appID, privateKey, production)
	if err != nil {
		return nil, fmt.Errorf("alipay new client: %w", err)
	}
	if err := c.LoadAliPayPublicKey(alipayPublicKey); err != nil {
		return nil, fmt.Errorf("alipay load public key: %w", err)
	}
	return &Alipay{client: c}, nil
}

func (a *Alipay) Name() string { return "alipay" }

func (a *Alipay) CreateOrder(in CreateOrderInput) (string, error) {
	pay := alipay.TradePagePay{
		Trade: alipay.Trade{
			NotifyURL:   in.NotifyURL,
			ReturnURL:   in.ReturnURL,
			Subject:     defaultSubject(in.Subject),
			OutTradeNo:  in.OutTradeNo,
			TotalAmount: centsToYuan(in.AmountCents),
			ProductCode: "FAST_INSTANT_TRADE_PAY",
		},
	}
	u, err := a.client.TradePagePay(pay)
	if err != nil {
		return "", fmt.Errorf("alipay trade page pay: %w", err)
	}
	return u.String(), nil
}

func (a *Alipay) QueryOrder(outTradeNo string) (bool, string, error) {
	rsp, err := a.client.TradeQuery(context.Background(), alipay.TradeQuery{OutTradeNo: outTradeNo})
	if err != nil {
		return false, "", fmt.Errorf("alipay query: %w", err)
	}
	// TRADE_SUCCESS / TRADE_FINISHED 视为已支付。
	paid := rsp.TradeStatus == alipay.TradeStatusSuccess || rsp.TradeStatus == alipay.TradeStatusFinished
	return paid, rsp.TradeNo, nil
}

func (a *Alipay) ParseNotify(r *http.Request) (string, string, int64, error) {
	if err := r.ParseForm(); err != nil {
		return "", "", 0, err
	}
	n, err := a.client.DecodeNotification(r.Context(), r.PostForm)
	if err != nil {
		return "", "", 0, fmt.Errorf("alipay verify notify: %w", err)
	}
	// 仅交易成功才视为有效入账通知;否则当普通状态回调忽略。
	if n.TradeStatus != alipay.TradeStatusSuccess && n.TradeStatus != alipay.TradeStatusFinished {
		return n.OutTradeNo, "", 0, errNotifyIgnored
	}
	amt, _ := yuanToCents(n.TotalAmount)
	return n.OutTradeNo, n.TradeNo, amt, nil
}

// centsToYuan 分 → 元(两位小数字符串),支付宝要求 total_amount 单位为元。
func centsToYuan(cents int64) string {
	return strconv.FormatFloat(float64(cents)/100, 'f', 2, 64)
}

// yuanToCents 元字符串 → 分。
func yuanToCents(yuan string) (int64, error) {
	yuan = strings.TrimSpace(yuan)
	if yuan == "" {
		return 0, nil
	}
	f, err := strconv.ParseFloat(yuan, 64)
	if err != nil {
		return 0, err
	}
	return int64(f*100 + 0.5), nil
}

func defaultSubject(s string) string {
	if strings.TrimSpace(s) != "" {
		return s
	}
	return "账户充值"
}

// errNotifyIgnored 表示回调到达但交易状态非"成功"(如等待买家付款),调用方应 ACK 但不入账。
var errNotifyIgnored = fmt.Errorf("notify ignored: trade not success")
