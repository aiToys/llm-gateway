package store

import (
	"context"
	"errors"
	"time"

	"github.com/aitoys/llm-gateway/internal/model"
	"github.com/jackc/pgx/v5"
)

// CreatePaymentOrder 新建支付订单。out_trade_no 由调用方保证唯一(并有 DB UNIQUE 约束兜底)。
func (s *Store) CreatePaymentOrder(ctx context.Context, o *model.PaymentOrder) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO payment_orders(id,tenant_id,user_id,out_trade_no,provider,amount_cents,status,prepay_data,expires_at,created_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		o.ID, o.TenantID, o.UserID, o.OutTradeNo, o.Provider, o.AmountCents, o.Status, o.PrepayData, o.ExpiresAt, o.CreatedAt)
	return err
}

// UpdatePaymentOrderPrepay 写回下单后的预支付数据(微信 code_url / 支付宝跳转 URL)。
func (s *Store) UpdatePaymentOrderPrepay(ctx context.Context, id, prepayData string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE payment_orders SET prepay_data=$2 WHERE id=$1`, id, prepayData)
	return err
}

const paymentOrderCols = `id,tenant_id,user_id,out_trade_no,provider,amount_cents,status,prepay_data,transaction_id,paid_at,expires_at,created_at`

// GetPaymentOrderByTradeNo 按商户订单号查询。
func (s *Store) GetPaymentOrderByTradeNo(ctx context.Context, outTradeNo string) (*model.PaymentOrder, error) {
	return scanPaymentOrderRow(s.Pool.QueryRow(ctx,
		`SELECT `+paymentOrderCols+` FROM payment_orders WHERE out_trade_no=$1`, outTradeNo))
}

// GetPaymentOrder 按主键查询。
func (s *Store) GetPaymentOrder(ctx context.Context, id string) (*model.PaymentOrder, error) {
	return scanPaymentOrderRow(s.Pool.QueryRow(ctx,
		`SELECT `+paymentOrderCols+` FROM payment_orders WHERE id=$1`, id))
}

// SettlePaymentAtomic 在单事务内完成"订单 pending→paid + 加余额 + 写 recharges + 写 ledger",
// 保证回调入账不会出现"订单已置 paid 但余额未加"的资金不一致窗口(此前 MarkPaid 与 Recharge 跨事务)。
// 仅当本次完成 pending→paid 返回 newlyPaid=true 时才加余额(幂等:回调/查单重入只加一次)。
// 返回 newlyPaid 与加余额后的新余额。
func (s *Store) SettlePaymentAtomic(ctx context.Context, outTradeNo, txnID string, paidAt time.Time) (bool, int64, error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return false, 0, err
	}
	defer tx.Rollback(ctx)

	var id, tenantID, userID string
	var amount int64
	// 临界区: pending→paid 或 closed→paid 的那次拿到行。已 paid 则 RowsAffected=0。
	// 允许 closed→paid: CloseExpired 在"渠道查单 paid=false"与"MarkClosed"之间存在窗口,
	// 用户在此窗口内付款但异步回调尚未到达时,订单会被关单;随后到达的回调带可信付款证据
	// (验签通过/查单 paid),应允许补入账,否则用户付款却永久无法到账。
	if err = tx.QueryRow(ctx,
		`UPDATE payment_orders SET status='paid', transaction_id=$2, paid_at=$3
		 WHERE out_trade_no=$1 AND status IN ('pending','closed')
		 RETURNING id, tenant_id, user_id, amount_cents`, outTradeNo, txnID, paidAt).
		Scan(&id, &tenantID, &userID, &amount); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// 已 paid(重复回调/查单),幂等跳过。
			_ = tx.Commit(ctx)
			return false, 0, nil
		}
		return false, 0, err
	}

	// 加余额 + 写 recharges + 写 ledger(镜像 AdjustAtomic 的充值路径)。
	var nb int64
	if err = tx.QueryRow(ctx,
		`UPDATE users SET balance_cents = balance_cents + $2 WHERE id = $1 RETURNING balance_cents`,
		userID, amount).Scan(&nb); err != nil {
		return false, 0, err
	}
	if _, err = tx.Exec(ctx,
		`INSERT INTO recharges(id,tenant_id,user_id,amount_cents,status,created_at) VALUES($1,$2,$3,$4,$5,$6)`,
		storeID(), tenantID, userID, amount, "success", paidAt); err != nil {
		return false, 0, err
	}
	if _, err = tx.Exec(ctx,
		`INSERT INTO billing_ledger(id,tenant_id,user_id,request_id,model,input_tokens,output_tokens,cost_cents,price_cents,margin_cents,type,balance_after,created_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		storeID(), tenantID, userID, outTradeNo, "-", 0, 0, 0, -amount, 0, model.LedgerRecharge, nb, paidAt); err != nil {
		return false, 0, err
	}
	if err = tx.Commit(ctx); err != nil {
		return false, 0, err
	}
	return true, nb, nil
}

// MarkClosed 将订单置为 closed(超时未支付关单)。仅 pending 可关闭,避免误关已付单。
func (s *Store) MarkClosed(ctx context.Context, outTradeNo string) error {
	_, err := s.Pool.Exec(ctx,
		`UPDATE payment_orders SET status='closed' WHERE out_trade_no=$1 AND status='pending'`, outTradeNo)
	return err
}

// ListPendingBefore 列出指定时刻之前仍处于 pending 的订单(关单扫描用)。
func (s *Store) ListPendingBefore(ctx context.Context, t time.Time) ([]*model.PaymentOrder, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id,tenant_id,user_id,out_trade_no,provider,amount_cents,status,prepay_data,transaction_id,paid_at,expires_at,created_at
		 FROM payment_orders WHERE status='pending' AND expires_at<$1 ORDER BY expires_at LIMIT 200`, t)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.PaymentOrder
	for rows.Next() {
		o, err := scanPaymentOrderRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// scannable 同时兼容 pgx.Row(单行)与 pgx.Rows(多行集合的当前行),二者均有 Scan 方法。
type scannable interface {
	Scan(dest ...any) error
}

func scanPaymentOrderRow(row scannable) (*model.PaymentOrder, error) {
	o := &model.PaymentOrder{}
	if err := row.Scan(&o.ID, &o.TenantID, &o.UserID, &o.OutTradeNo, &o.Provider, &o.AmountCents,
		&o.Status, &o.PrepayData, &o.TransactionID, &o.PaidAt, &o.ExpiresAt, &o.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil // 未找到,非错误
		}
		return nil, err
	}
	return o, nil
}
