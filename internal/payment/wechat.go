package payment

import (
	"context"
	"crypto/rsa"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/core/auth/verifiers"
	"github.com/wechatpay-apiv3/wechatpay-go/core/downloader"
	"github.com/wechatpay-apiv3/wechatpay-go/core/notify"
	"github.com/wechatpay-apiv3/wechatpay-go/core/option"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/native"
	"github.com/wechatpay-apiv3/wechatpay-go/utils"
)

// Wechat 基于微信支付 Native(扫码支付,PC Web 场景)。
//   - 下单返回 code_url,前端渲染成二维码。
//   - 回调为 JSON,notify.Handler 内含验签 + AES-GCM 解密。
//   - 商户私钥支持文件路径或 PEM 文本。
type Wechat struct {
	client   *core.Client
	notify   *notify.Handler
	appid    string
	mchid    string
	apiSVC   *native.NativeApiService
	apiV3Key string
}

// NewWechat 构造微信支付客户端。
//   - appid: 应用 AppID(公众号/小程序/移动应用)
//   - mchid: 商户号
//   - mchSerial: 商户证书序列号
//   - privateKey: PEM 文本 或 指向 apiclient_key.pem 的文件路径
//   - apiV3Key: APIv3 密钥(用于回调解密)
func NewWechat(appid, mchid, mchSerial, privateKey, apiV3Key string) (*Wechat, error) {
	if appid == "" || mchid == "" || mchSerial == "" || privateKey == "" || apiV3Key == "" {
		return nil, fmt.Errorf("wechat: appid/mchid/mch_serial/private_key/api_v3_key 均不能为空")
	}
	pk, err := loadWechatPrivateKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("wechat load private key: %w", err)
	}
	ctx := context.Background()
	opts := []core.ClientOption{
		option.WithWechatPayAutoAuthCipher(mchid, mchSerial, pk, apiV3Key),
	}
	client, err := core.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("wechat new client: %w", err)
	}
	// 回调验签需平台证书验证者: AutoAuthCipher 已注册下载器,取其 visitor 构造 verifier。
	visitor := downloader.MgrInstance().GetCertificateVisitor(mchid)
	verifier := verifiers.NewSHA256WithRSAVerifier(visitor)
	return &Wechat{
		client:   client,
		notify:   notify.NewNotifyHandler(apiV3Key, verifier),
		appid:    appid,
		mchid:    mchid,
		apiSVC:   &native.NativeApiService{Client: client},
		apiV3Key: apiV3Key,
	}, nil
}

func (w *Wechat) Name() string { return "wechat" }

func (w *Wechat) CreateOrder(in CreateOrderInput) (string, error) {
	expire := time.Now().Add(15 * time.Minute)
	resp, _, err := w.apiSVC.Prepay(context.Background(), native.PrepayRequest{
		Appid:       core.String(w.appid),
		Mchid:       core.String(w.mchid),
		Description: core.String(defaultSubject(in.Subject)),
		OutTradeNo:  core.String(in.OutTradeNo),
		TimeExpire:  core.Time(expire),
		NotifyUrl:   core.String(in.NotifyURL),
		Amount: &native.Amount{
			Currency: core.String("CNY"),
			Total:    core.Int64(in.AmountCents),
		},
	})
	if err != nil {
		return "", fmt.Errorf("wechat prepay: %w", err)
	}
	if resp.CodeUrl == nil {
		return "", fmt.Errorf("wechat prepay: empty code_url")
	}
	return *resp.CodeUrl, nil
}

func (w *Wechat) QueryOrder(outTradeNo string) (bool, string, error) {
	tx, _, err := w.apiSVC.QueryOrderByOutTradeNo(context.Background(), native.QueryOrderByOutTradeNoRequest{
		Mchid:      core.String(w.mchid),
		OutTradeNo: core.String(outTradeNo),
	})
	if err != nil {
		return false, "", fmt.Errorf("wechat query: %w", err)
	}
	paid := tx.TradeState != nil && *tx.TradeState == "SUCCESS"
	txnID := ""
	if tx.TransactionId != nil {
		txnID = *tx.TransactionId
	}
	return paid, txnID, nil
}

func (w *Wechat) ParseNotify(r *http.Request) (string, string, int64, error) {
	content := new(payments.Transaction)
	if _, err := w.notify.ParseNotifyRequest(r.Context(), r, content); err != nil {
		return "", "", 0, fmt.Errorf("wechat parse notify: %w", err)
	}
	// 仅 SUCCESS 入账;其他状态(如 NOTPAY)忽略但 ACK。
	if content.TradeState == nil || *content.TradeState != "SUCCESS" {
		out := ""
		if content.OutTradeNo != nil {
			out = *content.OutTradeNo
		}
		return out, "", 0, errNotifyIgnored
	}
	out, txn := "", ""
	if content.OutTradeNo != nil {
		out = *content.OutTradeNo
	}
	if content.TransactionId != nil {
		txn = *content.TransactionId
	}
	var amt int64
	if content.Amount != nil && content.Amount.PayerTotal != nil {
		amt = *content.Amount.PayerTotal
	}
	return out, txn, amt, nil
}

// loadWechatPrivateKey 兼容 PEM 文本与文件路径两种配置方式。
func loadWechatPrivateKey(s string) (*rsa.PrivateKey, error) {
	if _, err := os.Stat(s); err == nil {
		return utils.LoadPrivateKeyWithPath(s)
	}
	return utils.LoadPrivateKey(s)
}
