package store

import (
	"context"
	"fmt"
	"time"

	"github.com/aitoys/llm-gateway/internal/model"
)

// InsertRequestLog 写一条请求/响应日志(由 relay 异步调用)。
func (s *Store) InsertRequestLog(ctx context.Context, l *model.RequestLog) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO request_logs(id,request_id,tenant_id,user_id,api_key_id,model,provider,channel_id,method,path,status,latency_ms,input_tokens,output_tokens,price_cents,request_body,response_body,error,created_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)`,
		l.ID, l.RequestID, l.TenantID, l.UserID, l.APIKeyID, l.Model, l.Provider, l.ChannelID,
		l.Method, l.Path, l.Status, l.LatencyMs, l.InputTokens, l.OutputTokens, l.PriceCents,
		l.RequestBody, l.ResponseBody, l.Error, l.CreatedAt)
	return err
}

// ReqLogFilter 请求日志查询过滤(空值=不过滤)。
type ReqLogFilter struct {
	TenantID string
	UserID   string
	APIKeyID string
	Model    string
	Status   int // 0=不限
}

// ListRequestLogs 按过滤+时间倒序取最近 limit 条。limit clamp [1,1000] 默认 200。
// 列表不返回 request_body/response_body(可能很大),详情用 GetRequestLog。
func (s *Store) ListRequestLogs(ctx context.Context, f ReqLogFilter, limit int) ([]*model.RequestLog, error) {
	limit = ClampLimit(limit, 200, 1000)
	q := `SELECT id,request_id,tenant_id,user_id,api_key_id,model,provider,channel_id,method,path,status,latency_ms,input_tokens,output_tokens,price_cents,error,created_at FROM request_logs WHERE 1=1`
	args := []any{}
	n := 1
	addStr := func(col, val string) {
		if val == "" {
			return
		}
		q += fmt.Sprintf(" AND %s=$%d", col, n)
		args = append(args, val)
		n++
	}
	addStr("tenant_id", f.TenantID)
	addStr("user_id", f.UserID)
	addStr("api_key_id", f.APIKeyID)
	addStr("model", f.Model)
	if f.Status != 0 {
		q += fmt.Sprintf(" AND status=$%d", n)
		args = append(args, f.Status)
		n++
	}
	q += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", n)
	args = append(args, limit)

	rows, err := s.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.RequestLog
	for rows.Next() {
		l := &model.RequestLog{}
		if err := rows.Scan(&l.ID, &l.RequestID, &l.TenantID, &l.UserID, &l.APIKeyID, &l.Model, &l.Provider, &l.ChannelID,
			&l.Method, &l.Path, &l.Status, &l.LatencyMs, &l.InputTokens, &l.OutputTokens, &l.PriceCents, &l.Error, &l.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// GetRequestLog 取单条详情(含 request_body/response_body)。
func (s *Store) GetRequestLog(ctx context.Context, id string) (*model.RequestLog, error) {
	l := &model.RequestLog{}
	err := s.Pool.QueryRow(ctx,
		`SELECT id,request_id,tenant_id,user_id,api_key_id,model,provider,channel_id,method,path,status,latency_ms,input_tokens,output_tokens,price_cents,request_body,response_body,error,created_at
		 FROM request_logs WHERE id=$1`, id).
		Scan(&l.ID, &l.RequestID, &l.TenantID, &l.UserID, &l.APIKeyID, &l.Model, &l.Provider, &l.ChannelID,
			&l.Method, &l.Path, &l.Status, &l.LatencyMs, &l.InputTokens, &l.OutputTokens, &l.PriceCents,
			&l.RequestBody, &l.ResponseBody, &l.Error, &l.CreatedAt)
	if err != nil {
		return nil, err
	}
	return l, nil
}

// DeleteOldRequestLogs 删除早于 before 的请求日志(TTL 清理,由后台 worker 调用)。返回删除行数。
func (s *Store) DeleteOldRequestLogs(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.Pool.Exec(ctx, `DELETE FROM request_logs WHERE created_at < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
