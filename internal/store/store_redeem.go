package store

import (
	"context"
	"errors"
	"time"

	"github.com/aitoys/llm-gateway/internal/crypto"
	"github.com/aitoys/llm-gateway/internal/model"
	"github.com/jackc/pgx/v5"
)

// ErrInvalidRedeemCode 兑换码无效/已用/过期/停用。
var ErrInvalidRedeemCode = errors.New("invalid redeem code")

// CreateRedeemCode 插入一张卡密。
func (s *Store) CreateRedeemCode(ctx context.Context, c *model.RedeemCode) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO redeem_codes(id,code,amount_cents,status,note,created_at,expires_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7)`,
		c.ID, c.Code, c.AmountCents, c.Status, c.Note, c.CreatedAt, c.ExpiresAt)
	return err
}

// ListRedeemCodes 列出卡密(按创建时间倒序)。仅平台 admin 调用,看全部。
func (s *Store) ListRedeemCodes(ctx context.Context, limit int) ([]model.RedeemCode, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.Pool.Query(ctx,
		`SELECT id,code,amount_cents,status,COALESCE(note,''),used_by_user_id,used_at,created_at,expires_at
		 FROM redeem_codes ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.RedeemCode
	for rows.Next() {
		var c model.RedeemCode
		if err := rows.Scan(&c.ID, &c.Code, &c.AmountCents, &c.Status, &c.Note, &c.UsedByUserID, &c.UsedAt, &c.CreatedAt, &c.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// RedeemCodeAtomic 原子兑换:标记卡密 used + 加余额 + 写 recharge 账目,单事务保证账实一致。
// 返回 (amount, newBalance)。code 无效/已用/过期返回 ErrInvalidRedeemCode,余额与账目均不变。
func (s *Store) RedeemCodeAtomic(ctx context.Context, code, userID, tenantID string) (amount, newBalance int64, err error) {
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		// 原子标记 used(仅 active 且未过期)。RETURNING 拿面额;无行=无效/已用/过期。
		e := tx.QueryRow(ctx,
			`UPDATE redeem_codes SET status='used', used_by_user_id=$2, used_at=now()
			 WHERE code=$1 AND status='active' AND (expires_at IS NULL OR expires_at>now())
			 RETURNING amount_cents`, code, userID).Scan(&amount)
		if e != nil {
			if errors.Is(e, pgx.ErrNoRows) {
				return ErrInvalidRedeemCode
			}
			return e
		}
		// 加余额(recharge 加余额无下界风险,不触发 balance>=0 CHECK)。
		if _, e := tx.Exec(ctx, `UPDATE users SET balance_cents=balance_cents+$2 WHERE id=$1`, userID, amount); e != nil {
			return e
		}
		if e := tx.QueryRow(ctx, `SELECT balance_cents FROM users WHERE id=$1`, userID).Scan(&newBalance); e != nil {
			return e
		}
		// 写 recharge 账目(PriceCents 负数=加余额,与 AdjustAtomic/Recharge 一致;BalanceAfter 取加余额后快照)。
		leg := &model.BillingLedger{
			ID: crypto.NewID(), TenantID: tenantID, UserID: userID,
			PriceCents: -amount, Type: model.LedgerRecharge, BalanceAfter: newBalance, CreatedAt: time.Now(),
		}
		_, e = tx.Exec(ctx, ledgerInsertSQL, ledgerInsertArgs(leg)...)
		return e
	})
	return amount, newBalance, err
}
