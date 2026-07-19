package store

import (
	"context"
	"time"

	"github.com/aitoys/llm-gateway/internal/model"
)

// EnqueuePendingCharge 计费失败时落库一条待重试项(best-effort,新连接单条 insert)。
func (s *Store) EnqueuePendingCharge(ctx context.Context, p *model.PendingCharge) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO pending_charges(id,tenant_id,user_id,request_id,model,input_tokens,output_tokens,cache_read_tokens,cache_write_tokens,price_cents,cost_cents,attempts,status,next_retry_at,created_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		p.ID, p.TenantID, p.UserID, p.RequestID, p.Model, p.InputTokens, p.OutputTokens,
		p.CacheReadTokens, p.CacheWriteTokens, p.PriceCents, p.CostCents, p.Attempts, p.Status, p.NextRetryAt, p.CreatedAt)
	return err
}

const pendingChargeCols = `id,tenant_id,user_id,request_id,model,input_tokens,output_tokens,cache_read_tokens,cache_write_tokens,price_cents,cost_cents,attempts,status,last_error,next_retry_at,created_at`

// ListPendingForRetry 取到期待重试项(状态 pending 且 next_retry_at<=before),按到期先后,限 limit 条。
func (s *Store) ListPendingForRetry(ctx context.Context, before time.Time, limit int) ([]*model.PendingCharge, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT `+pendingChargeCols+` FROM pending_charges
		 WHERE status='pending' AND next_retry_at<=$1
		 ORDER BY next_retry_at LIMIT $2`, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.PendingCharge
	for rows.Next() {
		p := &model.PendingCharge{}
		if err := rows.Scan(&p.ID, &p.TenantID, &p.UserID, &p.RequestID, &p.Model, &p.InputTokens, &p.OutputTokens,
			&p.CacheReadTokens, &p.CacheWriteTokens, &p.PriceCents, &p.CostCents, &p.Attempts, &p.Status, &p.LastError,
			&p.NextRetryAt, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// MarkPendingDone 标记重试成功。仅 pending→done,避免重复结算把已终态行改回。
func (s *Store) MarkPendingDone(ctx context.Context, id string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE pending_charges SET status='done' WHERE id=$1 AND status='pending'`, id)
	return err
}

// MarkPendingRetry 记录一次失败: attempts+1、写错误、安排下次重试时间。仅 pending 行可改。
func (s *Store) MarkPendingRetry(ctx context.Context, id, lastError string, attempts int, nextRetryAt time.Time) error {
	_, err := s.Pool.Exec(ctx,
		`UPDATE pending_charges SET attempts=$2, last_error=$3, next_retry_at=$4 WHERE id=$1 AND status='pending'`,
		id, attempts, lastError, nextRetryAt)
	return err
}

// MarkPendingAbandoned 重试耗尽,放弃(配合告警指标,需人工介入对账)。仅 pending→abandoned。
func (s *Store) MarkPendingAbandoned(ctx context.Context, id, lastError string) error {
	_, err := s.Pool.Exec(ctx,
		`UPDATE pending_charges SET status='abandoned', last_error=$2 WHERE id=$1 AND status='pending'`, id, lastError)
	return err
}
