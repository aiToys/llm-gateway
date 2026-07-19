package store

import (
	"context"
	"time"

	"github.com/aitoys/llm-gateway/internal/crypto"
	"github.com/aitoys/llm-gateway/internal/model"
)

// InsertAudit 写一条审计日志,带来源 IP 与可选 payload(变更前后快照等)。
func (s *Store) InsertAudit(ctx context.Context, actorID, action, target, ip string, payload []byte) error {
	id, err := crypto.RandomHex(12)
	if err != nil {
		return err
	}
	_, err = s.Pool.Exec(ctx,
		`INSERT INTO audit_logs(id, actor_id, action, target, ip, payload, created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`,
		id, actorID, action, target, ip, payload, time.Now())
	return err
}

// storeID 生成一个随机 hex id(store 包内复用)。
func storeID() string {
	id, err := crypto.RandomHex(16)
	if err != nil {
		return "id-" + time.Now().Format("20060102150405")
	}
	return id
}

func (s *Store) ListAudit(ctx context.Context, limit int) ([]*model.AuditLog, error) {
	limit = ClampLimit(limit, 200, 1000)
	rows, err := s.Pool.Query(ctx,
		`SELECT id, coalesce(actor_id,''), action, coalesce(target,''), coalesce(ip,''), created_at FROM audit_logs ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.AuditLog
	for rows.Next() {
		a := &model.AuditLog{}
		if err := rows.Scan(&a.ID, &a.ActorID, &a.Action, &a.Target, &a.IP, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
